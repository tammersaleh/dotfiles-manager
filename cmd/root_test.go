package cmd_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/tammersaleh/dotfiles-manager/cmd"
)

// run drives the CLI in-process and returns exit code, stdout, stderr.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errW bytes.Buffer
	code := cmd.Run(args, &out, &errW)
	return code, out.String(), errW.String()
}

// oneJSONObject asserts s is exactly one line holding one JSON object and
// returns it decoded.
func oneJSONObject(t *testing.T, s string) map[string]any {
	t.Helper()
	if strings.Count(s, "\n") != 1 || !strings.HasSuffix(s, "\n") {
		t.Fatalf("want exactly one line, got %q", s)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not a JSON object: %q: %v", s, err)
	}
	return m
}

func mustParse(t *testing.T, args ...string) *cmd.CLI {
	t.Helper()
	var cli cmd.CLI
	parser, err := kong.New(&cli, kong.Name("dfm"), kong.Exit(func(int) { t.Fatal("unexpected exit") }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(args); err != nil {
		t.Fatal(err)
	}
	return &cli
}

func TestStubs_NotImplemented(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"install", []string{"install"}},
		{"public", []string{"public", ".examplerc"}},
		{"private", []string{"private", ".examplerc"}},
		{"ignore", []string{"ignore", ".examplerc"}},
		{"pull", []string{"pull"}},
		{"pull --no-hooks", []string{"pull", "--no-hooks"}},
		{"status", []string{"status"}},
		{"install --json", []string{"--json", "install"}},
		{"install --quiet", []string{"--quiet", "install"}},
		{"install --dry-run", []string{"--dry-run", "install"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errW := run(t, tt.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if out != "" {
				t.Errorf("stdout should be empty, got %q", out)
			}
			m := oneJSONObject(t, errW)
			if m["error"] != "not_implemented" {
				t.Errorf("error = %v, want not_implemented", m["error"])
			}
			cmdName := tt.args[len(tt.args)-1]
			if strings.HasPrefix(cmdName, "-") || cmdName == ".examplerc" {
				cmdName = tt.args[0]
				if strings.HasPrefix(cmdName, "-") {
					cmdName = tt.args[1]
				}
			}
			if d, _ := m["detail"].(string); !strings.Contains(d, "dfm "+cmdName) {
				t.Errorf("detail = %q should name 'dfm %s'", d, cmdName)
			}
			if h, _ := m["hint"].(string); h == "" {
				t.Error("hint should be present")
			}
		})
	}
}

func TestInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"bogus"}},
		{"unknown flag", []string{"--nope", "install"}},
		{"missing arg", []string{"public"}},
		{"no command", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errW := run(t, tt.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if out != "" {
				t.Errorf("stdout should be empty, got %q", out)
			}
			m := oneJSONObject(t, errW)
			if m["error"] != "invalid_arguments" {
				t.Errorf("error = %v, want invalid_arguments", m["error"])
			}
			if h, _ := m["hint"].(string); !strings.Contains(h, "--help") {
				t.Errorf("hint = %q should point at --help", h)
			}
		})
	}
}

func TestHelp(t *testing.T) {
	code, out, errW := run(t, "--help")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if errW != "" {
		t.Errorf("stderr should be empty, got %q", errW)
	}
	for _, want := range []string{"Usage: dfm", "--root", "--target", "--dry-run", "--json", "--verbose", "--quiet",
		"install", "public", "private", "ignore", "pull", "status", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestGlobalFlags_Parse(t *testing.T) {
	cli := mustParse(t, "--root", "/r", "--target", "/t", "--dry-run", "--json", "--verbose", "--quiet", "install")
	if cli.Root != "/r" || cli.Target != "/t" {
		t.Errorf("root=%q target=%q", cli.Root, cli.Target)
	}
	if !cli.DryRun || !cli.JSON || !cli.Verbose || !cli.Quiet {
		t.Errorf("bools not set: %+v", cli)
	}
}

func TestGlobalFlags_Env(t *testing.T) {
	t.Setenv("DFM_ROOT", "/env/root")
	t.Setenv("DFM_TARGET", "/env/target")
	cli := mustParse(t, "install")
	if cli.Root != "/env/root" || cli.Target != "/env/target" {
		t.Errorf("root=%q target=%q", cli.Root, cli.Target)
	}
	// A flag beats the env var.
	cli = mustParse(t, "--root", "/flag", "install")
	if cli.Root != "/flag" {
		t.Errorf("flag should override env, got %q", cli.Root)
	}
}

func TestGlobalFlags_LazyDefaults(t *testing.T) {
	t.Setenv("DFM_ROOT", "")
	t.Setenv("DFM_TARGET", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	cli := mustParse(t, "install")
	if cli.Root != "" || cli.Target != "" {
		t.Fatalf("parse must not resolve defaults: root=%q target=%q", cli.Root, cli.Target)
	}
	root, err := cli.RootDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "dotfiles"); root != want {
		t.Errorf("RootDir = %q, want %q", root, want)
	}
	target, err := cli.TargetDir()
	if err != nil {
		t.Fatal(err)
	}
	if target != home {
		t.Errorf("TargetDir = %q, want %q", target, home)
	}

	cli = mustParse(t, "--root", "/x", "--target", "/y", "install")
	if r, _ := cli.RootDir(); r != "/x" {
		t.Errorf("RootDir with flag = %q", r)
	}
	if tg, _ := cli.TargetDir(); tg != "/y" {
		t.Errorf("TargetDir with flag = %q", tg)
	}
}
