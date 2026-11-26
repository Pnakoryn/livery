package apply

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

// Execute carries out r on disk in the order of r.Actions, except that a Write waiting for this
// run's removals (a file that becomes a folder, or the reverse) runs last. A blocked (Edited) action is no
// mutation: it is skipped, the rest is applied, and the returned lock keeps its old record (see
// Compute); the caller reports it. Before the first write it reads every target again (except a
// Write that waits for this run's removals): when one is no longer what Compute saw (a.Old), it
// writes nothing and returns a Finding. It checks each
// target once more right before its own mutation (before the rename of a written file, before
// a removal, after reading a shared JSON file and before changing it): on a difference it stops
// with the same Finding, and the actions already applied in this run stay applied. A change
// another program makes between that last read and the rename is still lost; the run lock
// keeps other livery runs out, nothing coordinates with a running harness.
//
// It does not write the lock; it returns the lock that is true after what it did. On success
// that is r.Lock. When it stops after at least one mutation, it is r.Old with every completed
// mutation applied and every Adopt action adopted (see partialLock). When it stops before any
// mutation, it is r.Old.
func Execute(r Result) (lock.Lock, error) {
	changed, err := changedTargets(r)
	if err != nil {
		return r.Old, err
	}
	if len(changed) > 0 {
		return r.Old, changedWhileRunning(r.UpdateCmd(), changed...)
	}

	entries := map[string][]Action{}
	for _, a := range r.Actions {
		if a.Op == SetEntry || a.Op == UnsetEntry {
			entries[a.Path] = append(entries[a.Path], a)
		}
	}
	// A Write marked afterRemove runs after every other mutation, so after the removals of what
	// is in its way (a file that becomes a folder, or the reverse). The rest keeps the order of
	// r.Actions.
	ordered := make([]Action, 0, len(r.Actions))
	for _, a := range r.Actions {
		if !a.afterRemove {
			ordered = append(ordered, a)
		}
	}
	for _, a := range r.Actions {
		if a.afterRemove {
			ordered = append(ordered, a)
		}
	}
	done := map[string]bool{}
	var completed []Action // the mutations applied so far; a shared file adds all its actions
	for _, a := range ordered {
		switch a.Op {
		case Write, Remove:
		case SetEntry, UnsetEntry:
			if done[a.Path] {
				continue
			}
			done[a.Path] = true
		default:
			continue
		}
		if beforeMutation != nil {
			beforeMutation(a)
		}
		check := func() error { return sameFile(a, r.linkRoots(), r.bound(), r.UpdateCmd()) }
		var err error
		switch a.Op {
		case Write:
			mode := os.FileMode(0o644)
			if a.Exec {
				mode = 0o755
			}
			err = writeAtomic(a.Path, a.New, mode, r.UpdateCmd(), check)
		case Remove:
			err = removeFile(a.Path, r.Roots, r.Project, r.UpdateCmd(), check)
		case SetEntry, UnsetEntry:
			err = applyEntries(a.Path, entries[a.Path], r)
		}
		if err != nil {
			if len(completed) == 0 {
				return r.Old, err
			}
			return partialLock(r, completed), err
		}
		switch a.Op {
		case Write, Remove:
			completed = append(completed, a)
		default:
			completed = append(completed, entries[a.Path]...)
		}
	}
	return r.Lock, nil
}

// partialLock is the lock that is true after a run that stopped once the mutations in
// completed were applied: r.Old (or, when there was none, the fields of r.Lock without files
// and entries), with every Adopt action adopted and every completed Write, Remove,
// SetEntry and UnsetEntry recorded.
func partialLock(r Result, completed []Action) lock.Lock {
	l := r.Old
	if l.Source == "" { // no old lock: first install
		l = r.Lock
		l.Files, l.Merged = nil, nil
	}
	scope := r.Scope()
	files := map[string]string{}
	for p, sum := range l.Files {
		files[key(scope, scope.Load(p))] = sum
	}
	type mkey struct {
		path, key string
		list      bool
	}
	merged := map[mkey]map[string]string{}
	for _, m := range l.Merged {
		k := mkey{key(scope, scope.Load(m.Path)), m.Key, m.List}
		if merged[k] == nil {
			merged[k] = map[string]string{}
		}
		for name, sum := range m.Entries {
			merged[k][name] = sum
		}
	}
	record := func(a Action) {
		path := key(scope, a.Path)
		if a.Key == "" && a.Name == "" {
			if a.Op == Remove {
				delete(files, path)
			} else {
				files[path] = lock.Sum(a.New)
			}
			return
		}
		k := mkey{path, a.Key, a.List}
		if a.Op == UnsetEntry {
			delete(merged[k], a.Name)
			return
		}
		sum := a.Sum
		switch {
		case a.List:
			sum = lock.Sum([]byte(a.Name))
		case sum != "":
		default:
			var err error
			if sum, err = lock.SumJSON(a.New); err != nil { // Compute already checked it; leave the old record
				return
			}
		}
		if merged[k] == nil {
			merged[k] = map[string]string{}
		}
		merged[k][a.Name] = sum
	}
	for _, a := range r.Actions {
		if a.Op == Unchanged || a.Op == Adopt {
			record(a)
		}
	}
	for _, a := range completed {
		record(a)
	}

	l.Files = files
	l.Merged = nil
	for k, entries := range merged {
		if len(entries) > 0 {
			l.Merged = append(l.Merged, lock.Merged{Path: k.path, Key: k.key, List: k.list, Entries: entries})
		}
	}
	lock.SortMerged(l.Merged)
	return l
}

// beforeMutation, when set (by tests), is called in Execute right before each single mutation,
// before its own check: a file write or removal, or the update of one shared JSON file (called
// with its first action).
var beforeMutation func(Action)

// changedWhileRunning is the Finding for targets that are no longer what Compute saw.
func changedWhileRunning(next string, names ...string) error {
	return exit.Findingf("changed while livery was running: %s; run %s again", strings.Join(names, ", "), next)
}

// sameFile reads the target of a Write or Remove again, as Compute does (no link is followed),
// and returns the changedWhileRunning Finding when it is no longer a.Old.
func sameFile(a Action, roots []string, home, next string) error {
	same, err := sameFileTarget(a, roots, home)
	if err != nil {
		return err
	}
	if !same {
		return changedWhileRunning(next, Target(a.Path, a))
	}
	return nil
}

// sameFileTarget reports whether the target of a Write or Remove is still a.Old: absent when
// a.Old is nil, else the same bytes.
func sameFileTarget(a Action, roots []string, home string) (bool, error) {
	disk, present, blocked, _, err := readFile(a.Path, roots, home)
	switch {
	case err != nil:
		return false, err
	case blocked:
		return false, nil
	case a.Old == nil:
		return !present, nil
	default:
		return present && bytes.Equal(disk, a.Old), nil
	}
}

// changedTargets reads the target of every Write, Remove, SetEntry and UnsetEntry again, with
// the rules Compute uses (no link is followed), and names those that are no longer a.Old.
func changedTargets(r Result) ([]string, error) {
	var names []string
	docs := map[string]*Doc{} // nil: the shared file cannot be read as Compute read it
	for _, a := range r.Actions {
		var same bool
		switch a.Op {
		case Write, Remove:
			if a.afterRemove {
				// Blocked until this run's removals: checked right before its own write.
				continue
			}
			var err error
			same, err = sameFileTarget(a, r.linkRoots(), r.bound())
			if err != nil {
				return nil, err
			}
		case SetEntry, UnsetEntry:
			d, ok := docs[a.Path]
			if !ok {
				d = readDoc(a.Path, r.linkRoots(), r.bound())
				docs[a.Path] = d
			}
			same = sameEntry(d, a)
		default:
			continue
		}
		if !same {
			names = append(names, Target(a.Path, a))
		}
	}
	return names, nil
}

// readDoc reads the shared JSON file path as Compute does; nil when it is behind a link, is
// not a regular file, or cannot be read or parsed. A missing file is an empty document.
func readDoc(path string, roots []string, home string) *Doc {
	if linkedElement(path, roots, home) != "" {
		return nil
	}
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &Doc{path: path}
	case err != nil, !fi.Mode().IsRegular():
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	d, err := ParseDoc(path, data)
	if err != nil {
		return nil
	}
	return d
}

// sameEntry reports whether the entry of a in d is what Compute saw: absent when a.Old is nil,
// else the same lock.SumJSON. For a list element only its presence counts.
func sameEntry(d *Doc, a Action) bool {
	if d == nil {
		return false
	}
	if a.List {
		present, err := d.hasItem(a.Key, a.Name)
		return err == nil && present == (a.Old != nil)
	}
	entries, err := d.Entries(a.Key)
	if err != nil {
		return false
	}
	v, present := entries[a.Name]
	if a.Old == nil {
		return !present
	}
	if !present {
		return false
	}
	want, err := lock.SumJSON(a.Old)
	if err != nil {
		return false
	}
	got, err := lock.SumJSON(v)
	return err == nil && got == want
}

// applyEntries applies every SetEntry and UnsetEntry for one JSON file and writes it once. A
// file left holding nothing but now-empty keys this run unset from is removed instead. After
// reading the file it checks every entry it changes against a.Old and stops with the
// changedWhileRunning Finding on a difference; keys it does not touch come from this read.
func applyEntries(path string, actions []Action, r Result) error {
	changed := func() error {
		var names []string
		for _, a := range actions {
			names = append(names, Target(path, a))
		}
		return changedWhileRunning(r.UpdateCmd(), names...)
	}
	if linkedElement(path, r.linkRoots(), r.bound()) != "" {
		return changed()
	}
	mode := os.FileMode(0o600)
	fi, err := os.Lstat(path)
	switch {
	case err == nil && !fi.Mode().IsRegular():
		return changed()
	case err == nil:
		mode = fi.Mode().Perm()
	case errors.Is(err, fs.ErrNotExist):
	default:
		return exit.Environmentf("%s: cannot read: %v; check its permissions, then run %s", path, err, r.UpdateCmd())
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return exit.Environmentf("%s: cannot read: %v; check its permissions, then run %s", path, err, r.UpdateCmd())
	}
	d, err := ParseDoc(path, data)
	if err != nil {
		return err
	}
	var differ []string
	for _, a := range actions {
		if !sameEntry(d, a) {
			differ = append(differ, Target(path, a))
		}
	}
	if len(differ) > 0 {
		return changedWhileRunning(r.UpdateCmd(), differ...)
	}
	empty, err := mutateDoc(d, actions)
	if err != nil {
		return err
	}
	if empty {
		return removeFile(path, r.Roots, r.Project, r.UpdateCmd(), nil)
	}
	return writeAtomic(path, d.Bytes(), mode, r.UpdateCmd(), nil)
}

// mutateDoc applies the SetEntry and UnsetEntry actions to d. empty is true when nothing was
// set and d now holds nothing but empty keys the actions unset from: the file is removed then.
func mutateDoc(d *Doc, actions []Action) (empty bool, err error) {
	set := false
	unset := map[string]bool{} // key path -> true when it is a list
	for _, a := range actions {
		switch {
		case a.Op == SetEntry && a.List:
			set = true
			err = d.AddItem(a.Key, a.Name)
		case a.Op == SetEntry:
			set = true
			err = d.Set(a.Key, a.Name, a.New)
		case a.Op != UnsetEntry:
			continue
		case a.List:
			unset[a.Key] = true
			err = d.RemoveItem(a.Key, a.Name)
		default:
			if _, ok := unset[a.Key]; !ok {
				unset[a.Key] = false
			}
			err = d.Remove(a.Key, a.Name)
		}
		if err != nil {
			return false, err
		}
	}
	return !set && d.onlyEmpty(unset), nil
}
