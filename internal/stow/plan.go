package stow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tammersaleh/dotfiles-manager/internal/output"
)

// Action is one planned change, target-relative. It is also the --json row.
type Action struct {
	Kind    string `json:"action"` // link, unlink, mkdir, rmdir, unfold, refold
	Package string `json:"package,omitempty"`
	Path    string `json:"path"`
	Target  string `json:"target,omitempty"` // link text, for link
	Reason  string `json:"reason,omitempty"` // source_missing, for unlink
}

// Plan is the result of planning a restow. When Conflicts is non-empty the
// plan must not be applied.
type Plan struct {
	Target    string // absolute target directory
	Actions   []Action
	Conflicts []Conflict
	Meta      output.Meta
}

// Restow plans `stow --restow <packages...>` for root into target: unstow
// every package, then stow every package, as one operation. Nothing is
// changed. Returns an *output.Error for missing packages, bad ignore
// patterns, and unreadable trees.
func Restow(root, target string, packages []string) (plan *Plan, err error) {
	rootAbs, err := resolve(root, "root")
	if err != nil {
		return nil, err
	}
	targetAbs, err := resolve(target, "target")
	if err != nil {
		return nil, err
	}
	stowPath, err := filepath.Rel(targetAbs, rootAbs)
	if err != nil {
		return nil, &output.Error{Err: "bad_layout", Detail: err.Error(), Path: root}
	}
	if err := CheckPackages(root, rootAbs, packages); err != nil {
		return nil, err
	}

	p := newPlanner(targetAbs, stowPath)
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *output.Error:
				plan, err = nil, e
			case stowError:
				plan, err = nil, &output.Error{Err: "plan_failed", Detail: e.msg, Path: target}
			default:
				panic(r)
			}
		}
	}()
	for _, pkg := range packages {
		p.planUnstow(pkg)
	}
	for _, pkg := range packages {
		p.planStow(pkg)
	}
	return p.plan(), nil
}

func resolve(dir, what string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", &output.Error{Err: what + "_missing", Detail: err.Error(), Hint: "pass --" + what, Path: dir}
	}
	return abs, nil
}

// plan turns the surviving task queue into Actions and counts.
func (p *planner) plan() *Plan {
	pl := &Plan{Target: p.target, Conflicts: p.conflicts}
	for _, t := range p.tasks {
		if t.action == actSkip {
			continue
		}
		var a Action
		switch t.typ {
		case typeMarker:
			if t.ref.action == actSkip {
				continue
			}
			a = Action{Kind: t.kind, Package: t.pkg, Path: t.path}
			if t.kind == "unfold" {
				pl.Meta.Unfolded++
			} else {
				pl.Meta.Refolded++
			}
		case typeLink:
			if t.action == actCreate {
				a = Action{Kind: "link", Package: t.pkg, Path: t.path, Target: t.source}
				pl.Meta.Created++
			} else {
				a = Action{Kind: "unlink", Package: t.pkg, Path: t.path, Reason: t.reason}
				pl.Meta.Removed++
			}
		case typeDir:
			if t.action == actCreate {
				a = Action{Kind: "mkdir", Path: t.path}
			} else {
				a = Action{Kind: "rmdir", Path: t.path}
			}
		}
		pl.Actions = append(pl.Actions, a)
	}
	return pl
}

// Apply performs the plan's actions in order. Refuses when the plan has
// conflicts. The optional trace callback sees every filesystem operation
// before it runs (for --verbose).
func (pl *Plan) Apply(trace func(string)) error {
	if len(pl.Conflicts) > 0 {
		return errors.New("plan has conflicts")
	}
	for _, a := range pl.Actions {
		path := filepath.Join(pl.Target, a.Path)
		var err error
		switch a.Kind {
		case "link":
			if trace != nil {
				trace(fmt.Sprintf("symlink %s -> %s", path, a.Target))
			}
			err = os.Symlink(a.Target, path)
		case "unlink":
			if trace != nil {
				trace("unlink " + path)
			}
			err = os.Remove(path)
		case "mkdir":
			if trace != nil {
				trace("mkdir " + path)
			}
			err = os.Mkdir(path, 0o777)
		case "rmdir":
			if trace != nil {
				trace("rmdir " + path)
			}
			err = os.Remove(path)
		}
		if err != nil {
			return &output.Error{Err: "apply_failed", Detail: err.Error(), Path: a.Path,
				Hint: "the target tree is partially updated; rerun dfm install after fixing the cause"}
		}
	}
	return nil
}

// CheckPackages returns package_missing unless every package is a directory
// under rootAbs. root is the path as the user gave it, for the message.
func CheckPackages(root, rootAbs string, packages []string) error {
	for _, pkg := range packages {
		dir := filepath.Join(rootAbs, pkg)
		if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
			return &output.Error{
				Err:    "package_missing",
				Detail: fmt.Sprintf("%s does not contain package %s", root, pkg),
				Hint:   "clone the package repository into the root first",
				Path:   dir,
			}
		}
	}
	return nil
}
