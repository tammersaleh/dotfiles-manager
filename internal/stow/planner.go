// Package stow reimplements the parts of GNU Stow 2.4.1 that dfm depends
// on: --restow of the public and private packages into the target, with
// tree folding, unfolding, refolding, dangling-link cleanup, ignore lists,
// and conflict detection. See SPEC.md "Stow semantics dfm must reproduce".
//
// The planner is a deliberate port of Stow.pm's algorithm, including its
// task queue and the virtual filesystem view (is_a_link, is_a_dir,
// is_a_node) that lets later planning steps see earlier planned changes.
// Anywhere this file reads oddly, check the Perl: parity with stow is the
// contract, not elegance.
package stow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Packages is the fixed package list, in stow order.
var Packages = []string{"public", "private"}

type taskAction int

const (
	actCreate taskAction = iota
	actRemove
	actSkip
)

type taskType int

const (
	typeLink taskType = iota
	typeDir
	typeMarker // unfold/refold row; no filesystem effect
)

// task is one entry in the plan's task queue. Paths are relative to the
// target, as stow builds them with cwd = target.
type task struct {
	action taskAction
	typ    taskType
	path   string
	source string // link text for link tasks
	pkg    string // package the row is attributed to
	reason string // "source_missing" for dangling-link removals
	kind   string // marker kind: "unfold" or "refold"
	ref    *task  // marker: the dir task whose survival decides the marker's
}

// Conflict is one reason the plan must not run.
type Conflict struct {
	Package string
	Path    string // target-relative
	Detail  string
	Hint    string
}

// stowError is a fatal planning failure (unreadable directory, unreadable
// link). It is raised with panic and recovered by Restow so the port can
// keep Stow.pm's control flow.
type stowError struct{ msg string }

func (e stowError) Error() string { return e.msg }

// planner ports the Stow object: one plan for one target.
type planner struct {
	target   string // absolute, symlinks resolved
	stowPath string // root relative to target ("dotfiles" in production)

	tasks       []*task
	linkTaskFor map[string]*task
	dirTaskFor  map[string]*task
	conflicts   []Conflict
	ignores     map[string]*IgnoreList // keyed by package dir relative to target
}

func newPlanner(target, stowPath string) *planner {
	return &planner{
		target:      target,
		stowPath:    stowPath,
		linkTaskFor: map[string]*task{},
		dirTaskFor:  map[string]*task{},
		ignores:     map[string]*IgnoreList{},
	}
}

func (p *planner) fail(format string, args ...any) {
	panic(stowError{fmt.Sprintf(format, args...)})
}

// --- raw filesystem, relative to the target (Perl -l, -d, -e, readlink, opendir)

func (p *planner) abs(rel string) string { return filepath.Join(p.target, rel) }

func (p *planner) fsIsLink(rel string) bool {
	fi, err := os.Lstat(p.abs(rel))
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

func (p *planner) fsIsDir(rel string) bool {
	fi, err := os.Stat(p.abs(rel))
	return err == nil && fi.IsDir()
}

func (p *planner) fsExists(rel string) bool {
	_, err := os.Stat(p.abs(rel))
	return err == nil
}

func (p *planner) fsReadlink(rel string) string {
	dest, err := os.Readlink(p.abs(rel))
	if err != nil {
		p.fail("could not readlink %s (%v)", rel, err)
	}
	return dest
}

func (p *planner) fsReaddir(rel string) []string {
	entries, err := os.ReadDir(p.abs(rel))
	if err != nil {
		p.fail("cannot read directory: %s (%v)", rel, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names) // Perl sort: bytewise
	return names
}

// --- path helpers (Stow::Util)

// joinPaths is Stow::Util::join_paths: lexical join, "./" and "x/.."
// removed, no symlink resolution.
func joinPaths(parts ...string) string {
	var nonEmpty []string
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return filepath.Join(nonEmpty...)
}

// parent is Stow::Util::parent: everything before the last slash, or "".
func parent(path string) string {
	elts := strings.Split(path, "/")
	return strings.Join(elts[:len(elts)-1], "/")
}

// --- plan entry points

func (p *planner) planUnstow(pkg string) {
	p.unstowContents(pkg, ".", ".")
}

func (p *planner) planStow(pkg string) {
	p.stowContents(p.stowPath, pkg, ".", ".")
}

// --- ignore

func (p *planner) ignored(stowPath, pkg, target string) bool {
	pkgDir := joinPaths(stowPath, pkg)
	l, ok := p.ignores[pkgDir]
	if !ok {
		var err error
		l, err = LoadIgnoreList(p.abs(pkgDir))
		if err != nil {
			panic(err) // *output.Error; recovered by Restow
		}
		p.ignores[pkgDir] = l
	}
	return l.Match(target)
}

// --- stow

func (p *planner) shouldSkipTarget(target string) bool {
	if target == p.stowPath {
		return true
	}
	if p.markedStowDir(target) {
		return true
	}
	return p.fsExists(joinPaths(target, ".nonstow"))
}

func (p *planner) markedStowDir(dir string) bool {
	return p.fsExists(joinPaths(dir, ".stow"))
}

func (p *planner) stowContents(stowPath, pkg, pkgSubdir, targetSubdir string) {
	if p.shouldSkipTarget(pkgSubdir) {
		return
	}
	pkgPathFromCwd := joinPaths(stowPath, pkg, pkgSubdir)
	if !p.isANode(targetSubdir) {
		p.fail("stow_contents() called with non-directory target: %s", targetSubdir)
	}
	for _, node := range p.fsReaddir(pkgPathFromCwd) {
		packageNodePath := joinPaths(pkgSubdir, node)
		targetNodePath := joinPaths(targetSubdir, node)
		if p.ignored(stowPath, pkg, targetNodePath) {
			continue
		}
		p.stowNode(stowPath, pkg, packageNodePath, targetNodePath)
	}
}

func (p *planner) stowNode(stowPath, pkg, pkgSubpath, targetSubpath string) {
	pkgPathFromCwd := joinPaths(stowPath, pkg, pkgSubpath)

	// Absolute symlinks in the package cannot be unstowed, so stow refuses.
	if p.fsIsLink(pkgPathFromCwd) {
		if dest := p.fsReadlink(pkgPathFromCwd); strings.HasPrefix(dest, "/") {
			p.conflict(pkg, targetSubpath,
				fmt.Sprintf("source is an absolute symlink %s => %s", pkgPathFromCwd, dest),
				"make the symlink inside the package relative")
			return
		}
	}

	level := strings.Count(pkgSubpath, "/")
	linkDest := joinPaths(strings.Repeat("../", level), pkgPathFromCwd)

	switch {
	case p.isALink(targetSubpath):
		existingLinkDest := p.readALink(targetSubpath)
		if existingLinkDest == "" {
			p.fail("Could not read link: %s", targetSubpath)
		}
		existingPkgPath, existingStowPath, existingPkg := p.findStowedPath(targetSubpath, existingLinkDest)
		if existingPkgPath == "" {
			p.conflict(pkg, targetSubpath,
				fmt.Sprintf("%s is a symlink to %s, which is not inside the dotfiles root", targetSubpath, existingLinkDest),
				adoptHint(pkg, targetSubpath))
			return
		}
		if !p.isANode(existingPkgPath) {
			// The existing link is dangling: replace it.
			p.doUnlink(targetSubpath, existingPkg, "source_missing")
			p.doLink(linkDest, targetSubpath, pkg)
			return
		}
		switch {
		case existingLinkDest == linkDest:
			// Already points where we want.
		case p.isADir(joinPaths(parent(targetSubpath), existingLinkDest)) &&
			p.isADir(joinPaths(parent(targetSubpath), linkDest)):
			// Both the existing link and the new one are directories: unfold.
			p.doUnlink(targetSubpath, existingPkg, "")
			dir := p.doMkdir(targetSubpath)
			p.addMarker("unfold", existingPkg, targetSubpath, dir)
			p.stowContents(existingStowPath, existingPkg, pkgSubpath, targetSubpath)
			p.stowContents(stowPath, pkg, pkgSubpath, targetSubpath)
		default:
			p.conflict(pkg, targetSubpath,
				fmt.Sprintf("%s is already linked to %s, which belongs to another package", targetSubpath, existingLinkDest),
				"remove it and rerun")
		}

	case p.isANode(targetSubpath):
		if p.isADir(targetSubpath) {
			if !p.fsIsDir(pkgPathFromCwd) {
				p.conflict(pkg, targetSubpath,
					fmt.Sprintf("%s is a directory but %s is not", targetSubpath, pkgPathFromCwd),
					adoptHint(pkg, targetSubpath))
			} else {
				p.stowContents(stowPath, pkg, pkgSubpath, targetSubpath)
			}
		} else {
			p.conflict(pkg, targetSubpath,
				fmt.Sprintf("%s exists and is not a dotfiles symlink", targetSubpath),
				adoptHint(pkg, targetSubpath))
		}

	default:
		p.doLink(linkDest, targetSubpath, pkg)
	}
}

func adoptHint(pkg, path string) string {
	return fmt.Sprintf("dfm %s %s to adopt it, or remove it and rerun", pkg, path)
}

// --- unstow

func (p *planner) unstowContents(pkg, pkgSubdir, targetSubdir string) {
	if p.shouldSkipTarget(targetSubdir) {
		return
	}
	pkgPathFromCwd := joinPaths(p.stowPath, pkg, pkgSubdir)
	if !p.fsIsDir(pkgPathFromCwd) {
		p.fail("unstow_contents() called with non-directory path: %s", pkgPathFromCwd)
	}
	if !p.isANode(targetSubdir) {
		p.fail("unstow_contents() called with invalid target: %s", targetSubdir)
	}
	for _, node := range p.fsReaddir(pkgPathFromCwd) {
		targetNodePath := joinPaths(targetSubdir, node)
		if p.ignored(p.stowPath, pkg, targetNodePath) {
			continue
		}
		packageNodePath := joinPaths(pkgSubdir, node)
		p.unstowNode(pkg, packageNodePath, targetNodePath)
	}
	if p.fsIsDir(targetSubdir) {
		p.cleanupInvalidLinks(targetSubdir)
	}
}

func (p *planner) unstowNode(pkg, pkgSubpath, targetSubpath string) {
	switch {
	case p.isALink(targetSubpath):
		p.unstowLinkNode(pkg, pkgSubpath, targetSubpath)
	case p.isADir(targetSubpath):
		// Stow.pm tests the raw filesystem (-d) here, which follows a folded
		// link that an earlier package's unstow already scheduled for
		// removal, and then dies with "unstow_contents() called with invalid
		// target". Using the planned view instead skips the vanished link, so
		// the stow phase unfolds the directory as plain `stow` would.
		if !p.fsIsDir(joinPaths(p.stowPath, pkg, pkgSubpath)) {
			// The target is a directory but the package entry is not. Stow
			// dies here too; dfm leaves it for the stow phase, which reports
			// it as a conflict. Nothing changes either way.
			return
		}
		p.unstowContents(pkg, pkgSubpath, targetSubpath)
		// Removing this package's links may have left the directory holding
		// links from a single package: fold it.
		if parentInPkg := p.foldable(targetSubpath); parentInPkg != "" {
			p.foldTree(targetSubpath, parentInPkg)
		}
	}
}

func (p *planner) unstowLinkNode(pkg, pkgSubpath, targetSubpath string) {
	linkDest := p.readALink(targetSubpath)
	if linkDest == "" {
		p.fail("Could not read link: %s", targetSubpath)
	}
	if strings.HasPrefix(linkDest, "/") {
		return // stow warns and ignores absolute symlinks
	}
	existingPkgPath, _, existingPkg := p.findStowedPath(targetSubpath, linkDest)
	if existingPkgPath == "" {
		return // not ours; leave it alone
	}
	pkgPathFromCwd := joinPaths(p.stowPath, pkg, pkgSubpath)
	if p.fsExists(existingPkgPath) {
		if existingPkgPath == pkgPathFromCwd {
			p.doUnlink(targetSubpath, pkg, "")
		}
		return
	}
	p.doUnlink(targetSubpath, existingPkg, "source_missing")
}

// linkOwnedByPackage returns the package a link belongs to, or "".
func (p *planner) linkOwnedByPackage(targetSubpath, linkDest string) string {
	_, _, pkg := p.findStowedPath(targetSubpath, linkDest)
	return pkg
}

// findStowedPath evaluates a link's text relative to its directory and
// returns (package path from target, stow path, package) when it points
// into a stow directory, else three empty strings.
func (p *planner) findStowedPath(targetSubpath, linkDest string) (string, string, string) {
	if strings.HasPrefix(linkDest, "/") {
		return "", "", ""
	}
	pkgPathFromCwd := joinPaths(parent(targetSubpath), linkDest)
	if pkg, _ := p.linkDestWithinStowDir(pkgPathFromCwd); pkg != "" {
		return pkgPathFromCwd, p.stowPath, pkg
	}
	if stowPath, pkg := p.findContainingMarkedStowDir(pkgPathFromCwd); stowPath != "" {
		return pkgPathFromCwd, stowPath, pkg
	}
	return "", "", ""
}

func (p *planner) linkDestWithinStowDir(linkDest string) (string, string) {
	prefix := p.stowPath + "/"
	if !strings.HasPrefix(linkDest, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(linkDest, prefix)
	dirs := strings.Split(rest, "/")
	return dirs[0], strings.Join(dirs[1:], "/")
}

func (p *planner) findContainingMarkedStowDir(pkgPathFromCwd string) (string, string) {
	segments := strings.Split(pkgPathFromCwd, "/")
	for i := range segments {
		prefix := joinPaths(segments[:i+1]...)
		if p.markedStowDir(prefix) {
			if i == len(segments)-1 {
				p.fail("find_stowed_path() called directly on stow dir")
			}
			return prefix, segments[i+1]
		}
	}
	return "", ""
}

// cleanupInvalidLinks removes links in dir that point into a stow
// directory at a path that no longer exists. Note it reads the real
// filesystem, not the planned view, exactly as stow does.
func (p *planner) cleanupInvalidLinks(dir string) {
	if !p.fsIsDir(dir) {
		p.fail("cleanup_invalid_links() called with a non-directory: %s", dir)
	}
	for _, node := range p.fsReaddir(dir) {
		nodePath := joinPaths(dir, node)
		if !p.fsIsLink(nodePath) {
			continue
		}
		if _, ok := p.linkTaskFor[nodePath]; ok {
			continue // already scheduled
		}
		linkDest := p.fsReadlink(nodePath)
		if p.fsExists(joinPaths(dir, linkDest)) {
			continue
		}
		if owner := p.linkOwnedByPackage(nodePath, linkDest); owner != "" {
			p.doUnlink(nodePath, owner, "source_missing")
		}
	}
}

// foldable returns the package-relative directory targetSubdir can be
// folded into (relative to targetSubdir's parent), or "" when it cannot:
// every surviving child must be a link and they must all point into the
// same package directory.
func (p *planner) foldable(targetSubdir string) string {
	parentInPkg := ""
	for _, node := range p.fsReaddir(targetSubdir) {
		targetNodePath := joinPaths(targetSubdir, node)
		if !p.isANode(targetNodePath) {
			continue // scheduled for removal
		}
		if !p.isALink(targetNodePath) {
			return ""
		}
		linkDest := p.readALink(targetNodePath)
		if linkDest == "" {
			p.fail("Could not read link %s", targetNodePath)
		}
		newParent := parent(linkDest)
		if parentInPkg == "" {
			parentInPkg = newParent
		} else if parentInPkg != newParent {
			return ""
		}
	}
	if parentInPkg == "" {
		return ""
	}
	parentInPkg = strings.TrimPrefix(parentInPkg, "../")
	if p.linkOwnedByPackage(targetSubdir, parentInPkg) != "" {
		return parentInPkg
	}
	return ""
}

func (p *planner) foldTree(targetSubdir, pkgSubpath string) {
	pkg := p.linkOwnedByPackage(targetSubdir, pkgSubpath)
	for _, node := range p.fsReaddir(targetSubdir) {
		path := joinPaths(targetSubdir, node)
		if !p.isANode(path) {
			continue
		}
		p.doUnlink(path, p.linkOwnedByPackage(path, p.readALink(path)), "")
	}
	dir := p.doRmdir(targetSubdir)
	p.doLink(pkgSubpath, targetSubdir, pkg)
	p.addMarker("refold", pkg, targetSubdir, dir)
}

func (p *planner) conflict(pkg, path, detail, hint string) {
	p.conflicts = append(p.conflicts, Conflict{Package: pkg, Path: path, Detail: detail, Hint: hint})
}

// --- virtual filesystem: the real tree overlaid with planned tasks

func (p *planner) linkTaskAction(path string) (taskAction, bool) {
	t, ok := p.linkTaskFor[path]
	if !ok {
		return 0, false
	}
	return t.action, true
}

func (p *planner) dirTaskAction(path string) (taskAction, bool) {
	t, ok := p.dirTaskFor[path]
	if !ok {
		return 0, false
	}
	return t.action, true
}

func (p *planner) parentLinkScheduledForRemoval(targetPath string) bool {
	prefix := ""
	for _, part := range strings.Split(targetPath, "/") {
		if part == "" {
			continue
		}
		prefix = joinPaths(prefix, part)
		if t, ok := p.linkTaskFor[prefix]; ok && t.action == actRemove {
			return true
		}
	}
	return false
}

func (p *planner) isALink(targetPath string) bool {
	if a, ok := p.linkTaskAction(targetPath); ok {
		return a == actCreate
	}
	if p.fsIsLink(targetPath) {
		return !p.parentLinkScheduledForRemoval(targetPath)
	}
	return false
}

func (p *planner) isADir(targetPath string) bool {
	if a, ok := p.dirTaskAction(targetPath); ok {
		return a == actCreate
	}
	if p.parentLinkScheduledForRemoval(targetPath) {
		return false
	}
	return p.fsIsDir(targetPath)
}

func (p *planner) isANode(targetPath string) bool {
	laction, hasL := p.linkTaskAction(targetPath)
	daction, hasD := p.dirTaskAction(targetPath)
	switch {
	case hasL && laction == actRemove:
		if hasD && daction == actRemove {
			p.fail("removing link and dir: %s", targetPath)
		}
		return hasD && daction == actCreate // unfolding
	case hasL && laction == actCreate:
		if hasD && daction == actCreate {
			p.fail("creating link and dir: %s", targetPath)
		}
		return true
	case hasD:
		return daction == actCreate
	}
	if p.parentLinkScheduledForRemoval(targetPath) {
		return false
	}
	return p.fsExists(targetPath)
}

func (p *planner) readALink(link string) string {
	if t, ok := p.linkTaskFor[link]; ok {
		switch t.action {
		case actCreate:
			return t.source
		case actRemove:
			p.fail("read_a_link() passed a path that is scheduled for removal: %s", link)
		}
	} else if p.fsIsLink(link) {
		return p.fsReadlink(link)
	}
	p.fail("read_a_link() passed a non-link path: %s", link)
	return ""
}

// --- task queue

func (p *planner) doLink(linkDest, linkSrc, pkg string) {
	if t, ok := p.dirTaskFor[linkSrc]; ok && t.action == actCreate {
		p.fail("new link (%s => %s) clashes with planned new directory", linkSrc, linkDest)
	}
	if t, ok := p.linkTaskFor[linkSrc]; ok {
		switch t.action {
		case actCreate:
			if t.source != linkDest {
				p.fail("new link clashes with planned new link: %s => %s", t.path, t.source)
			}
			return
		case actRemove:
			if t.source == linkDest {
				// Removing and recreating the same link is a no-op.
				t.action = actSkip
				delete(p.linkTaskFor, linkSrc)
				return
			}
		}
	}
	t := &task{action: actCreate, typ: typeLink, path: linkSrc, source: linkDest, pkg: pkg}
	p.tasks = append(p.tasks, t)
	p.linkTaskFor[linkSrc] = t
}

func (p *planner) doUnlink(file, pkg, reason string) {
	if t, ok := p.linkTaskFor[file]; ok {
		if t.action == actCreate {
			// Creating then removing is a no-op.
			t.action = actSkip
			delete(p.linkTaskFor, file)
		}
		return
	}
	source := p.fsReadlink(file)
	t := &task{action: actRemove, typ: typeLink, path: file, source: source, pkg: pkg, reason: reason}
	p.tasks = append(p.tasks, t)
	p.linkTaskFor[file] = t
}

// doMkdir returns the dir task the marker should track: the new create task,
// or the remove task it cancelled.
func (p *planner) doMkdir(dir string) *task {
	if t, ok := p.linkTaskFor[dir]; ok && t.action == actCreate {
		p.fail("new dir clashes with planned new link (%s => %s)", t.path, t.source)
	}
	if t, ok := p.dirTaskFor[dir]; ok {
		if t.action == actRemove {
			t.action = actSkip
			delete(p.dirTaskFor, dir)
		}
		return t
	}
	t := &task{action: actCreate, typ: typeDir, path: dir}
	p.tasks = append(p.tasks, t)
	p.dirTaskFor[dir] = t
	return t
}

func (p *planner) doRmdir(dir string) *task {
	if t, ok := p.linkTaskFor[dir]; ok {
		p.fail("rmdir clashes with planned operation: link %s => %s", t.path, t.source)
	}
	if _, ok := p.dirTaskFor[dir]; ok {
		// Stow.pm reads link_task_for here by mistake and dies; a second
		// dir task for one path is an internal error either way.
		p.fail("rmdir clashes with planned dir task for %s", dir)
	}
	t := &task{action: actRemove, typ: typeDir, path: dir}
	p.tasks = append(p.tasks, t)
	p.dirTaskFor[dir] = t
	return t
}

func (p *planner) addMarker(kind, pkg, path string, ref *task) {
	p.tasks = append(p.tasks, &task{action: actCreate, typ: typeMarker, kind: kind, pkg: pkg, path: path, ref: ref})
}
