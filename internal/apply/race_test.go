package apply

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

// executeChanged runs Execute on res after change altered the disk, and checks it returns the
// "changed while livery was running" Finding naming want and that it wrote nothing.
func executeChanged(t *testing.T, res Result, home string, change func(), want ...string) {
	t.Helper()
	change()
	before := snapshot(t, home)
	_, err := Execute(res)
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Finding {
		t.Fatalf("err %v, want a Finding", err)
	}
	if !strings.HasPrefix(err.Error(), "changed while livery was running: ") || !strings.HasSuffix(err.Error(), "; run livery update again") {
		t.Errorf("message: %s", err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("message %q does not name %s", err, w)
		}
	}
	if after := snapshot(t, home); after != before {
		t.Errorf("Execute changed the disk:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestExecuteNewFileAppeared(t *testing.T) {
	home := t.TempDir()
	res, err := compute(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".h", "skills", "a", "SKILL.md")
	executeChanged(t, res, home, func() { put(t, path, "theirs\n") }, path)
	if got, _ := os.ReadFile(path); string(got) != "theirs\n" {
		t.Errorf("content %q", got)
	}
	gone(t, filepath.Join(home, ".h", "skills", "b"))
}

func TestExecuteOwnedFileEdited(t *testing.T) {
	home := t.TempDir()
	first := run(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	next := skillPlan(home, true)
	next.Files[0].Data = []byte("a v2\n")
	next.Files[1].Data = []byte("#!/bin/sh\necho v2\n")
	res, err := compute(t, []adapter.Plan{next}, first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	path := next.Files[0].Path
	executeChanged(t, res, home, func() { put(t, path, "hand edit\n") }, path)
	if got, _ := os.ReadFile(next.Files[1].Path); string(got) != "#!/bin/sh\n" {
		t.Errorf("run.sh written: %q", got)
	}
}

func TestExecuteFileToRemoveEdited(t *testing.T) {
	home := t.TempDir()
	first := run(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	next := skillPlan(home, false)
	next.Files[1].Data = []byte("#!/bin/sh\necho v2\n")
	res, err := compute(t, []adapter.Plan{next}, first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".h", "skills", "b", "deep", "SKILL.md")
	executeChanged(t, res, home, func() { put(t, path, "hand edit\n") }, path)
	if got, _ := os.ReadFile(path); string(got) != "hand edit\n" {
		t.Errorf("content %q", got)
	}
}

func TestExecuteEntryEdited(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "settings.json")
	other := filepath.Join(home, ".h", "skills", "a", "SKILL.md")
	x := adapter.Entry{Path: path, Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`)}
	plan := func(entries ...adapter.Entry) []adapter.Plan {
		p := entryPlan(home, entries...)
		p[0].Files = []adapter.File{{Path: other, Data: []byte("a\n")}}
		return p
	}
	first := run(t, plan(x), lock.Lock{}, home)

	x2 := x
	x2.Value = json.RawMessage(`{"cmd":"x2"}`)
	y := adapter.Entry{Path: path, Key: "servers", Name: "y", Value: json.RawMessage(`{"cmd":"y"}`)}
	next := plan(x2, y)
	next[0].Files[0].Data = []byte("a v2\n")

	t.Run("entry edited", func(t *testing.T) {
		res, err := compute(t, next, first.Lock, home)
		if err != nil {
			t.Fatal(err)
		}
		edited := `{"theme": "dark", "servers": {"x": {"cmd": "mine"}}}`
		executeChanged(t, res, home, func() { put(t, path, edited) }, path+" servers.x")
		if got, _ := os.ReadFile(path); string(got) != edited {
			t.Errorf("settings %s", got)
		}
		if got, _ := os.ReadFile(other); string(got) != "a\n" {
			t.Errorf("other file written: %q", got)
		}
	})

	t.Run("new entry appeared", func(t *testing.T) {
		put(t, path, `{"servers": {"x": {"cmd":"x"}}}`)
		res, err := compute(t, next, first.Lock, home)
		if err != nil {
			t.Fatal(err)
		}
		executeChanged(t, res, home, func() {
			put(t, path, `{"servers": {"x": {"cmd":"x"}, "y": {"cmd":"theirs"}}}`)
		}, path+" servers.y")
	})

	t.Run("other keys changed", func(t *testing.T) {
		put(t, path, `{"servers": {"x": {"cmd":"x"}}}`)
		res, err := compute(t, next, first.Lock, home)
		if err != nil {
			t.Fatal(err)
		}
		// Reformatting x and adding keys livery does not touch does not block.
		put(t, path, `{"theme": "dark", "servers": {"x": { "cmd" : "x" }, "z": 1}}`)
		if _, err := Execute(res); err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		data, _ := os.ReadFile(path)
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		servers := doc["servers"].(map[string]any)
		if doc["theme"] != "dark" || servers["z"] != float64(1) || servers["x"].(map[string]any)["cmd"] != "x2" || servers["y"] == nil {
			t.Errorf("doc %s", data)
		}
		if got, _ := os.ReadFile(other); string(got) != "a v2\n" {
			t.Errorf("other file %q", got)
		}
	})
}
