package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
)

// TestPrepareProjectNoPersonal: in project scope the personal layer is not read. Adapters
// write below the project root.
func TestPrepareProjectNoPersonal(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, fixture(), "v0.1.0")
	env := enginetest.Env(t)
	project := t.TempDir()
	personal := filepath.Join(paths.ConfigDir(env), "personal", "skills", "mine", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(personal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personal, []byte("---\nname: mine\ndescription: d\n---\nmine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	has := func(p Prepared) bool {
		for _, a := range p.Result.Actions {
			if strings.Contains(a.Path, "/mine/") {
				return true
			}
		}
		return false
	}

	machine, err := Prepare(nil, env, "test", cfgFor(src))
	if err != nil {
		t.Fatal(err)
	}
	if !has(machine) {
		t.Fatal("machine scope: the personal skill is not installed; the test proves nothing")
	}

	env.Project = project
	p, err := Prepare(nil, env, "test", cfgFor(src))
	if err != nil {
		t.Fatal(err)
	}
	if has(p) {
		t.Error("project scope installs the personal skill")
	}
	if p.Result.Project != project {
		t.Errorf("Result.Project = %q", p.Result.Project)
	}
	now, err := apply.Execute(p.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := now.Write(paths.LockFile(env)); err != nil {
		t.Fatal(err)
	}
	l, _, err := lock.ReadIn(filepath.Join(project, ".livery", "lock.json"), lock.ScopeOf(env))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Files) == 0 {
		t.Fatal("no files in the project lock")
	}
	for f := range l.Files {
		if !strings.HasPrefix(f, ".kiro/") {
			t.Errorf("lock path %q is not relative to the project", f)
		}
	}
	again, err := Prepare(nil, env, "test", cfgFor(src))
	if err != nil {
		t.Fatal(err)
	}
	if again.Result.Changes() {
		t.Errorf("second run changes: %+v", again.Result.Actions)
	}
	if n := count(again.Result, apply.Unchanged); n == 0 {
		t.Error("second run: nothing unchanged")
	}
}
