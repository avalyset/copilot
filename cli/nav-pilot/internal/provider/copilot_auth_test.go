package provider

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navikt/copilot/cli/nav-pilot/internal/testhome"
	"golang.org/x/sys/unix"
)

func TestCopilotAuthRecoveryEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, hosts, mode string
		env               []string
		warn              bool
	}{
		{"no evidence", "", "auto", nil, true},
		{"native login unknown", "github.example.com:\n  user: other\n", "auto", nil, true},
		{"gh login record", "github.com:\n  user: selected\n", "auto", nil, false},
		{"malformed record", "[", "auto", nil, true},
		{"empty user", "github.com:\n  user: ''\n", "auto", nil, true},
		{"explicit token", "", "auto", []string{"COPILOT_GITHUB_TOKEN=fake"}, false},
		{"blank token", "", "auto", []string{"GH_TOKEN= \t"}, true},
		{"env_only refusal owns recovery", "", "env_only", nil, false},
		{"local BYOK", "", "auto", []string{"COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:1/v1"}, false},
		{"gh_only with gh record", "github.com:\n  user: selected\n", "gh_only", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateHome(t)
			dir := t.TempDir()
			t.Setenv("GH_CONFIG_DIR", dir)
			if err := os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte(tc.hosts), 0o600); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			ReportCopilotAuthRecovery(&b, tc.env, tc.mode)
			if strings.Contains(b.String(), CopilotHostLoginCommand) != tc.warn {
				t.Fatalf("recovery = %q, want warning %v", b.String(), tc.warn)
			}
			if tc.warn && (!strings.Contains(b.String(), "launch will continue") || !strings.Contains(b.String(), "changes gh's identity")) {
				t.Fatalf("warning must not block native login or hide identity change: %s", &b)
			}
		})
	}
}

func TestHostGHRecoveryCommandsClearAmbientIdentity(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "commands")
	script := "#!/bin/sh\nfor name in GH_TOKEN GITHUB_TOKEN COPILOT_GITHUB_TOKEN GH_HOST GH_REPO; do\n eval 'present=${'\"$name\"'+yes}'\n [ -z \"$present\" ] || exit 9\ndone\nprintf '%s\\n' \"$*\" >> \"$AUTH_TEST_OUT\"\n"
	for _, binary := range []string{"gh", "nav-pilot"} {
		if err := testhome.WriteExec(filepath.Join(dir, binary), script); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("AUTH_TEST_OUT", out)
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "COPILOT_GITHUB_TOKEN", "GH_HOST", "GH_REPO"} {
		t.Setenv(name, "polluted")
	}
	var b bytes.Buffer
	ReportHostGHRecovery(&b)
	for _, command := range []string{CopilotHostLoginCommand, HostGHRestartCommand} {
		if !strings.Contains(b.String(), command) {
			t.Fatalf("recovery omitted command: %s", &b)
		}
		if got, err := exec.Command("/bin/sh", "-c", command).CombinedOutput(); err != nil {
			t.Fatalf("printed command failed: %v, %s", err, got)
		}
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "auth login --hostname github.com --web\n\n" {
		t.Fatalf("fake host commands = %q, %v", got, err)
	}
	if os.Getenv("GH_TOKEN") != "polluted" {
		t.Fatal("subshell cleanup must not modify the parent")
	}
}

func TestGHLoginRecordRejectsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_CONFIG_DIR", dir)
	p := filepath.Join(dir, "hosts.yml")
	if err := unix.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if ghLoginRecorded() {
		t.Fatal("FIFO is not login evidence")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("github.com:\n  user: selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
	if ghLoginRecorded() {
		t.Fatal("symlink is not login evidence")
	}
}

func TestGHLoginRecordConfigLocation(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("GH_CONFIG_DIR", "")
	for _, xdg := range []string{"", t.TempDir()} {
		t.Setenv("XDG_CONFIG_HOME", xdg)
		dir := filepath.Join(home, ".config", "gh")
		if xdg != "" {
			dir = filepath.Join(xdg, "gh")
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte("github.com:\n  user: selected\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !ghLoginRecorded() {
			t.Fatalf("login record not found in %s", dir)
		}
	}
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	if ghLoginRecorded() {
		t.Fatal("GH_CONFIG_DIR must win over XDG_CONFIG_HOME")
	}
}
