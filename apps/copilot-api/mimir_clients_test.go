package main

import (
	"fmt"
	"testing"
)

// A client under 20 devices per day is folded into «Andre»; a month under 20 in
// total is null; only whole-percent shares come out.
func TestClientChartFloor(t *testing.T) {
	got := clientChart([]mimirClientRow{
		{Month: "2026-08", Days: 10, Copilot: 1500, Opencode: 150, Other: 350}, // opencode 15/day -> Andre
		{Month: "2026-09", Days: 10, Copilot: 600, Opencode: 300, Other: 100},
		{Month: "2026-10", Days: 10, Copilot: 100, Opencode: 50},
	})
	shares := func(m int) string {
		var s []string
		for _, b := range got.Bands {
			if b.Shares[m] == nil {
				s = append(s, "nil")
			} else {
				s = append(s, fmt.Sprint(*b.Shares[m]))
			}
		}
		return fmt.Sprint(s)
	}
	for m, want := range []string{"[75 0 25]", "[60 30 10]", "[nil nil nil]"} {
		if g := shares(m); g != want {
			t.Errorf("%s: got %s, want %s", got.Months[m], g, want)
		}
	}
}
