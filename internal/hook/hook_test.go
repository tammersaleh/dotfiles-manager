package hook_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tammersaleh/dotfiles-manager/internal/hook"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile's mode is masked by umask; force it.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestExecutable(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		mode os.FileMode
		want bool
	}{
		{"exec", 0o755, true},
		{"owner exec only", 0o700, true},
		{"other exec only", 0o601, true},
		{"not exec", 0o644, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "_"))
			write(t, path, "#!/bin/sh\n", tt.mode)
			got, err := hook.Executable(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Executable(%o) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		got, err := hook.Executable(filepath.Join(dir, "nope"))
		if err != nil || got {
			t.Errorf("missing: got %v, %v", got, err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		got, err := hook.Executable(dir)
		if err != nil || got {
			t.Errorf("directory: got %v, %v", got, err)
		}
	})
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hook.sh")
	write(t, path, "#!/bin/sh\npwd\necho oops >&2\nexit 3\n", 0o755)

	var stdout, stderr bytes.Buffer
	code, err := hook.Run(path, cwd, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Errorf("exit = %d, want 3", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != cwd {
		t.Errorf("hook cwd = %q, want %q", got, cwd)
	}
	if stderr.String() != "oops\n" {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRun_CannotStart(t *testing.T) {
	code, err := hook.Run(filepath.Join(t.TempDir(), "missing"), t.TempDir(), nil, nil)
	if err == nil || code != -1 {
		t.Errorf("got %d, %v; want -1 and an error", code, err)
	}
}
