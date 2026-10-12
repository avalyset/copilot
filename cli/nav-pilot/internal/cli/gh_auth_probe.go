package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	providerpkg "github.com/navikt/copilot/cli/nav-pilot/internal/provider"
)

// Match cplt's host binary locations and resolved roots, never a project PATH
// entry or a sandbox gh wrapper. Homebrew's bin symlink resolves into Cellar.
var hostGHBinary = func() string {
	return findSetupGH(
		[]string{"/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin", "/run/current-system/sw/bin", "/home/linuxbrew/.linuxbrew/bin"},
		[]string{"/usr/bin", "/bin", "/usr/local", "/opt/homebrew", "/run/current-system", "/nix/store", "/snap", "/home/linuxbrew/.linuxbrew"})
}

func findSetupGH(dirs, roots []string) string {
	for _, dir := range dirs {
		path, err := filepath.EvalSymlinks(filepath.Join(dir, "gh"))
		if err != nil {
			continue
		}
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
			continue
		}
		for _, root := range roots {
			realRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(realRoot, path)
			if err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return path
			}
		}
	}
	return ""
}

// Setup and doctor inspect host gh metadata only. Never resolve through PATH,
// use a project working directory, or let an exported token mask stored login.
func probeHostGHAuth() (ghAuth, string) {
	if e2eSeams == "1" {
		return ghAuthMissing, ""
	}
	if os.Getenv(cpltSandboxEnvVar) != "" {
		return ghAuthUnknown, "host sign-in must be checked outside cplt"
	}
	path := hostGHBinary()
	if path == "" {
		return ghAuthMissing, ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ghAuthUnknown, "could not resolve the host home directory"
	}
	ctx, cancel := context.WithTimeout(context.Background(), ghAuthTimeout)
	defer cancel()
	check := exec.CommandContext(ctx, path, "auth", "status", "--hostname", "github.com")
	check.Dir, check.Env, check.WaitDelay = home, providerpkg.CleanHostGHEnv(os.Environ()), time.Second
	out, err := check.CombinedOutput()
	if ctx.Err() != nil {
		return ghAuthUnknown, "host gh status exceeded its five-second deadline"
	}
	return ghAuthFrom(out, err)
}
