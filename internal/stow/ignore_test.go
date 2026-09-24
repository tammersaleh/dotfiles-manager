package stow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

func TestIgnoreList_Defaults(t *testing.T) {
	l, err := LoadIgnoreList(t.TempDir()) // no .stow-local-ignore: built-in list
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want bool
	}{
		{".git", true},
		{".gitignore", true},
		{".gitmodules", true},
		{".config/.git", true}, // segment patterns match at any depth
		{"README.md", true},
		{"README", true},
		{"LICENSE.txt", true},
		{"COPYING", true},
		{".example/README.md", false}, // ^/README.* is anchored to the top level
		{".example/backup~", true},
		{".example/#autosave#", true},
		{".example/.#lock", true},
		{".example/file,v", true},
		{"CVS", true},
		{".example/CVS", true},
		{".stow-local-ignore", true},
		{".config/.stow-local-ignore", false}, // only the top-level one
		{".example/keep.swp", false},
		{".examplerc", false},
		{".gitattributes", false}, // not in stow's defaults, only in the public list
	}
	for _, tt := range tests {
		if got := l.Match(tt.path); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIgnoreList_LocalFile(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		`\.gitignore`,
		"",
		"# a comment",
		`   tags.*   `,
		`skip\.me   # trailing comment`,
		`\#hash`,
		`\.claude/settings\.local\.json`,
		`sub/dir`,
		`\.gitignore`, // duplicate
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, LocalIgnoreFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadIgnoreList(dir)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want bool
	}{
		{".gitignore", true},
		{".config/.gitignore", true},
		{"tags", true},
		{"tags.lock", true},
		{".config/tags", true},
		{"skip.me", true},
		{"#hash", true},
		{".claude/settings.local.json", true},
		{".claude/deeper/settings.local.json", false}, // path pattern is anchored
		{"x/.claude/settings.local.json", true},       // (^|/) lets it match at any depth
		{"settings.local.json", false},
		{"sub/dir", true},
		{"sub/dir/file", true}, // (/|$) matches a directory prefix
		{"sub", false},
		{".stow-local-ignore", true}, // always added
		{"README.md", false},         // defaults are replaced, not merged
		{".git", false},
		{"comment", false},
	}
	for _, tt := range tests {
		if got := l.Match(tt.path); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestIgnoreList_BadPattern(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, LocalIgnoreFile)
	if err := os.WriteFile(file, []byte("ok\n\n# c\n(?<=look)behind\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadIgnoreList(dir)
	var oErr *output.Error
	if !errors.As(err, &oErr) {
		t.Fatalf("want *output.Error, got %v", err)
	}
	if oErr.Err != "bad_ignore_pattern" {
		t.Errorf("code = %s", oErr.Err)
	}
	if oErr.Path != file {
		t.Errorf("path = %s, want %s", oErr.Path, file)
	}
	if !strings.HasPrefix(oErr.Detail, file+":4: (?<=look)behind: ") {
		t.Errorf("detail = %q", oErr.Detail)
	}
	if oErr.ExitCode() != output.ExitGeneral {
		t.Errorf("exit = %d", oErr.ExitCode())
	}
}

func TestJoinPaths(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{[]string{".", "foo"}, "foo"},
		{[]string{".", "."}, "."},
		{[]string{"", "dotfiles/public/x"}, "dotfiles/public/x"},
		{[]string{"../", "dotfiles/public/.config/a"}, "../dotfiles/public/.config/a"},
		{[]string{"../../", "dotfiles/public/a/b"}, "../../dotfiles/public/a/b"},
		{[]string{".config", "../dotfiles/public/.config/a"}, "dotfiles/public/.config/a"},
		{[]string{".config/a", "../../dotfiles/public/.config/a/b"}, "dotfiles/public/.config/a/b"},
		{[]string{"a", "../.."}, ".."},
	}
	for _, tt := range tests {
		if got := joinPaths(tt.parts...); got != tt.want {
			t.Errorf("joinPaths(%q) = %q, want %q", tt.parts, got, tt.want)
		}
	}
}

func TestParent(t *testing.T) {
	tests := map[string]string{
		"foo":                  "",
		".config/a":            ".config",
		".config/a/b":          ".config/a",
		"../dotfiles/public/x": "../dotfiles/public",
	}
	for in, want := range tests {
		if got := parent(in); got != want {
			t.Errorf("parent(%q) = %q, want %q", in, got, want)
		}
	}
}
