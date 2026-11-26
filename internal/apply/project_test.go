package apply

import (
	"bytes"
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

func harnessPlan(project string) []adapter.Plan {
	root := filepath.Join(project, ".kiro")
	return []adapter.Plan{{
		Root: root,
		Files: []adapter.File{
			{Path: filepath.Join(root, "skills", "review", "SKILL.md"), Data: []byte("review\n"), Origin: "base: skills/review"},
		},
		Entries: []adapter.Entry{
			{Path: filepath.Join(root, "settings", "mcp.json"), Key: "mcpServers", Name: "gh", Value: []byte(`{"command":"gh"}`), Origin: "base: mcp/gh"},
		},
	}}
}

var projectMeta = lock.Lock{Livery: "test", Source: "s", Commit: "c0ffee"}

func TestProjectLockRelative(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	scope := lock.Scope{Home: home, Project: project}
	plans := harnessPlan(project)

	res, err := Compute(plans, lock.Lock{}, projectMeta, scope, "")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	res.Home = home
	Report(&b, res, false)
	want := "set     .kiro/settings/mcp.json mcpServers.gh\nwrite   .kiro/skills/review/SKILL.md\n"
	if b.String() != want {
		t.Errorf("report:\n%s\nwant:\n%s", b.String(), want)
	}
	b.Reset()
	if err := Diff(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "+++ livery/.kiro/skills/review/SKILL.md") {
		t.Errorf("diff does not name the relative path:\n%s", b.String())
	}

	now, err := Execute(res)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(project, ".livery", "lock.json")
	if err := now.Write(lockPath); err != nil {
		t.Fatal(err)
	}
	got, found, err := lock.ReadIn(lockPath, scope)
	if err != nil || !found {
		t.Fatalf("ReadIn: %v, %v", found, err)
	}
	if !reflect.DeepEqual(keys(got.Files), []string{".kiro/skills/review/SKILL.md"}) {
		t.Errorf("files = %v", got.Files)
	}
	if len(got.Merged) != 1 || got.Merged[0].Path != ".kiro/settings/mcp.json" {
		t.Errorf("merged = %+v", got.Merged)
	}

	again, err := Compute(plans, got, projectMeta, scope, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Changes() {
		t.Errorf("second run changes: %+v", again.Actions)
	}
	if !reflect.DeepEqual(again.Lock.Files, got.Files) || !reflect.DeepEqual(again.Lock.Merged, got.Merged) {
		t.Errorf("second lock = %+v, want %+v", again.Lock, got)
	}

	// Without the item, the owned file and entry are removed.
	gone, err := Compute([]adapter.Plan{{Root: plans[0].Root}}, got, projectMeta, scope, "")
	if err != nil {
		t.Fatal(err)
	}
	if count := len(gone.Actions); count != 2 || gone.Actions[0].Op != UnsetEntry || gone.Actions[1].Op != Remove {
		t.Errorf("removal actions = %+v", gone.Actions)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestProjectRefusesOutside(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "p")
	scope := lock.Scope{Home: home, Project: project}
	for _, plan := range []adapter.Plan{
		{Files: []adapter.File{{Path: filepath.Join(home, ".kiro", "x.md"), Data: []byte("x")}}},
		{Files: []adapter.File{{Path: project + "x/y.md", Data: []byte("x")}}},
		{Files: []adapter.File{{Path: project, Data: []byte("x")}}},
		{Entries: []adapter.Entry{{Path: filepath.Join(project, "..", "mcp.json"), Key: "k", Name: "n", Value: []byte("1")}}},
	} {
		_, err := Compute([]adapter.Plan{plan}, lock.Lock{}, projectMeta, scope, "")
		var e *exit.Error
		if !errors.As(err, &e) || e.Code != exit.Finding || !strings.Contains(err.Error(), "refusing to write outside the project") {
			t.Errorf("plan %+v: err = %v, want exit 1 refusing to write outside the project", plan, err)
		}
	}
	if _, err := os.Stat(project); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("project folder created: %v", err)
	}
}

// TestProjectMessagesNoPersonalLayer: a project has no personal layer, so the messages for a
// file, entry or element wanted twice and an invalid JSON value do not point to it; the user
// scope still does.
func TestProjectMessagesNoPersonalLayer(t *testing.T) {
	for _, scoped := range []bool{true, false} {
		root := t.TempDir()
		path := filepath.Join(root, "x.json")
		scope := lock.Scope{Home: t.TempDir()}
		if scoped {
			scope.Project = root
		}
		for name, c := range map[string]struct {
			plans []adapter.Plan
			code  exit.Code
		}{
			"file": {[]adapter.Plan{
				{Files: []adapter.File{{Path: path, Data: []byte("1"), Origin: "team: one"}}},
				{Files: []adapter.File{{Path: path, Data: []byte("2"), Origin: "team: two"}}},
			}, exit.Finding},
			"entry": {[]adapter.Plan{{Entries: []adapter.Entry{
				{Path: path, Key: "k", Name: "n", Value: []byte(`1`), Origin: "team: one"},
				{Path: path, Key: "k", Name: "n", Value: []byte(`1`), Origin: "team: two"},
			}}}, exit.Finding},
			"element": {[]adapter.Plan{{Entries: []adapter.Entry{
				{Path: path, Key: "k", Name: "n", List: true, Origin: "team: one"},
				{Path: path, Key: "k", Name: "n", List: true, Origin: "team: two"},
			}}}, exit.Finding},
			"invalid": {[]adapter.Plan{{Entries: []adapter.Entry{
				{Path: path, Key: "k", Name: "n", Value: []byte(`{`), Origin: "team: one"},
			}}}, exit.Usage},
		} {
			_, err := Compute(c.plans, lock.Lock{}, projectMeta, scope, "")
			var e *exit.Error
			if !errors.As(err, &e) || e.Code != c.code {
				t.Fatalf("%s: err %v, want exit code %d", name, err, c.code)
			}
			if got := strings.Contains(e.Message, "personal layer"); got == scoped {
				t.Errorf("%s (project %v): message %q mentions personal layer: %v", name, scoped, e.Message, got)
			}
		}
	}
}
