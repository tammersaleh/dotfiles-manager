package cmd_test

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/tammersaleh/dotfiles-manager/cmd"
)

func TestVersion_Human(t *testing.T) {
	cmd.Version = "1.2.3"
	t.Cleanup(func() { cmd.Version = "" })

	code, out, errW := run(t, "version")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if out != "1.2.3\n" {
		t.Errorf("stdout = %q, want %q", out, "1.2.3\n")
	}
	if errW != "" {
		t.Errorf("stderr should be empty, got %q", errW)
	}
}

func TestVersion_JSON(t *testing.T) {
	cmd.Version = "1.2.3"
	t.Cleanup(func() { cmd.Version = "" })

	code, out, errW := run(t, "--json", "version")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if errW != "" {
		t.Errorf("stderr should be empty, got %q", errW)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want row + _meta, got %q", out)
	}
	if lines[0] != `{"version":"1.2.3"}` {
		t.Errorf("row = %s", lines[0])
	}
	if lines[1] != `{"_meta":{"has_more":false}}` {
		t.Errorf("meta = %s", lines[1])
	}
}

func TestVersion_DevFallback(t *testing.T) {
	// Under `go test` the ldflags value is empty and build info reports
	// "(devel)" or nothing, so the fallback must be "dev".
	cmd.Version = ""
	code, out, _ := run(t, "version")
	if code != 0 || out != "dev\n" {
		t.Errorf("exit=%d stdout=%q, want 0 and %q", code, out, "dev\n")
	}
}

func TestResolveVersion(t *testing.T) {
	bi := func(v string, ok bool) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			if !ok {
				return nil, false
			}
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	tests := []struct {
		name    string
		ldflags string
		bi      func() (*debug.BuildInfo, bool)
		want    string
	}{
		{"ldflags wins", "0.1.0", bi("v9.9.9", true), "0.1.0"},
		{"module version", "", bi("v0.2.0", true), "v0.2.0"},
		{"devel is unset", "", bi("(devel)", true), "dev"},
		{"empty is unset", "", bi("", true), "dev"},
		{"no build info", "", bi("", false), "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cmd.ResolveVersionForTest(tt.ldflags, tt.bi); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
