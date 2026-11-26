package apply

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

// writeAtomic writes data to a temporary file next to path and renames it over path, creating
// parent directories (0755). check, when not nil, runs right before the rename; its error stops
// the write.
func writeAtomic(path string, data []byte, mode os.FileMode, next string, check func() error) error {
	dir := filepath.Dir(path)
	if check != nil {
		// Before the folder is created: a blocker that came back during the run (an old file
		// where a folder is needed) is a "changed while livery was running" Finding, not an
		// environment error from MkdirAll.
		if err := check(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return exit.Environmentf("%s: cannot create the directory: %v; check its permissions, then run %s", dir, err, next)
	}
	err := paths.WriteAtomic(path, data, mode, "."+filepath.Base(path)+".livery-*", check)
	var ce *paths.CheckError
	if errors.As(err, &ce) {
		return ce.Err
	}
	if err != nil {
		var cr *paths.CreateError
		if errors.As(err, &cr) {
			err = cr.Err
		}
		return exit.Environmentf("%s: cannot write: %v; check its permissions, then run %s", path, err, next)
	}
	return nil
}

// removeFile removes path, then the directories it leaves empty inside its root (see pruneDirs;
// project is the project root in project scope, else ""). check, when not nil, runs right before
// the removal; its error stops it.
func removeFile(path string, roots []string, project, next string, check func() error) error {
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return exit.Environmentf("%s: cannot remove: %v; check its permissions, then run %s", path, err, next)
	}
	pruneDirs(path, roots, project)
	return nil
}

// pruneDirs removes the empty parent directories of path upward, only directories strictly
// inside the innermost root that contains path. The root itself and anything outside every root
// stay. It stops at the first parent that is not a real directory: a link is never removed,
// whatever it points to.
//
// In project scope (project set) it also removes nothing when any element strictly below the
// project root, a harness folder included, is a link.
func pruneDirs(path string, roots []string, project string) {
	root := innermostRoot(path, roots)
	if root == "" || linkedElement(filepath.Dir(path), roots, "") != "" ||
		project != "" && linkedElement(filepath.Dir(path), nil, project) != "" {
		// Behind a link: the directories are not livery's, and the link itself is never removed.
		return
	}
	for dir := filepath.Dir(path); inside(root, dir); dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err != nil || !fi.IsDir() { // a link, or not a directory
			return
		}
		if os.Remove(dir) != nil { // not empty, or not ours to remove
			return
		}
	}
}

// innermostRoot returns the longest root in roots that path is strictly below, or "".
func innermostRoot(path string, roots []string) string {
	root := ""
	for _, r := range roots {
		r = filepath.Clean(r)
		if inside(r, path) && len(r) > len(root) {
			root = r
		}
	}
	return root
}

// inside reports whether p is strictly below root.
func inside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel)
}
