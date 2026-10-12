package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/navikt/copilot/cli/nav-pilot/internal/testhome"
)

func TestHostGHProbeDecisionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		exit         int
		want         ghAuth
	}{
		{"absent stored login despite ambient token", "not logged in", 1, ghAuthNone},
		{"keyring", "Logged in to github.com account selected (keyring)", 0, ghAuthKeyring},
		{"plaintext fallback", "Logged in to github.com account selected (/home/user/.config/gh/hosts.yml)", 0, ghAuthOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedConfig(t)
			dir := t.TempDir()
			path, record := filepath.Join(dir, "trusted-gh"), filepath.Join(dir, "calls")
			t.Setenv("AUTH_TEST_OUT", record)
			t.Setenv(cpltSandboxEnvVar, "")
			for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "COPILOT_GITHUB_TOKEN", "GH_HOST", "GH_REPO"} {
				t.Setenv(name, "polluted")
			}
			script := "#!/bin/sh\nfor name in GH_TOKEN GITHUB_TOKEN COPILOT_GITHUB_TOKEN GH_HOST GH_REPO; do\n eval 'present=${'\"$name\"'+yes}'\n [ -z \"$present\" ] || exit 9\ndone\nprintf '%s|%s\\n' \"$PWD\" \"$*\" > \"$AUTH_TEST_OUT\"\nprintf '%s\\n' '" + tc.output + "'\nexit " + fmt.Sprint(tc.exit) + "\n"
			if err := testhome.WriteExec(path, script); err != nil {
				t.Fatal(err)
			}
			if err := testhome.WriteExec(filepath.Join(dir, "gh"), "#!/bin/sh\nprintf shim > \"$AUTH_TEST_OUT.shim\"\nexit 0\n"); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			binary, probe := hostGHBinary, ghAuthProbe
			t.Cleanup(func() { hostGHBinary, ghAuthProbe = binary, probe })
			ghAuthProbe = probeHostGHAuth
			hostGHBinary = func() string { return path }
			// Exercise the shared setup/doctor decision through the real probe.
			st, detail := ghAuthProbe()
			if st != tc.want {
				t.Fatalf("status = %v, %s", st, detail)
			}
			var out bytes.Buffer
			reportGHAuth(&out, "", "copilot", st, detail)
			if st == ghAuthNone && !strings.Contains(out.String(), "does not start login") {
				t.Fatalf("missing manual recovery: %s", &out)
			}
			if st == ghAuthOK && !strings.Contains(out.String(), "has not verified secure storage") {
				t.Fatalf("plaintext must not promise secure storage: %s", &out)
			}
			home, _ := os.UserHomeDir()
			home, _ = filepath.EvalSymlinks(home)
			got, err := os.ReadFile(record)
			if err != nil || string(got) != home+"|auth status --hostname github.com\n" {
				t.Fatalf("host process boundary: %q, %v", got, err)
			}
			if _, err := os.Stat(record + ".shim"); !os.IsNotExist(err) {
				t.Fatalf("ambient project shim was executed: %v", err)
			}
			if os.Getenv("GH_TOKEN") != "polluted" {
				t.Fatal("parent credentials were modified")
			}
		})
	}
}

func TestGHAuthStorageMixedAccounts(t *testing.T) {
	stubSSH(t, false)
	plaintext := "  ✓ Logged in to github.com account active (/home/user/.config/gh/hosts.yml)\n  - Active account: true\n"
	keyring := "  ✓ Logged in to github.com account inactive (keyring)\n  - Active account: false\n"
	for _, tc := range []struct {
		name, status string
		want         ghAuth
	}{
		{"active plaintext first", plaintext + keyring, ghAuthOK},
		{"active plaintext last", keyring + plaintext, ghAuthOK},
		{"keyring only", keyring, ghAuthKeyring},
		{"plaintext only", plaintext, ghAuthOK},
		{"unknown successful status", "github.com\nstorage unknown\n", ghAuthOK},
		{"unrelated keyring substring", "diagnostic mentions (keyring)\n", ghAuthOK},
		{"other host keyring", "Logged in to github.example.com account x (keyring)\n", ghAuthOK},
		{"unknown account storage", "Logged in to github.com account x (unknown)\n" + keyring, ghAuthOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, detail := ghAuthFrom([]byte(tc.status), nil)
			if st != tc.want {
				t.Fatalf("storage = %v, want %v", st, tc.want)
			}
			var out bytes.Buffer
			reportGHAuth(&out, "", "copilot", st, detail)
			if strings.Contains(out.String(), "has not verified secure storage") != (tc.want != ghAuthKeyring) {
				t.Fatalf("storage warning = %s", &out)
			}
		})
	}
}

func TestFindSetupGHRejectsUntrustedSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	path := filepath.Join(outside, "gh")
	if err := testhome.WriteExec(path, "#!/bin/sh\nexit 0\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "gh")); err != nil {
		t.Fatal(err)
	}
	if got := findSetupGH([]string{root}, []string{root}); got != "" {
		t.Fatalf("escaped trusted root: %s", got)
	}
	path, _ = filepath.EvalSymlinks(path)
	if got := findSetupGH([]string{root}, []string{outside}); got != path {
		t.Fatalf("expected allowed symlink target %s, got %s", path, got)
	}
}

func TestHostGHProbeBoundedAndUnavailable(t *testing.T) {
	isolatedConfig(t)
	t.Setenv(cpltSandboxEnvVar, "")
	binary, probe := hostGHBinary, ghAuthProbe
	t.Cleanup(func() { hostGHBinary, ghAuthProbe = binary, probe })
	ghAuthProbe = probeHostGHAuth
	hostGHBinary = func() string { return "" }
	if st, _ := ghAuthProbe(); st != ghAuthMissing {
		t.Fatal("missing trusted binary must not use PATH")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	if err := testhome.WriteExec(path, "#!/bin/sh\nexec /bin/sleep 30\n"); err != nil {
		t.Fatal(err)
	}
	hostGHBinary = func() string { return path }
	start := time.Now()
	if st, _ := ghAuthProbe(); st != ghAuthUnknown {
		t.Fatal("timeout must be unknown")
	}
	if elapsed := time.Since(start); elapsed > ghAuthTimeout+3*time.Second {
		t.Fatalf("recheck took %s", elapsed)
	}
	t.Setenv(cpltSandboxEnvVar, "1")
	hostGHBinary = func() string { t.Fatal("nested sandbox must not resolve host gh"); return "" }
	if st, _ := ghAuthProbe(); st != ghAuthUnknown {
		t.Fatal("nested sandbox must not claim host status")
	}
}
