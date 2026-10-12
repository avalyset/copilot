package provider

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/unix"
)

const hostGHUnset = "unset GH_TOKEN GITHUB_TOKEN COPILOT_GITHUB_TOKEN GH_HOST GH_REPO; "
const CopilotHostLoginCommand = "(" + hostGHUnset + "gh auth login --hostname github.com --web)"
const HostGHRestartCommand = "(" + hostGHUnset + "nav-pilot)"

// ReportHostGHRecovery scopes environment cleanup to each host command so a
// polluted shell cannot override the stored login at login or restart time.
func ReportHostGHRecovery(w io.Writer) {
	fmt.Fprintf(w, "In a host terminal, install gh if needed, then run:\n  %s\n  %s\nChoose the account you intend to use; this command changes gh's identity. Keep credentials in the OS keychain.\n", CopilotHostLoginCommand, HostGHRestartCommand)
}

// ReportCopilotAuthRecovery never probes the Keychain or starts login. Native
// Copilot credentials cannot be reliably inferred from gh's sign-in state.
func ReportCopilotAuthRecovery(w io.Writer, env []string, mode string) {
	if mode == "gh_only" {
		fmt.Fprintln(w, "Warning: copilot_auth_mode=gh_only strips inherited token variables only; exclusive gh-source enforcement is unsupported. cplt can inject a token and Copilot can use its stored login. Use nav-pilot config set copilot_auth_mode auto to remove this filter.")
	}
	if mode == "env_only" {
		fmt.Fprintln(w, "Warning: copilot_auth_mode=env_only checks only that a non-blank token reaches cplt; exclusive env-source delivery is unsupported. cplt can deny that variable or supply another credential, and Copilot can use its stored login. Use nav-pilot config set copilot_auth_mode auto to remove this check.")
	}
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "COPILOT_PROVIDER_BASE_URL="); ok && strings.TrimSpace(value) != "" {
			return
		}
	}
	if hasEnvToken(env) || mode == "env_only" || ghLoginRecorded() {
		return
	}
	fmt.Fprintln(w, "Could not confirm a Copilot login from the environment or gh's github.com login record. Native Copilot Keychain login may still work; launch will continue. If you need to sign in, choose an account with Copilot access and exit the sandbox first: cplt blocks browser opening by default, so Copilot /login may fail.")
	ReportHostGHRecovery(w)
}

// A login record is evidence, not proof that its token is still valid. Avoid
// gh auth status on launch: it needs network and adds a blocking auth probe.
func ghLoginRecorded() bool {
	dir := os.Getenv("GH_CONFIG_DIR")
	if dir == "" {
		dir = os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return false
			}
			dir = filepath.Join(home, ".config")
		}
		dir = filepath.Join(dir, "gh")
	}
	// A writable config path must not follow a symlink or hang on a FIFO.
	f, err := os.OpenFile(filepath.Join(dir, "hosts.yml"), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return false
	}
	var hosts map[string]struct {
		User string `yaml:"user"`
	}
	if err := yaml.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&hosts); err != nil {
		return false
	}
	return strings.TrimSpace(hosts["github.com"].User) != ""
}
