package apply

import (
	"path/filepath"
	"sort"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/lock"
)

// Op is what happens to one file or one shared JSON entry.
type Op string

// The operations Compute decides on.
const (
	Write     Op = "write"
	Remove    Op = "remove"
	Unchanged Op = "unchanged"
	// Adopt takes a present, identical file or entry that livery did not own into the lock;
	// nothing is written.
	Adopt      Op = "adopt"
	KeptYours  Op = "kept, yours"
	Edited     Op = "edited by you"
	LeftEdited Op = "left, edited by you"
	SetEntry   Op = "set"
	UnsetEntry Op = "unset"
)

// Action is one decision for a file, or for an entry when Key and Name are set.
type Action struct {
	Op   Op
	Path string // absolute
	// Key and Name identify an entry in a shared JSON file; empty for files. Key is a path of
	// object keys joined by "."; with List, Name is an element of the array of strings at Key.
	Key, Name string
	List      bool
	// Old and New are the file content, or the entry's JSON, for diffs (nil when absent).
	Old, New []byte
	Exec     bool
	Origin   string
	// Sum is the checksum the lock records for a non-list entry: the one of the wanted value,
	// which New may extend with keys the entry already had that livery does not own.
	Sum string
	// Missing is set on a Write or SetEntry for something the old lock records but the disk
	// no longer has.
	Missing bool
	// afterRemove is set on a Write whose path is blocked by owned files this run removes (a
	// file that becomes a folder, or the reverse): Execute runs it after the removals and checks
	// its target only right before its own write.
	afterRemove bool
}

// Result is the outcome of Compute: what to do, and the lock as it will be afterwards.
type Result struct {
	// Actions are sorted by Path, Key, Name.
	Actions []Action
	// Lock has Files and Merged as they will be after Execute; other fields come from meta.
	Lock lock.Lock
	// Old is the lock Compute was given: what livery owned before this run.
	Old lock.Lock
	// Roots are every plan's Root, sorted, without duplicates.
	Roots []string
	// Home is the home folder Compute was given; it bounds the link check of paths outside
	// every root in machine scope.
	Home string
	// Project is the project root in project scope (lock paths relative to it; it bounds the
	// link check of paths outside every root); empty in machine scope.
	Project string
	// Command is the livery command the run is, named by hints that retry it ("uninstall");
	// empty means "update".
	Command string
}

// ProjectArg is " --project" when project is a project root, else "": the suffix of a hint that
// names a livery command, so the hint works in the scope the run is in.
func ProjectArg(project string) string {
	if project != "" {
		return " --project"
	}
	return ""
}

// UpdateCmd is the command a hint names to retry the run: "livery update" (or "livery" and
// Command), with --project in project scope.
func (r Result) UpdateCmd() string {
	c := r.Command
	if c == "" {
		c = "update"
	}
	return "livery " + c + ProjectArg(r.Project)
}

// Scope is the lock scope of r.
func (r Result) Scope() lock.Scope { return lock.Scope{Home: r.Home, Project: r.Project} }

// bound is the folder below which the link check of a path outside every root starts.
func (r Result) bound() string {
	if r.Project != "" {
		return r.Project
	}
	return r.Home
}

// linkRoots are the roots whose own path is not link-checked: every root in machine scope (a
// harness folder in the home folder may be a link), none in project scope. In project scope every
// path element strictly below the project root is checked, the harness folder included: the
// project's content comes from a repository someone else may have written, and a link in it
// could make livery write outside the project.
func (r Result) linkRoots() []string {
	if r.Project != "" {
		return nil
	}
	return r.Roots
}

// Blocked returns the Edited actions: owned files or entries the user changed, which stop
// the run.
func (r Result) Blocked() []Action {
	var out []Action
	for _, a := range r.Actions {
		if a.Op == Edited {
			out = append(out, a)
		}
	}
	return out
}

// Changes reports whether executing r would change anything on disk or in the lock (an
// adoption changes only the lock).
func (r Result) Changes() bool {
	for _, a := range r.Actions {
		switch a.Op {
		case Write, Remove, SetEntry, UnsetEntry, Adopt:
			return true
		}
	}
	return false
}

// Compute decides, per file and per shared JSON entry, what to do given what the plans want,
// what the old lock says livery owns, and what is on disk. Lock paths go through scope. In
// project scope a plan path outside the project is a Finding. It reads the disk and writes
// nothing. command is "uninstall" or "" (update): the hints of errors found while computing
// name it too.
func Compute(plans []adapter.Plan, old lock.Lock, meta lock.Lock, scope lock.Scope, command string) (Result, error) {
	res := Result{Lock: meta, Old: old, Home: scope.Home, Project: scope.Project, Command: command}
	res.Lock.Files = map[string]string{}
	res.Lock.Merged = nil
	for _, p := range plans {
		for _, f := range p.Files {
			if _, err := scope.Store(filepath.Clean(f.Path)); err != nil {
				return Result{}, err
			}
		}
		for _, e := range p.Entries {
			if _, err := scope.Store(filepath.Clean(e.Path)); err != nil {
				return Result{}, err
			}
		}
	}

	roots := map[string]bool{}
	for _, p := range plans {
		if p.Root != "" && !roots[p.Root] {
			roots[p.Root] = true
			res.Roots = append(res.Roots, p.Root)
		}
	}
	sort.Strings(res.Roots)

	// A file wanted twice is refused before any entry is looked at.
	if _, _, err := wantedFiles(plans, scope.Project == ""); err != nil {
		return Result{}, err
	}
	entries, err := computeEntries(plans, old, scope, &res)
	if err != nil {
		return Result{}, err
	}
	files, err := computeFiles(plans, old, scope, &res)
	if err != nil {
		return Result{}, err
	}
	res.Actions = files
	res.Actions = append(res.Actions, entries...)

	sort.SliceStable(res.Actions, func(i, j int) bool {
		a, b := res.Actions[i], res.Actions[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.List != b.List {
			return !a.List
		}
		return a.Name < b.Name
	})
	lock.SortMerged(res.Lock.Merged)
	return res, nil
}

// key is the lock path of path; Compute has already refused paths Store rejects.
func key(scope lock.Scope, path string) string {
	k, _ := scope.Store(path)
	return k
}
