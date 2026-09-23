package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func newPrinter(json, quiet, verbose bool) (*Printer, *bytes.Buffer, *bytes.Buffer) {
	out, errW := &bytes.Buffer{}, &bytes.Buffer{}
	return &Printer{Out: out, Err: errW, JSON: json, Quiet: quiet, Verbose: verbose}, out, errW
}

func TestProgress(t *testing.T) {
	tests := []struct {
		name         string
		json, quiet  bool
		wantOnStderr bool
	}{
		{"human", false, false, true},
		{"quiet", false, true, false},
		{"json", true, false, false},
		{"json quiet", true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, out, errW := newPrinter(tt.json, tt.quiet, false)
			p.Progress("linked %s", ".examplerc")
			if out.Len() != 0 {
				t.Errorf("Progress wrote to stdout: %q", out.String())
			}
			got := errW.String()
			if tt.wantOnStderr && got != "linked .examplerc\n" {
				t.Errorf("stderr = %q, want %q", got, "linked .examplerc\n")
			}
			if !tt.wantOnStderr && got != "" {
				t.Errorf("stderr = %q, want empty", got)
			}
		})
	}
}

func TestVerbosef(t *testing.T) {
	tests := []struct {
		name           string
		verbose, quiet bool
		want           string
	}{
		{"off", false, false, ""},
		{"on", true, false, "symlink a -> b\n"},
		{"on beats quiet", true, true, "symlink a -> b\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, out, errW := newPrinter(false, tt.quiet, tt.verbose)
			p.Verbosef("symlink %s -> %s", "a", "b")
			if out.Len() != 0 {
				t.Errorf("Verbosef wrote to stdout: %q", out.String())
			}
			if errW.String() != tt.want {
				t.Errorf("stderr = %q, want %q", errW.String(), tt.want)
			}
		})
	}
}

func TestRowAndMeta_JSON(t *testing.T) {
	p, out, errW := newPrinter(true, false, false)
	if err := p.Row(map[string]any{"action": "link", "path": ".examplerc"}); err != nil {
		t.Fatal(err)
	}
	if err := p.PrintMeta(Meta{Created: 1}); err != nil {
		t.Fatal(err)
	}
	if errW.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", errW.String())
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out.String())
	}
	if lines[0] != `{"action":"link","path":".examplerc"}` {
		t.Errorf("row = %s", lines[0])
	}
	if lines[1] != `{"_meta":{"has_more":false,"created":1}}` {
		t.Errorf("meta = %s", lines[1])
	}
}

func TestRowAndMeta_HumanIsNoop(t *testing.T) {
	p, out, errW := newPrinter(false, false, false)
	if err := p.Row(map[string]any{"action": "link"}); err != nil {
		t.Fatal(err)
	}
	if err := p.PrintMeta(Meta{}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || errW.Len() != 0 {
		t.Errorf("human mode wrote rows: stdout=%q stderr=%q", out.String(), errW.String())
	}
}

func TestMeta_ZeroCountersOmitted(t *testing.T) {
	p, out, _ := newPrinter(true, false, false)
	if err := p.PrintMeta(Meta{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != `{"_meta":{"has_more":false}}` {
		t.Errorf("meta = %s", got)
	}
}

func TestResult(t *testing.T) {
	p, out, _ := newPrinter(false, false, false)
	p.Result("%s", "1.2.3")
	if out.String() != "1.2.3\n" {
		t.Errorf("stdout = %q", out.String())
	}
	p, out, _ = newPrinter(true, false, false)
	p.Result("%s", "1.2.3")
	if out.Len() != 0 {
		t.Errorf("Result under --json wrote %q", out.String())
	}
}

func TestPrintError_IgnoresQuietAndJSON(t *testing.T) {
	for _, mode := range []struct {
		name        string
		json, quiet bool
	}{{"human", false, false}, {"quiet", false, true}, {"json", true, false}} {
		t.Run(mode.name, func(t *testing.T) {
			p, out, errW := newPrinter(mode.json, mode.quiet, false)
			e := &Error{Err: "conflict", Detail: "exists", Hint: "remove it", Path: ".x", Code: ExitConflict}
			if err := p.PrintError(e); err != nil {
				t.Fatal(err)
			}
			if out.Len() != 0 {
				t.Errorf("error went to stdout: %q", out.String())
			}
			var got map[string]any
			if err := json.Unmarshal(errW.Bytes(), &got); err != nil {
				t.Fatalf("stderr is not one JSON object: %q", errW.String())
			}
			want := map[string]any{"error": "conflict", "detail": "exists", "hint": "remove it", "path": ".x"}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
			if _, ok := got["Code"]; ok {
				t.Error("Code leaked into JSON")
			}
			if strings.Count(errW.String(), "\n") != 1 {
				t.Errorf("want exactly one line, got %q", errW.String())
			}
		})
	}
}

func TestError_OmitsEmptyFields(t *testing.T) {
	b, err := json.Marshal(&Error{Err: "not_implemented"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"error":"not_implemented"}` {
		t.Errorf("got %s", b)
	}
}

func TestError_ErrorAndExitCode(t *testing.T) {
	e := &Error{Err: "dirty_tree", Detail: "public has changes"}
	if e.Error() != "dirty_tree: public has changes" {
		t.Errorf("Error() = %q", e.Error())
	}
	if (&Error{Err: "x"}).Error() != "x" {
		t.Error("Error() without detail should be the code alone")
	}
	if e.ExitCode() != ExitGeneral {
		t.Errorf("unclassified ExitCode = %d, want %d", e.ExitCode(), ExitGeneral)
	}
	if (&Error{Err: "conflict", Code: ExitConflict}).ExitCode() != ExitConflict {
		t.Error("ExitCode should return Code when set")
	}
	var target *Error
	if !errors.As(error(e), &target) {
		t.Error("*Error should satisfy errors.As")
	}
}
