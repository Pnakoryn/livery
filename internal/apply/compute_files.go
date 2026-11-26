package apply

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

// WantedFiles returns the plans' files by cleaned path, with the paths in plan order. A path
// wanted twice is an error. It reads no disk.
func WantedFiles(plans []adapter.Plan) (map[string]adapter.File, []string, error) {
	return wantedFiles(plans, true)
}

// personalHint is the part of a hint that points to the personal layer; a project has none.
func personalHint(project string) string {
	if project != "" {
		return ""
	}
	return " or your personal layer"
}

func wantedFiles(plans []adapter.Plan, personal bool) (map[string]adapter.File, []string, error) {
	hint := ""
	if personal {
		hint = personalHint("")
	}
	wanted := map[string]adapter.File{}
	var order []string
	for _, p := range plans {
		for _, f := range p.Files {
			path := filepath.Clean(f.Path)
			if prev, ok := wanted[path]; ok {
				return nil, nil, exit.Findingf("%s: wanted twice, by %s and by %s; remove one of them from the shared repository%s", path, prev.Origin, f.Origin, hint)
			}
			wanted[path] = f
			order = append(order, path)
		}
	}
	return wanted, order, nil
}

func computeFiles(plans []adapter.Plan, old lock.Lock, scope lock.Scope, res *Result) ([]Action, error) {
	wanted, order, err := wantedFiles(plans, scope.Project == "")
	if err != nil {
		return nil, err
	}
	owned := map[string]string{}
	for p, sum := range old.Files {
		owned[scope.Load(p)] = sum
	}

	var actions []Action
	for _, path := range order {
		f := wanted[path]
		a := Action{Path: path, New: f.Data, Exec: f.Exec, Origin: f.Origin}
		newSum := lock.Sum(f.Data)
		disk, present, blocked, isExec, err := readFile(path, res.linkRoots(), res.bound())
		if err != nil {
			return nil, err
		}
		if blocked && clearedByRun(path, wanted, owned, res) {
			blocked = false
			a.afterRemove = true
		}
		oldSum, isOwned := owned[path]
		st := state{Blocked: blocked, Present: present, Owned: isOwned}
		if present {
			a.Old = disk
			diskSum := lock.Sum(disk)
			st.MatchesWanted = diskSum == newSum
			st.MatchesRecorded = isOwned && diskSum == oldSum
		}
		modeOnly := st.MatchesWanted && isExec != f.Exec
		if modeOnly && !isOwned {
			// Same bytes, other executable bit, not livery's: it is not the wanted file, and
			// livery does not change a file it does not own.
			st.MatchesWanted = false
		}
		d := decide(true, st)
		a.Op, _ = d.op(Write, Remove)
		a.Missing = !present && !blocked && isOwned
		if modeOnly && isOwned {
			// Same bytes, other executable bit: rewrite them with the wanted mode.
			a.Op = Write
		}
		if sum, ok := d.sum(newSum, oldSum); ok {
			res.Lock.Files[key(scope, path)] = sum
		}
		actions = append(actions, a)
	}

	for path, oldSum := range owned {
		if _, ok := wanted[path]; ok {
			continue
		}
		disk, present, blocked, _, err := readFile(path, res.linkRoots(), res.bound())
		if err != nil {
			return nil, err
		}
		// Blocked: something else (a directory, a symbolic link) took the place of the file: it
		// is the user's now. Absent: already gone, nothing to do and no longer owned.
		st := state{Blocked: blocked, Present: present, Owned: true}
		if present {
			st.MatchesRecorded = lock.Sum(disk) == oldSum
		}
		if op, ok := decide(false, st).op(Write, Remove); ok {
			actions = append(actions, Action{Op: op, Path: path, Old: disk})
		}
	}
	return actions, nil
}

// errNotCleared stops the walk of clearedByRun at an entry this run does not remove.
var errNotCleared = errors.New("not removed by this run")

// removedByRun reports whether this run removes the file at path: livery owns it, it is not
// wanted any more, and it is a regular file (not behind a link) as recorded.
func removedByRun(path string, wanted map[string]adapter.File, owned map[string]string, res *Result) bool {
	oldSum, ok := owned[path]
	if !ok {
		return false
	}
	if _, ok := wanted[path]; ok {
		return false
	}
	disk, present, blocked, _, err := readFile(path, res.linkRoots(), res.bound())
	return err == nil && present && !blocked && lock.Sum(disk) == oldSum
}

// clearedByRun reports whether what blocks the wanted file at path is removed by this run, so
// the file can be written after the removals: (a) the nearest existing ancestor of path is a
// regular file this run removes (a file that becomes a folder); (b) path is a directory whose
// every entry below it, at any depth, is a regular file this run removes, and that holds no
// empty folder (a folder that becomes a file). A link anywhere never counts.
func clearedByRun(path string, wanted map[string]adapter.File, owned map[string]string, res *Result) bool {
	if linkedElement(path, res.linkRoots(), res.bound()) != "" {
		return false
	}
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, syscall.ENOTDIR):
		for p := filepath.Dir(path); filepath.Dir(p) != p; p = filepath.Dir(p) {
			fi, err := os.Lstat(p)
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				continue
			}
			return err == nil && fi.Mode().IsRegular() && removedByRun(p, wanted, owned, res)
		}
		return false
	case err != nil || !fi.IsDir():
		return false
	}
	var dirs []string
	holds := map[string]bool{} // directories with a removed file below them
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		if !d.Type().IsRegular() || !removedByRun(p, wanted, owned, res) {
			return errNotCleared
		}
		for dir := filepath.Dir(p); !holds[dir]; dir = filepath.Dir(dir) {
			holds[dir] = true
			if dir == path {
				break
			}
		}
		return nil
	})
	if err != nil {
		return false
	}
	for _, d := range dirs {
		if !holds[d] {
			return false
		}
	}
	return len(dirs) > 0
}

// linkedElement returns the first path element of path that is a symbolic link, checking every
// element strictly below the innermost root in roots that contains path, down to and including
// path itself. The root and everything above it are never checked: a harness folder may be a
// link. For a path inside no root (a file left from a harness folder that has since moved) every
// element strictly below bound is checked, or every element of the path when it is not below
// bound (the home directory, or the project root in project scope). It returns "" when no
// element is a link; an element that does not exist or cannot be
// read ends the check (the caller's own Lstat of path reports it).
func linkedElement(path string, roots []string, bound string) string {
	var elems []string
	switch root := innermostRoot(path, roots); {
	case root != "":
		for p := path; inside(root, p); p = filepath.Dir(p) {
			elems = append(elems, p)
		}
	case bound != "" && inside(filepath.Clean(bound), path):
		for p := path; inside(filepath.Clean(bound), p); p = filepath.Dir(p) {
			elems = append(elems, p)
		}
	default:
		for p := filepath.Clean(path); filepath.Dir(p) != p; p = filepath.Dir(p) {
			elems = append(elems, p)
		}
	}
	for i := len(elems) - 1; i >= 0; i-- {
		fi, err := os.Lstat(elems[i])
		if err != nil {
			return ""
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return elems[i]
		}
	}
	return ""
}

// readFile reads path. present is false when it does not exist; blocked is true when the path
// is a directory, or it or a path element linkedElement checks is a symbolic link, or a parent
// is not a directory, so livery writes no file there. A link is never followed: its target may
// be anywhere on the machine.
func readFile(path string, roots []string, bound string) (data []byte, present, blocked, isExec bool, err error) {
	if linkedElement(path, roots, bound) != "" {
		return nil, false, true, false, nil
	}
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, syscall.ENOTDIR):
		return nil, false, true, false, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, false, false, false, nil
	case err != nil:
		return nil, false, false, false, exit.Environmentf("%s: cannot read: %v; check its permissions, then run livery again", path, err)
	case fi.IsDir(), fi.Mode()&fs.ModeSymlink != 0:
		return nil, false, true, false, nil
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, false, false, false, exit.Environmentf("%s: cannot read: %v; check its permissions, then run livery again", path, err)
	}
	return data, true, false, fi.Mode()&0o111 != 0, nil
}
