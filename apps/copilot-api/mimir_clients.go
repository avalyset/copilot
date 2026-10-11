package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

// Client share in nav-pilot for /innsikt/trender, from the daily Mimir copy
// (copilot-metrics mimir_daily.go). Only shares leave the server.
//
// nav_pilot_config rows count distinct devices per client and day, so they are
// not hit by the counter overcount in old Copilot CLI versions; the query still
// keeps counter_reliable rows only. A month's share is device-days per client.

const (
	mimirDataset = "copilot_metrics_staging"
	// minClientDevices is the public floor: a client is named only with at
	// least this many devices per day on average that month, otherwise it is
	// folded into «Andre». A month below it in total is left out.
	minClientDevices = 20
)

var clientBands = []string{"Copilot CLI", "opencode", "Andre"}

type mimirClientRow struct {
	Month    string `bigquery:"month"`
	Days     int64  `bigquery:"days"`
	Copilot  int64  `bigquery:"copilot"`
	Opencode int64  `bigquery:"opencode"`
	Other    int64  `bigquery:"other"`
}

// clientChart applies the floor and turns device-days into whole-percent shares.
func clientChart(rows []mimirClientRow) SegmentChart {
	seg := make([]segmentRow, len(rows))
	for i, r := range rows {
		s := segmentRow{Month: r.Month, A: r.Copilot, B: r.Opencode, C: r.Other}
		if r.Days > 0 {
			floor := minClientDevices * r.Days
			if s.A < floor {
				s.A, s.C = 0, s.C+s.A
			}
			if s.B < floor {
				s.B, s.C = 0, s.C+s.B
			}
			if s.A+s.B+s.C < floor {
				s = segmentRow{Month: r.Month}
			}
		}
		seg[i] = s
	}
	return buildChart(seg, clientBands)
}

func (bq *BigQueryClient) GetMimirClientShares(ctx context.Context) (SegmentChart, error) {
	q := bq.client.Query(fmt.Sprintf(`
      WITH t AS (SELECT * FROM %[1]s),
      -- Every day the job stored, so a day where all clients fell under the
      -- producer's k>=5 floor still counts in the divisor.
      days AS (SELECT FORMAT_DATE('%%Y-%%m', day) AS month, COUNT(DISTINCT day) AS days FROM t GROUP BY month)
      SELECT
        month,
        days.days,
        COALESCE(SUM(IF(client = 'copilot', devices, 0)), 0) AS copilot,
        COALESCE(SUM(IF(client = 'opencode', devices, 0)), 0) AS opencode,
        COALESCE(SUM(IF(client IN ('copilot', 'opencode'), 0, devices)), 0) AS other
      FROM days
      LEFT JOIN t ON FORMAT_DATE('%%Y-%%m', t.day) = days.month
        AND t.family = 'nav_pilot_config' AND t.counter_reliable
      GROUP BY month, days.days
      ORDER BY month
    `, bq.tableRef(mimirDataset, "mimir_daily")))
	it, err := q.Read(ctx)
	if err != nil {
		return SegmentChart{}, fmt.Errorf("execute query: %w", err)
	}
	rows, err := readAllRows[mimirClientRow](it)
	if err != nil {
		return SegmentChart{}, err
	}
	return clientChart(rows), nil
}

func (c *CachedBigQueryClient) GetMimirClientShares(ctx context.Context) (SegmentChart, error) {
	ctx, cancel := withQueryTimeout(ctx)
	defer cancel()
	return getCachedValue(c, "mimir_client_shares", func() (SegmentChart, error) {
		return c.client.GetMimirClientShares(ctx)
	})
}

// handleMimirClientShares handles GET /api/v1/copilot/usage/nav-pilot-clients
func (h *BigQueryHandlers) handleMimirClientShares(w http.ResponseWriter, r *http.Request) {
	chart, err := h.bqClient.GetMimirClientShares(r.Context())
	if err != nil {
		slog.Error("Failed to fetch nav-pilot client shares", "error", err)
		respondError(w, "internal_error", "Failed to fetch nav-pilot client shares", http.StatusInternalServerError)
		return
	}
	cacheControl(w, 3600, false)
	respondJSON(w, chart, http.StatusOK)
}
