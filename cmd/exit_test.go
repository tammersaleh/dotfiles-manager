package cmd_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles cmd/dfm into a temp dir with the given ldflags and
// returns its path. Proves the exit codes and the GoReleaser -X path are
// real, not just what Run returns in-process.
func buildBinary(t *testing.T, ldflags string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "dfm")
	args := []string{"build", "-o", bin}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "github.com/tammersaleh/dotfiles-manager/cmd/dfm")
	build := exec.Command("go", args...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestBinary_ExitCodes(t *testing.T) {
	bin := buildBinary(t, "-X github.com/tammersaleh/dotfiles-manager/cmd.Version=9.8.7")

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantErr    string // error code in the stderr JSON, empty for none
	}{
		{"version", []string{"version"}, 0, "9.8.7\n", ""},
		{"stub", []string{"pull"}, 1, "", "not_implemented"},
		{"status not a repo", []string{"status"}, 4, "", "git_failed"},
		{"install empty root", []string{"install"}, 1, "", "package_missing"},
		{"conflict", []string{"install"}, 2, "", "conflict"},
		{"bad args", []string{"nope"}, 1, "", "invalid_arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := exec.Command(bin, tt.args...)
			// Keep the subprocess away from the real ~/dotfiles.
			root, target := t.TempDir(), t.TempDir()
			if tt.name == "conflict" || tt.name == "status not a repo" {
				for _, pkg := range []string{"public", "private"} {
					if err := os.MkdirAll(filepath.Join(root, pkg), 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tt.name == "conflict" {
				for _, dir := range []string{filepath.Join(root, "public"), target} {
					if err := os.WriteFile(filepath.Join(dir, ".examplerc"), nil, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			c.Env = append(os.Environ(), "DFM_ROOT="+root, "DFM_TARGET="+target)
			var stderr strings.Builder
			c.Stderr = &stderr
			stdout, err := c.Output()

			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.wantCode, stderr.String())
			}
			if string(stdout) != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if tt.wantErr == "" {
				if stderr.Len() != 0 {
					t.Errorf("stderr should be empty, got %q", stderr.String())
				}
				return
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(stderr.String()), &m); err != nil {
				t.Fatalf("stderr not one JSON object: %q", stderr.String())
			}
			if m["error"] != tt.wantErr {
				t.Errorf("error = %v, want %s", m["error"], tt.wantErr)
			}
		})
	}
}
