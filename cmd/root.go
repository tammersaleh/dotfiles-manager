// Package cmd is the dfm command tree. Run is the in-process entrypoint;
// cmd/dfm/main.go is a thin wrapper around it so tests can drive the whole
// CLI with captured stdout and stderr and a real exit code.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// CLI is the Kong grammar. Global flags per SPEC.md "Global flags".
type CLI struct {
	Root    string `help:"Dotfiles root holding the public and private packages (default ~/dotfiles)." env:"DFM_ROOT" placeholder:"DIR"`
	Target  string `help:"Directory the packages are linked into (default $HOME)." env:"DFM_TARGET" placeholder:"DIR"`
	DryRun  bool   `help:"Print the plan without changing anything."`
	JSON    bool   `help:"JSONL on stdout, one object per action, then a _meta trailer."`
	Verbose bool   `help:"Echo every filesystem and git operation to stderr."`
	Quiet   bool   `help:"Suppress progress lines. Errors still print."`

	Install InstallCmd `cmd:"" help:"Restow public then private into the target."`
	Public  PublicCmd  `cmd:"" help:"Move a path into the public package and link it back."`
	Private PrivateCmd `cmd:"" help:"Move a path into the private package and link it back."`
	Ignore  IgnoreCmd  `cmd:"" help:"Add a path to the owning package's .gitignore."`
	Pull    PullCmd    `cmd:"" help:"Update both packages, relink, run post-pull hooks."`
	Status  StatusCmd  `cmd:"" help:"Report dirty/ahead/behind per package and pending conflicts."`
	Version VersionCmd `cmd:"" help:"Print the dfm version."`

	out io.Writer
	err io.Writer
}

// SetOutput overrides stdout and stderr. Run calls it; tests may too.
func (c *CLI) SetOutput(out, errW io.Writer) {
	c.out = out
	c.err = errW
}

// Printer builds the output.Printer for the parsed global flags.
func (c *CLI) Printer() *output.Printer {
	out, errW := io.Writer(os.Stdout), io.Writer(os.Stderr)
	if c.out != nil {
		out = c.out
	}
	if c.err != nil {
		errW = c.err
	}
	return &output.Printer{Out: out, Err: errW, JSON: c.JSON, Quiet: c.Quiet, Verbose: c.Verbose}
}

// RootDir returns --root, or ~/dotfiles when unset. Resolved lazily so
// parsing never touches $HOME and tests can override it.
func (c *CLI) RootDir() (string, error) {
	if c.Root != "" {
		return c.Root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", &output.Error{Err: "no_home", Detail: err.Error(), Hint: "pass --root or set DFM_ROOT", Code: output.ExitGeneral}
	}
	return filepath.Join(home, "dotfiles"), nil
}

// TargetDir returns --target, or $HOME when unset.
func (c *CLI) TargetDir() (string, error) {
	if c.Target != "" {
		return c.Target, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", &output.Error{Err: "no_home", Detail: err.Error(), Hint: "pass --target or set DFM_TARGET", Code: output.ExitGeneral}
	}
	return home, nil
}

// exitCode is the panic value Run uses to unwind when Kong asks to exit
// during parsing (--help, --version handled by Kong, parse errors).
type exitCode int

// Run parses args (without argv[0]), runs the command, and returns the
// process exit code. Every fatal error is written to errW as one JSON
// object before returning; nothing is written by the caller.
func Run(args []string, stdout, stderr io.Writer) (code int) {
	var cli CLI
	cli.SetOutput(stdout, stderr)

	defer func() {
		if r := recover(); r != nil {
			c, ok := r.(exitCode)
			if !ok {
				panic(r)
			}
			code = int(c)
		}
	}()

	parser, err := kong.New(&cli,
		kong.Name("dfm"),
		kong.Description("Manage dotfiles: stow-compatible symlinks plus repo bookkeeping for the public and private packages."),
		kong.Writers(stdout, stderr),
		kong.Exit(func(c int) { panic(exitCode(c)) }),
	)
	if err != nil {
		// Grammar bug, not user error. Surface it and fail.
		return fail(&cli, &output.Error{Err: "internal", Detail: err.Error(), Code: output.ExitGeneral})
	}

	ctx, err := parser.Parse(args)
	if err != nil {
		return fail(&cli, &output.Error{
			Err:    "invalid_arguments",
			Detail: err.Error(),
			Hint:   "run 'dfm --help' for usage",
			Code:   output.ExitGeneral,
		})
	}

	if err := ctx.Run(&cli); err != nil {
		var reported *output.Reported
		if errors.As(err, &reported) {
			return reported.Code
		}
		var oErr *output.Error
		if !errors.As(err, &oErr) {
			oErr = &output.Error{Err: "general_error", Detail: err.Error(), Code: output.ExitGeneral}
		}
		return fail(&cli, oErr)
	}
	return output.ExitSuccess
}

// fail prints e as the single stderr JSON object and returns its exit code.
func fail(cli *CLI, e *output.Error) int {
	if err := cli.Printer().PrintError(e); err != nil {
		fmt.Fprintln(os.Stderr, "dfm:", err)
	}
	return e.ExitCode()
}
