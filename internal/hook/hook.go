// Package hook runs a package's post-pull.sh. A hook is a plain executable:
// dfm gives it the package directory as cwd, passes its stdout and stderr
// through, and reports its exit code. Nothing here knows about packages or
// output modes; cmd decides what to run and where its output goes.
package hook

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// Executable reports whether path exists and has any execute bit set. A
// missing path is (false, nil); any other stat failure is returned.
func Executable(path string) (bool, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0, nil
}

// Run executes path with cwd set to dir, writing its stdout and stderr to
// the given writers, and returns the process exit code. A hook that could
// not be started at all returns -1 and the error.
func Run(path, dir string, stdout, stderr io.Writer) (int, error) {
	cmd := exec.Command(path)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}
