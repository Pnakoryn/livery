package apply

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

// snapshot describes every path below dir with its mode and content, to prove Compute writes
// nothing.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %v %v", p, fi.Mode(), fi.ModTime().UnixNano())
		if fi.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, " %x", sha256.Sum256(data))
		}
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// compute runs Compute and fails the test if it changed anything below home.
func compute(t *testing.T, plans []adapter.Plan, old lock.Lock, home string) (Result, error) {
	t.Helper()
	before := snapshot(t, home)
	res, err := Compute(plans, old, lock.Lock{Livery: "0.1.0", Source: "s", Commit: "c0ffee"}, lock.Scope{Home: home}, "")
	if after := snapshot(t, home); after != before {
		t.Fatalf("Compute changed the disk:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	return res, err
}

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	wantData = "wanted\n"
	oldData  = "installed before\n"
	userData = "the user's\n"
)

func TestComputeFiles(t *testing.T) {
	for _, c := range []struct {
		name   string
		disk   *string // nil: missing
		owned  bool
		wanted bool
		op     Op // "" for no action
		inLock bool
	}{
		{"missing, not owned, wanted", nil, false, true, Write, true},
		{"missing, owned, wanted", nil, true, true, Write, true},
		{"equal, not owned, wanted (adopted)", ptr(wantData), false, true, Adopt, true},
		{"equal, owned, wanted", ptr(wantData), true, true, Unchanged, true},
		{"differs, not owned, wanted", ptr(userData), false, true, KeptYours, false},
		{"differs, owned, not changed, wanted", ptr(oldData), true, true, Write, true},
		{"differs, owned, changed, wanted", ptr(userData), true, true, Edited, true},
		{"present, owned, not changed, not wanted", ptr(oldData), true, false, Remove, false},
		{"present, owned, changed, not wanted", ptr(userData), true, false, LeftEdited, false},
		{"missing, owned, not wanted", nil, true, false, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".h", "skills", "a.md")
			if c.disk != nil {
				put(t, path, *c.disk)
			}
			var old lock.Lock
			if c.owned {
				old.Files = map[string]string{"~/.h/skills/a.md": lock.Sum([]byte(oldData))}
			}
			plan := adapter.Plan{Root: filepath.Join(home, ".h")}
			if c.wanted {
				plan.Files = []adapter.File{{Path: path, Data: []byte(wantData), Origin: "team: a"}}
			}
			res, err := compute(t, []adapter.Plan{plan}, old, home)
			if err != nil {
				t.Fatal(err)
			}
			if c.op == "" {
				if len(res.Actions) != 0 {
					t.Fatalf("actions %+v, want none", res.Actions)
				}
			} else if len(res.Actions) != 1 || res.Actions[0].Op != c.op || res.Actions[0].Path != path {
				t.Fatalf("actions %+v, want one %q", res.Actions, c.op)
			}
			sum, in := res.Lock.Files["~/.h/skills/a.md"]
			if in != c.inLock || len(res.Lock.Files) != map[bool]int{true: 1}[c.inLock] {
				t.Fatalf("lock files %v, want in lock: %v", res.Lock.Files, c.inLock)
			}
			wantSum := lock.Sum([]byte(wantData))
			if c.op == Edited {
				wantSum = lock.Sum([]byte(oldData))
			}
			if in && sum != wantSum {
				t.Fatalf("lock checksum %s, want %s", sum, wantSum)
			}
			if res.Lock.Livery != "0.1.0" || res.Lock.Commit != "c0ffee" {
				t.Fatalf("meta not copied: %+v", res.Lock)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestComputeFileBlockedDestination(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "d")
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, "f"), "a file")
	under := filepath.Join(home, "f", "child")
	plans := []adapter.Plan{{Files: []adapter.File{
		{Path: dir, Data: []byte(wantData), Origin: "o1"},
		{Path: under, Data: []byte(wantData), Origin: "o2"},
	}}}
	old := lock.Lock{Files: map[string]string{"~/d": lock.Sum([]byte(oldData))}}
	res, err := compute(t, plans, old, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 || res.Actions[0].Op != KeptYours || res.Actions[1].Op != KeptYours {
		t.Fatalf("actions %+v", res.Actions)
	}
	if len(res.Lock.Files) != 0 {
		t.Fatalf("lock files %v", res.Lock.Files)
	}
}

func TestComputeEditedIsBlocked(t *testing.T) {
	home := t.TempDir()
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	put(t, a, userData)
	put(t, b, oldData)
	old := lock.Lock{Files: map[string]string{"~/a": lock.Sum([]byte(oldData)), "~/b": lock.Sum([]byte(oldData))}}
	plans := []adapter.Plan{{Files: []adapter.File{
		{Path: b, Data: []byte(wantData)},
		{Path: a, Data: []byte(wantData)},
	}}}
	res, err := compute(t, plans, old, home)
	if err != nil {
		t.Fatal(err)
	}
	blocked := res.Blocked()
	if len(blocked) != 1 || blocked[0].Path != a || string(blocked[0].Old) != userData || string(blocked[0].New) != wantData {
		t.Fatalf("Blocked() = %+v", blocked)
	}
	if !res.Changes() {
		t.Fatal("Changes() false with a Write")
	}
	if res.Actions[0].Path != a || res.Actions[1].Path != b {
		t.Fatalf("not sorted by path: %+v", res.Actions)
	}
}

func TestComputeChangesFalse(t *testing.T) {
	home := t.TempDir()
	put(t, filepath.Join(home, "a"), wantData)
	put(t, filepath.Join(home, "b"), userData)
	res, err := compute(t, []adapter.Plan{{Files: []adapter.File{
		{Path: filepath.Join(home, "a"), Data: []byte(wantData)},
		{Path: filepath.Join(home, "b"), Data: []byte(wantData)},
	}}}, lock.Lock{Files: map[string]string{"~/a": lock.Sum([]byte(wantData))}}, home)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changes() || len(res.Blocked()) != 0 {
		t.Fatalf("Changes() %v, Blocked() %v", res.Changes(), res.Blocked())
	}
}

const entryKey = "servers"

func TestComputeEntries(t *testing.T) {
	wantVal := `{"command":"x","args":["a","b"]}`
	oldVal := `{"command":"old"}`
	userVal := `{"command":"mine"}`
	for _, c := range []struct {
		name   string
		disk   string // "" missing
		owned  bool
		wanted bool
		op     Op
		inLock bool
	}{
		{"missing, not owned, wanted", "", false, true, SetEntry, true},
		{"missing, owned, wanted", "", true, true, SetEntry, true},
		{"equal, not owned, wanted (adopted)", wantVal, false, true, Adopt, true},
		{"equal reformatted, owned, wanted", "{\n  \"args\": [\"a\", \"b\"],\n  \"command\": \"x\"\n}", true, true, Unchanged, true},
		{"differs, not owned, wanted", userVal, false, true, KeptYours, false},
		{"differs, owned, not changed, wanted", oldVal, true, true, SetEntry, true},
		{"differs, owned, not changed (reformatted), wanted", "{ \"command\" : \"old\" }", true, true, SetEntry, true},
		{"differs, owned, changed, wanted", userVal, true, true, Edited, true},
		{"present, owned, not changed, not wanted", oldVal, true, false, UnsetEntry, false},
		{"present, owned, changed, not wanted", userVal, true, false, LeftEdited, false},
		{"missing, owned, not wanted", "", true, false, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".h", "settings.json")
			servers := `{"other":{"command":"theirs"}`
			if c.disk != "" {
				servers += `,"s":` + c.disk
			}
			put(t, path, `{"z":1,"`+entryKey+`":`+servers+`}}`)
			var old lock.Lock
			if c.owned {
				sum, _ := lock.SumJSON(json.RawMessage(oldVal))
				old.Merged = []lock.Merged{{Path: "~/.h/settings.json", Key: entryKey, Entries: map[string]string{"s": sum}}}
			}
			var plan adapter.Plan
			if c.wanted {
				plan.Entries = []adapter.Entry{{Path: path, Key: entryKey, Name: "s", Value: json.RawMessage(wantVal), Origin: "team: s"}}
			}
			res, err := compute(t, []adapter.Plan{plan}, old, home)
			if err != nil {
				t.Fatal(err)
			}
			if c.op == "" {
				if len(res.Actions) != 0 {
					t.Fatalf("actions %+v, want none", res.Actions)
				}
			} else if len(res.Actions) != 1 || res.Actions[0].Op != c.op || res.Actions[0].Path != path ||
				res.Actions[0].Key != entryKey || res.Actions[0].Name != "s" {
				t.Fatalf("actions %+v, want one %q", res.Actions, c.op)
			}
			if !c.inLock {
				if len(res.Lock.Merged) != 0 {
					t.Fatalf("lock merged %+v, want empty", res.Lock.Merged)
				}
				return
			}
			wantSum, _ := lock.SumJSON(json.RawMessage(wantVal))
			if c.op == Edited {
				wantSum, _ = lock.SumJSON(json.RawMessage(oldVal))
			}
			want := []lock.Merged{{Path: "~/.h/settings.json", Key: entryKey, Entries: map[string]string{"s": wantSum}}}
			if !reflect.DeepEqual(res.Lock.Merged, want) {
				t.Fatalf("lock merged %+v, want %+v", res.Lock.Merged, want)
			}
		})
	}
}

func TestComputeEntriesMissingFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "new.json")
	res, err := compute(t, []adapter.Plan{{Entries: []adapter.Entry{
		{Path: path, Key: "k", Name: "b", Value: json.RawMessage(`1`)},
		{Path: path, Key: "k", Name: "a", Value: json.RawMessage(`2`)},
	}}}, lock.Lock{}, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 || res.Actions[0].Name != "a" || res.Actions[0].Op != SetEntry || res.Actions[0].Old != nil {
		t.Fatalf("actions %+v", res.Actions)
	}
	if len(res.Lock.Merged) != 1 || len(res.Lock.Merged[0].Entries) != 2 {
		t.Fatalf("merged %+v", res.Lock.Merged)
	}
}

func TestComputeDuplicates(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "x")
	for name, plans := range map[string][]adapter.Plan{
		"files": {
			{Files: []adapter.File{{Path: path, Data: []byte("1"), Origin: "team: one"}}},
			{Files: []adapter.File{{Path: path, Data: []byte("2"), Origin: "personal: two"}}},
		},
		"entries": {{Entries: []adapter.Entry{
			{Path: path, Key: "k", Name: "n", Value: json.RawMessage(`1`), Origin: "team: one"},
			{Path: path, Key: "k", Name: "n", Value: json.RawMessage(`1`), Origin: "personal: two"},
		}}},
	} {
		_, err := compute(t, plans, lock.Lock{}, home)
		var e *exit.Error
		if !errors.As(err, &e) || e.Code != exit.Finding {
			t.Fatalf("%s: err %v, want a finding", name, err)
		}
		for _, s := range []string{path, "team: one", "personal: two"} {
			if !strings.Contains(e.Message, s) {
				t.Fatalf("%s: message %q does not name %q", name, e.Message, s)
			}
		}
	}
}

func TestComputeRoots(t *testing.T) {
	res, err := compute(t, []adapter.Plan{{Root: "/h/b"}, {Root: "/h/a"}, {Root: "/h/b"}}, lock.Lock{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/h/a", "/h/b"}; !reflect.DeepEqual(res.Roots, want) {
		t.Fatalf("Roots = %v, want %v", res.Roots, want)
	}
}

func TestComputeOrder(t *testing.T) {
	home := t.TempDir()
	j := filepath.Join(home, "a.json")
	res, err := compute(t, []adapter.Plan{{
		Files: []adapter.File{{Path: filepath.Join(home, "z")}, {Path: j}},
		Entries: []adapter.Entry{
			{Path: j, Key: "b", Name: "x", Value: json.RawMessage(`1`)},
			{Path: j, Key: "a", Name: "y", Value: json.RawMessage(`1`)},
			{Path: j, Key: "a", Name: "x", Value: json.RawMessage(`1`)},
		},
	}}, lock.Lock{}, home)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range res.Actions {
		got = append(got, strings.TrimPrefix(a.Path, home)+" "+a.Key+" "+a.Name)
	}
	if !sort.StringsAreSorted(got) || len(got) != 5 {
		t.Fatalf("not sorted: %q", got)
	}
}

// A symbolic link is never followed: its target may be anywhere on the machine.
func TestComputeSymlinkFileIsKept(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "dotfiles", "a.md")
	put(t, target, wantData)
	wanted := filepath.Join(home, "h", "a.md")
	gone := filepath.Join(home, "h", "gone.md")
	if err := os.MkdirAll(filepath.Dir(wanted), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{wanted, gone} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	plans := []adapter.Plan{{Files: []adapter.File{{Path: wanted, Data: []byte(wantData), Origin: "o"}}}}
	// Both were owned; one is still wanted (with identical content behind the link), one is not.
	old := lock.Lock{Files: map[string]string{"~/h/a.md": lock.Sum([]byte(wantData)), "~/h/gone.md": lock.Sum([]byte(wantData))}}
	res, err := compute(t, plans, old, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 || res.Actions[0].Op != KeptYours || res.Actions[1].Op != LeftEdited {
		t.Fatalf("actions %+v", res.Actions)
	}
	if len(res.Lock.Files) != 0 || res.Changes() {
		t.Fatalf("lock files %v, changes %v", res.Lock.Files, res.Changes())
	}
}

func TestComputeSymlinkSharedFileIsRefused(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "dotfiles", "s.json")
	put(t, target, `{"servers":{}}`)
	link := filepath.Join(home, "h", "s.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	plans := []adapter.Plan{{Entries: []adapter.Entry{{Path: link, Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`), Origin: "o"}}}}
	_, err := compute(t, plans, lock.Lock{}, home)
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Finding {
		t.Fatalf("want a Finding, got %v", err)
	}
	if !strings.Contains(err.Error(), link) || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("message %q", err.Error())
	}
}

func TestComputeEntriesBlockedPath(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, "f"), "a file")
	dir, under := filepath.Join(home, "d"), filepath.Join(home, "f", "child.json")
	plans := []adapter.Plan{{Entries: []adapter.Entry{
		{Path: dir, Key: "k", Name: "n", Value: json.RawMessage(`1`), Origin: "o"},
		{Path: under, Key: "k", Name: "n", Value: json.RawMessage(`1`), Origin: "o"},
	}}}
	old := lock.Lock{Merged: []lock.Merged{{Path: "~/d", Key: "k", Entries: map[string]string{"gone": "sha256:x"}}}}
	res, err := compute(t, plans, old, home)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]Op{}
	for _, a := range res.Actions {
		ops[a.Path+"/"+a.Name] = a.Op
	}
	if len(res.Actions) != 3 || ops[dir+"/n"] != KeptYours || ops[under+"/n"] != KeptYours || ops[dir+"/gone"] != LeftEdited {
		t.Fatalf("actions %+v", res.Actions)
	}
	if len(res.Lock.Merged) != 0 {
		t.Fatalf("merged %v", res.Lock.Merged)
	}
}

func TestComputeListEntryBlockedPath(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, "f"), "a file")
	dir, under := filepath.Join(home, "d"), filepath.Join(home, "f", "child.json")
	plans := []adapter.Plan{{Entries: []adapter.Entry{
		{Path: dir, Key: "k", Name: "n", List: true, Value: json.RawMessage(`"n"`), Origin: "o"},
		{Path: under, Key: "k", Name: "n", List: true, Value: json.RawMessage(`"n"`), Origin: "o"},
	}}}
	res, err := compute(t, plans, lock.Lock{}, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 {
		t.Fatalf("actions %+v", res.Actions)
	}
	for _, a := range res.Actions {
		if a.Op != KeptYours || !a.List {
			t.Errorf("action %+v, want kept, yours with List", a)
		}
	}
	if len(res.Lock.Merged) != 0 {
		t.Fatalf("merged %v", res.Lock.Merged)
	}
}

func TestComputeExecutableBit(t *testing.T) {
	for _, c := range []struct {
		name     string
		diskMode os.FileMode
		wantExec bool
		owned    bool
		op       Op
		after    os.FileMode // permission bits after Execute; 0: not executed
	}{
		{"wanted executable, disk 0644", 0o644, true, true, Write, 0o755},
		{"wanted not executable, disk 0755", 0o755, false, true, Write, 0o644},
		{"same state, other mode bits", 0o600, false, true, Unchanged, 0},
		{"unowned, not executable, wanted executable", 0o644, true, false, KeptYours, 0o644},
		{"unowned, executable, wanted not executable", 0o755, false, false, KeptYours, 0o755},
		{"unowned, restrictive mode", 0o600, true, false, KeptYours, 0o600},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, ".h", "skills", "a.sh")
			put(t, path, wantData)
			if err := os.Chmod(path, c.diskMode); err != nil {
				t.Fatal(err)
			}
			var old lock.Lock
			if c.owned {
				old.Files = map[string]string{"~/.h/skills/a.sh": lock.Sum([]byte(wantData))}
			}
			plan := adapter.Plan{Root: filepath.Join(home, ".h"), Files: []adapter.File{{Path: path, Data: []byte(wantData), Exec: c.wantExec, Origin: "team: a"}}}
			res, err := compute(t, []adapter.Plan{plan}, old, home)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Actions) != 1 || res.Actions[0].Op != c.op {
				t.Fatalf("actions %+v, want one %q", res.Actions, c.op)
			}
			wantFiles := 1
			if c.op == KeptYours {
				wantFiles = 0 // not livery's: no ownership is taken
			}
			if len(res.Lock.Files) != wantFiles || (wantFiles == 1 && res.Lock.Files["~/.h/skills/a.sh"] != lock.Sum([]byte(wantData))) {
				t.Fatalf("lock files %v", res.Lock.Files)
			}
			if c.after == 0 {
				return
			}
			if _, err := Execute(res); err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != c.after {
				t.Fatalf("mode %v, want %v", fi.Mode().Perm(), c.after)
			}
			if c.op == KeptYours {
				return // nothing was changed, so there is no diff
			}
			var b strings.Builder
			if err := Diff(&b, res); err != nil {
				t.Fatal(err)
			}
			want := "mode    ~/.h/skills/a.sh: " + map[bool]string{true: "executable", false: "not executable"}[c.wantExec] + "\n"
			if b.String() != want {
				t.Fatalf("diff %q, want %q", b.String(), want)
			}
		})
	}
}
