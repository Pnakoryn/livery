package apply

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

// runProject computes and executes plans in the project scope of project, failing on an error.
func runProject(t *testing.T, plans []adapter.Plan, old lock.Lock, project string) Result {
	t.Helper()
	res, err := Compute(plans, old, projectMeta, lock.Scope{Home: t.TempDir(), Project: project}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(res); err != nil {
		t.Fatal(err)
	}
	return res
}

func onlyOps(t *testing.T, res Result, want ...Op) {
	t.Helper()
	var got []Op
	for _, a := range res.Actions {
		got = append(got, a.Op)
	}
	if strings.Join(opStrings(got), ",") != strings.Join(opStrings(want), ",") {
		t.Fatalf("ops %v, want %v (actions %+v)", got, want, res.Actions)
	}
}

func opStrings(ops []Op) []string {
	var out []string
	for _, o := range ops {
		out = append(out, string(o))
	}
	return out
}

// In machine scope a linked harness folder is followed (TestLinkedRootIsUsed); in project scope
// it is the user's.
func TestProjectLinkedRootIsKept(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".h")
	check := linked(t, root, filepath.Join(t.TempDir(), "elsewhere"), map[string]string{"other.md": "other\n"})
	plans := []adapter.Plan{{Root: root, Files: []adapter.File{
		{Path: filepath.Join(root, "skills", "a", "SKILL.md"), Data: []byte(wantData), Origin: "o"},
	}}}

	res := runProject(t, plans, lock.Lock{}, project)
	onlyOps(t, res, KeptYours)
	if len(res.Lock.Files) != 0 {
		t.Errorf("lock files %v", res.Lock.Files)
	}
	check()

	// A file livery installed before the folder became a link is left, not removed.
	old := lock.Lock{Files: map[string]string{".h/other.md": lock.Sum([]byte("other\n"))}}
	res = runProject(t, []adapter.Plan{{Root: root}}, old, project)
	onlyOps(t, res, LeftEdited)
	check()
}

func TestProjectLinkedRootSharedFileIsRefused(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".h")
	check := linked(t, root, filepath.Join(t.TempDir(), "elsewhere"), map[string]string{"mcp.json": `{"servers":{}}`})
	entry := adapter.Entry{Path: filepath.Join(root, "mcp.json"), Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`), Origin: "o"}
	scope := lock.Scope{Home: t.TempDir(), Project: project}

	for _, tc := range []struct {
		name  string
		plans []adapter.Plan
		old   lock.Lock
	}{
		{"install", []adapter.Plan{{Root: root, Entries: []adapter.Entry{entry}}}, lock.Lock{}},
		{"uninstall", []adapter.Plan{{Root: root}}, lock.Lock{Merged: []lock.Merged{{Path: ".h/mcp.json", Key: "servers", Entries: map[string]string{"x": "sha256:0"}}}}},
	} {
		_, err := Compute(tc.plans, tc.old, projectMeta, scope, tc.name)
		var e *exit.Error
		if !errors.As(err, &e) || e.Code != exit.Finding || !strings.HasPrefix(err.Error(), root+": is a symbolic link") {
			t.Errorf("%s: err = %v, want a Finding naming %s", tc.name, err, root)
		}
		if err != nil && !strings.Contains(err.Error(), "then run livery "+tc.name+" --project") {
			t.Errorf("%s: err = %v, want the hint to name livery %s --project", tc.name, err, tc.name)
		}
		check()
	}
}

// A target outside the harness folder, below a linked folder of the project.
func TestProjectLinkedFolderOfTarget(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".h")
	check := linked(t, filepath.Join(project, "a"), filepath.Join(t.TempDir(), "elsewhere"), map[string]string{"other.txt": "other\n"})
	plans := []adapter.Plan{{Root: root, Files: []adapter.File{
		{Path: filepath.Join(project, "a", "b.txt"), Data: []byte(wantData), Origin: "o"},
	}}}

	res := runProject(t, plans, lock.Lock{}, project)
	onlyOps(t, res, KeptYours)
	if res.Actions[0].Path != filepath.Join(project, "a", "b.txt") || len(res.Lock.Files) != 0 {
		t.Errorf("actions %+v, lock files %v", res.Actions, res.Lock.Files)
	}
	check()
}

// A removed shared JSON file below a real harness folder still prunes its empty folders in
// project scope.
func TestProjectRemovedSharedFilePrunes(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".h")
	entry := adapter.Entry{Path: filepath.Join(root, "settings", "mcp.json"), Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`), Origin: "o"}
	res := runProject(t, []adapter.Plan{{Root: root, Entries: []adapter.Entry{entry}}}, lock.Lock{}, project)
	runProject(t, []adapter.Plan{{Root: root}}, res.Lock, project)
	gone(t, filepath.Join(root, "settings"))
}
