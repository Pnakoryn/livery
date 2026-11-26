package apply

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

type entryID struct {
	path, key, name string
	list            bool
}

// listValue is the JSON of a list element, the Old and New of its actions.
func listValue(s string) []byte { return []byte(content.JSONString(s)) }

func computeEntries(plans []adapter.Plan, old lock.Lock, scope lock.Scope, res *Result) ([]Action, error) {
	wanted := map[entryID]adapter.Entry{}
	var order []entryID
	for _, p := range plans {
		for _, e := range p.Entries {
			id := entryID{filepath.Clean(e.Path), e.Key, e.Name, e.List}
			if prev, ok := wanted[id]; ok {
				if id.list {
					return nil, exit.Findingf("%s: element %q of %q wanted twice, by %s and by %s; remove one of them from the shared repository%s", id.path, id.name, id.key, prev.Origin, e.Origin, personalHint(scope.Project))
				}
				return nil, exit.Findingf("%s: entry %q under %q wanted twice, by %s and by %s; remove one of them from the shared repository%s", id.path, id.name, id.key, prev.Origin, e.Origin, personalHint(scope.Project))
			}
			wanted[id] = e
			order = append(order, id)
		}
	}
	type objID struct{ path, key string }
	ownKeys := map[objID][]string{}
	for _, p := range plans {
		for _, k := range p.EntryKeys {
			ownKeys[objID{filepath.Clean(k.Path), k.Key}] = k.Keys
		}
	}
	// diskSum is the checksum of an entry on disk: of livery's keys only when the object has
	// EntryKeys and the value is a JSON object; extra are then the other keys of the value.
	diskSum := func(id entryID, disk json.RawMessage) (sum string, extra []member, err error) {
		if keys, ok := ownKeys[objID{id.path, id.key}]; ok {
			if own, rest, isObj := splitKeys(disk, keys); isObj {
				sum, err = lock.SumJSON(compact(own))
				return sum, rest, err
			}
		}
		sum, err = lock.SumJSON(disk)
		return sum, nil, err
	}
	owned := map[entryID]string{}
	for _, m := range old.Merged {
		path := scope.Load(m.Path)
		for name, sum := range m.Entries {
			owned[entryID{path, m.Key, name, m.List}] = sum
		}
	}

	docs := map[string]*Doc{}
	blockedPaths := map[string]bool{}
	// onDisk reads the entry; blocked is true when the shared file's path is a directory or
	// sits under a regular file: livery writes nothing there, the place is the user's.
	onDisk := func(id entryID) (v json.RawMessage, ok, blocked bool, err error) {
		if blockedPaths[id.path] {
			return nil, false, true, nil
		}
		d, found := docs[id.path]
		if !found {
			if link := linkedElement(id.path, res.linkRoots(), res.bound()); link != "" {
				return nil, false, false, exit.Findingf("%s: is a symbolic link, and livery does not write through links; replace the link with a regular folder or file, then run %s", link, res.UpdateCmd())
			}
			fi, err := os.Lstat(id.path)
			switch {
			case errors.Is(err, syscall.ENOTDIR):
				blockedPaths[id.path] = true
				return nil, false, true, nil
			case err == nil && fi.IsDir():
				blockedPaths[id.path] = true
				return nil, false, true, nil
			}
			data, err := os.ReadFile(id.path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, false, false, exit.Environmentf("%s: cannot read: %v; check the path and its permissions, then run livery again", id.path, err)
			}
			d, err = ParseDoc(id.path, data)
			if err != nil {
				return nil, false, false, err
			}
			docs[id.path] = d
		}
		if id.list {
			ok, err := d.hasItem(id.key, id.name)
			if err != nil || !ok {
				return nil, false, false, err
			}
			return listValue(id.name), true, false, nil
		}
		entries, err := d.Entries(id.key)
		if err != nil {
			return nil, false, false, err
		}
		v, ok = entries[id.name]
		return v, ok, false, nil
	}

	type mkey struct {
		path, key string
		list      bool
	}
	merged := map[mkey]map[string]string{}
	record := func(id entryID, sum string) {
		k := mkey{key(scope, id.path), id.key, id.list}
		if merged[k] == nil {
			merged[k] = map[string]string{}
		}
		merged[k][id.name] = sum
	}

	var actions []Action
	for _, id := range order {
		e := wanted[id]
		if !id.list {
			if _, err := lock.SumJSON(e.Value); err != nil {
				return nil, exit.Usagef("%s: the value of entry %q under %q from %s is not valid JSON (%v); fix it in the shared repository%s", id.path, id.name, id.key, e.Origin, err, personalHint(scope.Project))
			}
		}
		if id.list {
			a, err := computeItem(id, e, owned, onDisk, record)
			if err != nil {
				return nil, err
			}
			actions = append(actions, a)
			continue
		}
		newSum, err := lock.SumJSON(e.Value)
		if err != nil {
			return nil, exit.Usagef("%s: the value of entry %q under %q from %s is not valid JSON (%v); fix it in the shared repository%s", id.path, id.name, id.key, e.Origin, err, personalHint(scope.Project))
		}
		a := Action{Path: id.path, Key: id.key, Name: id.name, New: e.Value, Origin: e.Origin}
		disk, present, blocked, err := onDisk(id)
		if err != nil {
			return nil, err
		}
		oldSum, isOwned := owned[id]
		st := state{Blocked: blocked, Present: present, Owned: isOwned}
		var extra []member
		if present {
			a.Old = disk
			sum, rest, err := diskSum(id, disk)
			if err != nil {
				return nil, exit.Findingf("%s: entry %q under %q is not valid JSON; fix the file, then run livery again", id.path, id.name, id.key)
			}
			extra = rest
			st.MatchesWanted = sum == newSum
			st.MatchesRecorded = isOwned && sum == oldSum
		}
		d := decide(true, st)
		a.Op, _ = d.op(SetEntry, UnsetEntry)
		a.Sum = newSum
		if a.Op == SetEntry && len(extra) > 0 {
			a.New = withExtra(e.Value, extra)
		}
		a.Missing = !present && !blocked && isOwned
		if sum, ok := d.sum(newSum, oldSum); ok {
			record(id, sum)
		}
		actions = append(actions, a)
	}

	for id, oldSum := range owned {
		if _, ok := wanted[id]; ok {
			continue
		}
		disk, present, blocked, err := onDisk(id)
		if err != nil {
			return nil, err
		}
		st := state{Blocked: blocked, Present: present, Owned: true}
		switch {
		case present && id.list:
			// An element has no value the user could have edited: present means it is still
			// livery's to remove.
			st.MatchesRecorded = true
		case present:
			sum, _, err := diskSum(id, disk)
			if err != nil {
				return nil, exit.Findingf("%s: entry %q under %q is not valid JSON; fix the file, then run livery again", id.path, id.name, id.key)
			}
			st.MatchesRecorded = sum == oldSum
		}
		if op, ok := decide(false, st).op(SetEntry, UnsetEntry); ok {
			actions = append(actions, Action{Path: id.path, Key: id.key, Name: id.name, List: id.list, Old: disk, Op: op})
		}
	}

	for k, entries := range merged {
		res.Lock.Merged = append(res.Lock.Merged, lock.Merged{Path: k.path, Key: k.key, List: k.list, Entries: entries})
	}
	return actions, nil
}

// splitKeys splits the JSON object v into its members whose key is in keys and the others,
// both in their order in v; isObj is false when v is not a JSON object.
func splitKeys(v json.RawMessage, keys []string) (own, rest []member, isObj bool) {
	members, err := parseObject(v)
	if err != nil {
		return nil, nil, false
	}
	own = []member{}
	for i, m := range members {
		if find(members, m.key) != i { // a repeated key: the last one counts
			continue
		}
		if slices.Contains(keys, m.key) {
			own = append(own, m)
		} else {
			rest = append(rest, m)
		}
	}
	return own, rest, true
}

// withExtra is the object want followed by the members of extra it does not have; want
// unchanged when it is not a JSON object.
func withExtra(want json.RawMessage, extra []member) json.RawMessage {
	members, err := parseObject(want)
	if err != nil {
		return want
	}
	for _, m := range extra {
		if find(members, m.key) < 0 {
			members = append(members, m)
		}
	}
	return compact(members)
}

// computeItem decides on one wanted list element. Absent: add it (again, when livery owned it
// and it is gone). Present and owned: nothing. Present and not owned: adopted into the lock
// (the file is not written), so uninstall removes it. A linked config file (blocked) is kept.
func computeItem(id entryID, e adapter.Entry, owned map[entryID]string,
	onDisk func(entryID) (json.RawMessage, bool, bool, error), record func(entryID, string),
) (Action, error) {
	a := Action{Path: id.path, Key: id.key, Name: id.name, List: true, New: listValue(id.name), Origin: e.Origin}
	disk, present, blocked, err := onDisk(id)
	if err != nil {
		return Action{}, err
	}
	_, isOwned := owned[id]
	sum := lock.Sum([]byte(id.name))
	// An element has no value: present means it equals what is wanted.
	st := state{Blocked: blocked, Present: present, Owned: isOwned, MatchesWanted: present, MatchesRecorded: present && isOwned}
	if present {
		a.Old = disk
	}
	d := decide(true, st)
	a.Op, _ = d.op(SetEntry, UnsetEntry)
	a.Missing = !present && !blocked && isOwned
	if s, ok := d.sum(sum, sum); ok {
		record(id, s)
	}
	return a, nil
}
