package apply

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/lock"
)

// A first install stopped midway returns a lock with the meta fields of r.Lock and only the
// files written so far, plus the files that were already identical.
func TestExecutePartialFirstInstall(t *testing.T) {
	home := t.TempDir()
	plan := skillPlan(home, true)
	same := plan.Files[2].Path // b/deep/SKILL.md, already there with the wanted content
	put(t, same, "b\n")
	res, err := compute(t, []adapter.Plan{plan}, lock.Lock{}, home)
	if err != nil {
		t.Fatal(err)
	}
	edited := plan.Files[1].Path // run.sh, written after a/SKILL.md
	hookOnce(t, edited, func() { put(t, edited, "mine\n") })
	got, err := Execute(res)
	wantChanged(t, err, edited)
	want := lock.Lock{Livery: "0.1.0", Source: "s", Commit: "c0ffee", Files: map[string]string{
		"~/.h/skills/a/SKILL.md":      lock.Sum([]byte("a\n")),
		"~/.h/skills/b/deep/SKILL.md": lock.Sum([]byte("b\n")),
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lock %+v, want %+v", got, want)
	}
}

// Completed removals and a completed shared-file write (one entry unset, one set) are recorded;
// the file the run stopped on keeps its old record.
func TestExecutePartialRemoveAndEntries(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	shared := filepath.Join(root, "b.json")
	x := adapter.Entry{Path: shared, Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`)}
	y := adapter.Entry{Path: shared, Key: "servers", Name: "y", Value: json.RawMessage(`{"cmd":"y"}`)}
	gone1 := adapter.File{Path: filepath.Join(root, "a.md"), Data: []byte("a\n")}
	late := adapter.File{Path: filepath.Join(root, "c.md"), Data: []byte("c\n")}
	first := run(t, []adapter.Plan{{Root: root, Files: []adapter.File{gone1, late}, Entries: []adapter.Entry{x}}}, lock.Lock{}, home)

	late2 := late
	late2.Data = []byte("c v2\n")
	res, err := compute(t, []adapter.Plan{{Root: root, Files: []adapter.File{late2}, Entries: []adapter.Entry{y}}}, first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	hookOnce(t, late.Path, func() { put(t, late.Path, "mine\n") })
	got, err := Execute(res)
	wantChanged(t, err, late.Path)
	want := first.Lock
	want.Files = map[string]string{"~/.h/c.md": lock.Sum([]byte("c\n"))}
	want.Merged = []lock.Merged{{Path: "~/.h/b.json", Key: "servers", Entries: map[string]string{"y": mustSum(t, `{"cmd":"y"}`)}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lock %+v, want %+v", got, want)
	}
}
