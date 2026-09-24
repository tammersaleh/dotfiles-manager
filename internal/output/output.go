// Package output implements the dfm output contract: human progress lines on
// stderr, JSONL rows plus a _meta trailer on stdout under --json, and fatal
// errors as one JSON object on stderr regardless of mode. See SPEC.md
// "Output".
package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// Exit codes. SPEC.md "Exit codes".
const (
	ExitSuccess  = 0
	ExitGeneral  = 1
	ExitConflict = 2
	ExitReserved = 3
	ExitGit      = 4
)

// Error is a fatal error. It is written to stderr as one JSON object and
// maps to a process exit code via Code. Err is a stable snake_case code.
type Error struct {
	Err    string `json:"error"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
	Path   string `json:"path,omitempty"`
	Code   int    `json:"-"`
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Err + ": " + e.Detail
	}
	return e.Err
}

// ExitCode returns the process exit code for e. A zero Code means the
// caller did not classify the error, which is a general error.
func (e *Error) ExitCode() int {
	if e.Code == 0 {
		return ExitGeneral
	}
	return e.Code
}

// Reported is returned by a command that has already written its error
// lines to stderr (conflicts, one line each) and only needs the process to
// exit with Code. cmd.Run prints nothing for it.
type Reported struct{ Code int }

func (r *Reported) Error() string { return fmt.Sprintf("exit %d", r.Code) }

// Meta is the _meta trailer that ends every --json stream. HasMore is
// always emitted for parity with the sibling CLIs; the counters are
// omitted when zero so commands without them (version) stay minimal.
type Meta struct {
	HasMore  bool `json:"has_more"`
	Created  int  `json:"created,omitempty"`
	Removed  int  `json:"removed,omitempty"`
	Unfolded int  `json:"unfolded,omitempty"`
	Refolded int  `json:"refolded,omitempty"`
	Adopted  int  `json:"adopted,omitempty"` // public/private: paths moved into a package

	// status counters
	Dirty       int `json:"dirty,omitempty"`
	Conflicts   int `json:"conflicts,omitempty"`
	BrokenLinks int `json:"broken_links,omitempty"`
	Pending     int `json:"pending,omitempty"`
}

type metaWrapper struct {
	Meta Meta `json:"_meta"`
}

// Printer routes command output according to the global flags.
type Printer struct {
	Out     io.Writer // stdout: JSONL rows under JSON, human results otherwise
	Err     io.Writer // stderr: progress, verbose, fatal errors
	JSON    bool
	Quiet   bool
	Verbose bool
}

// Progress writes one human progress line to stderr. Suppressed by --quiet
// and by --json, where stdout rows carry the same information.
func (p *Printer) Progress(format string, args ...any) {
	if p.Quiet || p.JSON {
		return
	}
	_, _ = fmt.Fprintf(p.Err, format+"\n", args...)
}

// Verbosef writes one diagnostic line to stderr when --verbose is set.
// --quiet does not silence it: asking for both means verbose wins.
func (p *Printer) Verbosef(format string, args ...any) {
	if !p.Verbose {
		return
	}
	_, _ = fmt.Fprintf(p.Err, format+"\n", args...)
}

// Row writes one JSONL object to stdout. No-op unless --json is set.
func (p *Printer) Row(v any) error {
	if !p.JSON {
		return nil
	}
	return p.writeJSON(v)
}

// PrintMeta writes the _meta trailer to stdout. No-op unless --json is set.
func (p *Printer) PrintMeta(m Meta) error {
	if !p.JSON {
		return nil
	}
	return p.writeJSON(metaWrapper{Meta: m})
}

// Result writes a human-mode result line to stdout. No-op under --json;
// pair it with Row for the JSON shape. Used by the few commands whose
// output is the result itself (version) rather than progress.
func (p *Printer) Result(format string, args ...any) {
	if p.JSON {
		return
	}
	_, _ = fmt.Fprintf(p.Out, format+"\n", args...)
}

// PrintError writes e to stderr as one JSON object. Not affected by --quiet
// or --json.
func (p *Printer) PrintError(e *Error) error {
	return json.NewEncoder(p.Err).Encode(e)
}

func (p *Printer) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = p.Out.Write(b)
	return err
}
