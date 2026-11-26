package apply

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

// A file livery owns from a harness folder that has since moved lies inside no current root:
// every element below home is checked for links.
func TestOutsideRootsLinkOnTheWay(t *testing.T) {
	for _, c := range []struct{ name, link string }{
		{"folder holding the file", filepath.Join("old", "steering")},
		{"one level higher", "old"},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			target := filepath.Join(home, "elsewhere")
			files := map[string]string{"a.md": oldData}
			if c.link == "old" {
				files = map[string]string{filepath.Join("steering", "a.md"): oldData}
			}
			check := linked(t, filepath.Join(home, c.link), target, files)
			old := lock.Lock{Files: map[string]string{"~/old/steering/a.md": lock.Sum([]byte(oldData))}}
			plans := []adapter.Plan{{Root: filepath.Join(home, ".h")}}

			res := run(t, plans, old, home)
			if len(res.Actions) != 1 || res.Actions[0].Op != LeftEdited {
				t.Fatalf("actions %+v", res.Actions)
			}
			if res.Changes() {
				t.Errorf("changes %+v", res.Actions)
			}
			if _, err := os.Stat(filepath.Join(home, "old", "steering", "a.md")); err != nil {
				t.Errorf("link target file: %v", err)
			}
			check()
		})
	}
}

func TestOutsideRootsNoLinkIsRemoved(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "old", "steering", "a.md")
	put(t, path, oldData)
	old := lock.Lock{Files: map[string]string{"~/old/steering/a.md": lock.Sum([]byte(oldData))}}
	res := run(t, []adapter.Plan{{Root: filepath.Join(home, ".h")}}, old, home)
	if got := report(res, home); got != "remove  ~/old/steering/a.md\n" {
		t.Errorf("report %q", got)
	}
	gone(t, path)
}

func TestOutsideHomeEveryElementIsChecked(t *testing.T) {
	parent := t.TempDir()
	// On macOS the temp dir sits below /var, itself a link to /private/var.
	parent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(parent, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "other")
	check := linked(t, link, filepath.Join(parent, "target"), map[string]string{"a.md": oldData})
	if got := linkedElement(filepath.Join(link, "a.md"), nil, home); got != link {
		t.Errorf("linked element %q, want %q", got, link)
	}
	// ~/.claude.json directly in home: only the file itself is checked.
	if got := linkedElement(filepath.Join(home, ".claude.json"), nil, home); got != "" {
		t.Errorf("linked element %q", got)
	}
	check()
}

func TestOutsideRootsLinkedSharedFileIsRefused(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(home, "old")
	check := linked(t, link, filepath.Join(home, "elsewhere"), map[string]string{"mcp.json": `{"servers":{"x":{"cmd":"x"}}}`})
	old := lock.Lock{Merged: []lock.Merged{{Path: "~/old/mcp.json", Key: "servers", Entries: map[string]string{"x": mustSum(t, `{"cmd":"x"}`)}}}}
	_, err := compute(t, []adapter.Plan{{Root: filepath.Join(home, ".h")}}, old, home)
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Finding || !strings.HasPrefix(err.Error(), link+":") {
		t.Fatalf("want a Finding naming %s, got %v", link, err)
	}
	check()
}

func mustSum(t *testing.T, v string) string {
	t.Helper()
	s, err := lock.SumJSON(json.RawMessage(v))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// hookOnce sets beforeMutation for the test: change runs right before the mutation of the
// action whose path is at.
func hookOnce(t *testing.T, at string, change func()) {
	t.Helper()
	t.Cleanup(func() { beforeMutation = nil })
	beforeMutation = func(a Action) {
		if a.Path == at {
			change()
		}
	}
}

func wantChanged(t *testing.T, err error, name string) {
	t.Helper()
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Finding {
		t.Fatalf("err %v, want a Finding", err)
	}
	for _, part := range []string{name, "changed", "livery update"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("message %q lacks %q", err, part)
		}
	}
}

func TestExecuteFileEditedMidRun(t *testing.T) {
	home := t.TempDir()
	first := run(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	next := skillPlan(home, true)
	next.Files[0].Data = []byte("a v2\n")
	next.Files[1].Data = []byte("#!/bin/sh\necho v2\n")
	res, err := compute(t, []adapter.Plan{next}, first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	// Actions are sorted by path: SKILL.md of a first, then run.sh. run.sh is edited after the
	// first check, right before its own write.
	edited := next.Files[1].Path
	hookOnce(t, edited, func() { put(t, edited, "hand edit\n") })
	got, err := Execute(res)
	wantChanged(t, err, edited)
	// The lock names the completed write with its new checksum, the stopped one with its old.
	want := map[string]string{
		"~/.h/skills/a/SKILL.md":      lock.Sum([]byte("a v2\n")),
		"~/.h/skills/a/run.sh":        first.Lock.Files["~/.h/skills/a/run.sh"],
		"~/.h/skills/b/deep/SKILL.md": lock.Sum([]byte("b\n")),
	}
	if !reflect.DeepEqual(got.Files, want) {
		t.Errorf("lock files %v, want %v", got.Files, want)
	}
	if got.Commit != first.Lock.Commit {
		t.Errorf("lock commit %q", got.Commit)
	}
	if b, _ := os.ReadFile(edited); string(b) != "hand edit\n" {
		t.Errorf("edited file %q", b)
	}
	// The earlier action of the run stays applied.
	if b, _ := os.ReadFile(next.Files[0].Path); string(b) != "a v2\n" {
		t.Errorf("earlier file %q", b)
	}
	// No temporary file is left behind.
	ents, _ := os.ReadDir(filepath.Dir(edited))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".livery-") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

func TestExecuteRemoveEditedMidRun(t *testing.T) {
	home := t.TempDir()
	first := run(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	res, err := compute(t, []adapter.Plan{skillPlan(home, false)}, first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".h", "skills", "b", "deep", "SKILL.md")
	hookOnce(t, path, func() { put(t, path, "hand edit\n") })
	got, err := Execute(res)
	wantChanged(t, err, path)
	// Nothing was changed: the old lock comes back as it was.
	if !reflect.DeepEqual(got, first.Lock) {
		t.Errorf("lock %+v, want %+v", got, first.Lock)
	}
	if b, _ := os.ReadFile(path); string(b) != "hand edit\n" {
		t.Errorf("content %q", b)
	}
}

func TestExecuteEntryEditedMidRun(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "settings.json")
	// The file action sorts before settings.json; it triggers the edit of the shared file.
	early := filepath.Join(home, ".h", "a.md")
	x := adapter.Entry{Path: path, Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`)}
	plan := entryPlan(home, x)
	plan[0].Files = []adapter.File{{Path: early, Data: []byte("a\n")}}
	first := run(t, plan, lock.Lock{}, home)

	x2 := x
	x2.Value = json.RawMessage(`{"cmd":"x2"}`)
	next := entryPlan(home, x2)
	next[0].Files = []adapter.File{{Path: early, Data: []byte("a v2\n")}}
	res, err := compute(t, next, first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	edited := `{"theme": "dark", "servers": {"x": {"cmd": "mine"}}}`
	hookOnce(t, early, func() { put(t, path, edited) })
	got, err := Execute(res)
	wantChanged(t, err, path+" servers.x")
	if want := lock.Sum([]byte("a v2\n")); got.Files["~/.h/a.md"] != want {
		t.Errorf("a.md in the lock %q, want %q", got.Files["~/.h/a.md"], want)
	}
	if !reflect.DeepEqual(got.Merged, first.Lock.Merged) {
		t.Errorf("merged %+v, want the old %+v", got.Merged, first.Lock.Merged)
	}
	if b, _ := os.ReadFile(path); string(b) != edited {
		t.Errorf("settings %s", b)
	}
	if b, _ := os.ReadFile(early); string(b) != "a v2\n" {
		t.Errorf("earlier file %q", b)
	}
}
