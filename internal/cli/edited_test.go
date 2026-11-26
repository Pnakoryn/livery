package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

func TestUpdatePlanWithEdited(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	skill := filepath.Join(env.Home, ".kiro/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enginetest.Commit(t, src, map[string]string{
		"base/skills/review/SKILL.md": "skill v2\n",
		"base/instructions/go.md":     "go rules v2\n",
	}, "v0.2.0")
	code, out, errs := runUpdate(t, env, "--plan")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "edited by you: ~/.kiro/skills/review/SKILL.md") || !strings.Contains(out, "+go rules v2") || strings.Contains(out, "edited  ") {
		t.Errorf("output: %s", out)
	}
}

func TestUpdateOnlyEditedWritesNothingNew(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	skill := filepath.Join(env.Home, ".kiro/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != int(exit.Finding) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, bad := range []string{"write   ", "remove  ", "set     ", "unset   "} {
		if strings.Contains(out, bad) {
			t.Errorf("%q in output: %s", bad, out)
		}
	}
	if strings.Contains(out, "edited by you:") || !strings.Contains(out, "edited  ~/.kiro/skills/review/SKILL.md: ") || strings.Contains(out, "up to date") {
		t.Errorf("output: %s", out)
	}
	if b, _ := os.ReadFile(skill); string(b) != "mine\n" {
		t.Errorf("skill: %q", b)
	}
}

func TestUpdateEditedFromPersonalLayer(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	personal := filepath.Join(paths.PersonalDir(env), "instructions", "mine.md")
	if err := os.MkdirAll(filepath.Dir(personal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personal, []byte("my rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	installed := filepath.Join(env.Home, ".kiro/steering/mine.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != int(exit.Finding) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	want := "edited  ~/.kiro/steering/mine.md: you changed it; livery kept your version\n" +
		"        to keep your change: put it into ~/.config/livery/personal/instructions/mine.md, delete ~/.kiro/steering/mine.md, then run livery update\n"
	if !strings.Contains(out, want) {
		t.Errorf("output: %s", out)
	}
}

func TestDoctorEditedInProjectNamesNoPersonalLayer(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)
	if err := os.WriteFile(filepath.Join(proj, ".claude/rules/go.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env, "--project")
	if code != int(exit.Finding) || !strings.Contains(out, "edited: .claude/rules/go.md -> delete it, then livery update --project") || strings.Contains(out, "personal") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}
