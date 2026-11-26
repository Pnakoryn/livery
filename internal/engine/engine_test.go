package engine

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
)

func fixture() map[string]string {
	return map[string]string{
		"livery.json":                      `{"schema":1}`,
		"base/skills/review/SKILL.md":      "---\nname: review\ndescription: d\n---\nbase review\n",
		"base/instructions/go.md":          "base go\n",
		"base/mcp/github.json":             `{"command":"gh"}`,
		"teams/web/skills/review/SKILL.md": "---\nname: review\ndescription: d\n---\nteam review\n",
	}
}

func cfgFor(src string, teams ...string) config.Config {
	return config.Config{Source: src, Channel: "stable", Teams: teams, Harnesses: []string{"kiro"}}
}

func count(r apply.Result, op apply.Op) int {
	n := 0
	for _, a := range r.Actions {
		if a.Op == op {
			n++
		}
	}
	return n
}

func TestPrepareBase(t *testing.T) {
	t.Parallel()
	f := fixture()
	delete(f, "teams/web/skills/review/SKILL.md")
	src := enginetest.Source(t, f, "v0.1.0")
	env := enginetest.Env(t)
	p, err := Prepare(nil, env, "test", cfgFor(src))
	if err != nil {
		t.Fatal(err)
	}
	if p.Resolved.Tag != "v0.1.0" {
		t.Errorf("tag = %q", p.Resolved.Tag)
	}
	if n := count(p.Result, apply.Write); n != 2 {
		t.Errorf("writes = %d, want 2", n)
	}
	if n := count(p.Result, apply.SetEntry); n != 1 {
		t.Errorf("set = %d, want 1", n)
	}
	for _, a := range p.Result.Actions {
		if !strings.HasPrefix(a.Path, filepath.Join(env.Home, ".kiro")+"/") {
			t.Errorf("path outside .kiro: %s", a.Path)
		}
	}
	ents, _ := os.ReadDir(env.Home)
	for _, e := range ents {
		if e.Name() != ".cache" {
			t.Errorf("unexpected %s in home", e.Name())
		}
	}
}

func TestPrepareTeamOverride(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, fixture(), "v0.1.0")
	p, err := Prepare(nil, enginetest.Env(t), "test", cfgFor(src, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Overrides) != 1 {
		t.Errorf("overrides = %d", len(p.Overrides))
	}
	if !reflect.DeepEqual(p.Teams, []string{"web"}) {
		t.Errorf("teams = %v", p.Teams)
	}
	found := false
	for _, a := range p.Result.Actions {
		if strings.HasSuffix(a.Path, "skills/review/SKILL.md") {
			found = true
			if !strings.Contains(string(a.New), "team review") {
				t.Errorf("content = %q", a.New)
			}
		}
	}
	if !found {
		t.Error("skill not written")
	}
}

func TestTeamsSkipsDotFolders(t *testing.T) {
	t.Parallel()
	f := fixture()
	f["teams/.git/x.txt"] = "x"
	src := enginetest.Source(t, f, "v0.1.0")
	teams, _, err := Teams(nil, enginetest.Env(t), src, "stable")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(teams, []string{"web"}) {
		t.Errorf("teams = %v", teams)
	}
}

func TestPrepareUnknownTeam(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, fixture(), "v0.1.0")
	_, err := Prepare(nil, enginetest.Env(t), "test", cfgFor(src, "nope"))
	var ee *exit.Error
	if !errors.As(err, &ee) || ee.Code != exit.Usage {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "web") {
		t.Errorf("message = %q", err)
	}
}

func TestPreparePersonalWins(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, fixture(), "v0.1.0")
	env := enginetest.Env(t)
	f := filepath.Join(paths.PersonalDir(env), "instructions", "go.md")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("personal go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Prepare(nil, env, "test", cfgFor(src))
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, a := range p.Result.Actions {
		if strings.HasSuffix(a.Path, "steering/go.md") && strings.Contains(string(a.New), "personal go") {
			ok = true
		}
	}
	if !ok {
		t.Error("personal instruction did not win")
	}
}

func TestPrepareUnknownHarness(t *testing.T) {
	t.Parallel()
	f := fixture()
	f["base/cursor/x.json"] = "{}"
	src := enginetest.Source(t, f, "v0.1.0")
	_, err := Prepare(nil, enginetest.Env(t), "test", cfgFor(src))
	var ee *exit.Error
	if !errors.As(err, &ee) || ee.Code != exit.Usage {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "cursor/x.json") || !strings.Contains(err.Error(), "kiro") {
		t.Errorf("message = %q", err)
	}
}

func TestPrepareDroppedHarnessRemoves(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, fixture(), "v0.1.0")
	env := enginetest.Env(t)
	kiroFile := filepath.Join(env.Home, ".kiro", "steering", "go.md")
	data := []byte("old\n")
	if err := os.MkdirAll(filepath.Dir(kiroFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kiroFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	l := lock.Lock{Livery: "old", Source: src, Channel: "stable", Commit: "x",
		Files: map[string]string{"~/.kiro/steering/go.md": lock.Sum(data)}}
	if err := l.Write(paths.LockFile(env)); err != nil {
		t.Fatal(err)
	}
	cfg := cfgFor(src)
	cfg.Harnesses = []string{}
	p, err := Prepare(nil, env, "test", cfg)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, a := range p.Result.Actions {
		if a.Op == apply.Remove && a.Path == kiroFile {
			removed = true
		}
	}
	if !removed {
		t.Errorf("no Remove: %+v", p.Result.Actions)
	}
	if !reflect.DeepEqual(p.Result.Roots, []string{filepath.Join(env.Home, ".claude"), filepath.Join(env.Home, ".config", "opencode"), filepath.Join(env.Home, ".kiro")}) {
		t.Errorf("roots = %v", p.Result.Roots)
	}
}

var _ adapter.Adapter = Adapters()[0]

func TestPrepareKiroAndClaude(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, fixture(), "v0.1.0")
	env := enginetest.Env(t)
	cfg := cfgFor(src)
	cfg.Harnesses = []string{"kiro", "claude"}
	p, err := Prepare(nil, env, "test", cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		filepath.Join(env.Home, ".kiro", "skills", "review", "SKILL.md"):   false,
		filepath.Join(env.Home, ".claude", "skills", "review", "SKILL.md"): false,
	}
	for _, a := range p.Result.Actions {
		if _, ok := want[a.Path]; ok {
			want[a.Path] = true
		}
	}
	for path, ok := range want {
		if !ok {
			t.Errorf("no action for %s", path)
		}
	}
}

func TestPrepareUnknownTeamNoTeamsDir(t *testing.T) {
	t.Parallel()
	f := fixture()
	delete(f, "teams/web/skills/review/SKILL.md")
	src := enginetest.Source(t, f, "v0.1.0")
	_, err := Prepare(nil, enginetest.Env(t), "test", cfgFor(src, "nope"))
	if err == nil || !strings.Contains(err.Error(), "the shared repository has no teams") {
		t.Errorf("err = %v", err)
	}
}

func TestPreparePersonalNotDirectory(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, fixture(), "v0.1.0")
	env := enginetest.Env(t)
	pd := paths.PersonalDir(env)
	if err := os.MkdirAll(filepath.Dir(pd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pd, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(nil, env, "test", cfgFor(src))
	if err == nil || !strings.Contains(err.Error(), pd) || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %v", err)
	}
}

func TestPrepareReadErrorsNameNextCommand(t *testing.T) {
	t.Parallel()
	// The personal layer cannot be read: its parent is a file.
	src := enginetest.Source(t, fixture(), "v0.1.0")
	env := enginetest.Env(t)
	if err := os.MkdirAll(filepath.Dir(paths.ConfigDir(env)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigDir(env), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(nil, env, "test", cfgFor(src))
	if err == nil || !strings.Contains(err.Error(), "cannot read") || !strings.Contains(err.Error(), "livery update") {
		t.Errorf("personal layer: err = %v", err)
	}

	// teams in the shared repository is a file, not a directory.
	f := fixture()
	delete(f, "teams/web/skills/review/SKILL.md")
	f["teams"] = "not a directory\n"
	src = enginetest.Source(t, f, "v0.1.0")
	env2 := enginetest.Env(t)
	_, err = Prepare(nil, env2, "test", cfgFor(src))
	if err == nil || !strings.Contains(err.Error(), "cannot read") || !strings.Contains(err.Error(), "livery update") ||
		!strings.Contains(err.Error(), "teams in the shared repository is a file") || !strings.Contains(err.Error(), paths.CacheDir(env2)) {
		t.Errorf("teams: err = %v", err)
	}
}
