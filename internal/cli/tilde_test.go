package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

// homeSource moves a tagged source repository to <home>/src and returns env.
func homeSource(t *testing.T) adapter.Env {
	t.Helper()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	if err := os.Rename(src, filepath.Join(env.Home, "src")); err != nil {
		t.Skipf("cannot move the source: %v", err)
	}
	return env
}

func TestSetupSourceTildeFlag(t *testing.T) {
	t.Parallel()
	env := homeSource(t)
	code, out, errs := runSetup(t, env, "", false, "--source=~/src", "--harness", "kiro", "--team", "")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if c := loadCfg(t, env); c.Source != filepath.Join(env.Home, "src") {
		t.Fatalf("source %q", c.Source)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".kiro", "skills", "review", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestSetupSourceTildeAnswer(t *testing.T) {
	t.Parallel()
	env := homeSource(t)
	env.Dir = t.TempDir()
	code, out, errs := runSetup(t, env, "~/src\n", true, "--harness", "kiro", "--team", "", "--plan")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, filepath.Join(env.Home, "src")) {
		t.Fatalf("output: %s", out)
	}
}

func TestImportIntoTilde(t *testing.T) {
	env := enginetest.Env(t)
	kiroFolder(t, env, "")
	wd := t.TempDir()
	t.Chdir(wd)
	code, out, errs := runImport(t, env, "", false, "--from", "kiro", "--into=~/out")
	if code != 0 {
		t.Fatalf("flag: exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(env.Home, "out", "livery.json")); err != nil {
		t.Fatal(err)
	}
	code, out, errs = runImport(t, env, "~/out2\n\n", true, "--from", "kiro")
	if code != 0 {
		t.Fatalf("prompt: exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(env.Home, "out2", "livery.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wd, "~")); err == nil {
		t.Fatal("a folder named ~ was created")
	}
}

func TestCheckTildeDir(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	if err := os.Rename(checkCopy(t), filepath.Join(env.Home, "src")); err != nil {
		t.Skipf("cannot move the folder: %v", err)
	}
	code, out, errs := runCLI(t, env, "", false, "check", "~/src")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}

func TestUpdateLeftEditedSaysItLeftTheRelease(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	enginetest.Commit(t, src, map[string]string{"base/instructions/go.md": ""}, "v0.2.0")
	steer := filepath.Join(env.Home, ".kiro/steering/go.md")
	if err := os.WriteFile(steer, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	want := "left, edited by you: ~/.kiro/steering/go.md (no longer in the release; the file is yours now, delete it to remove it)"
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in %s", want, out)
	}
	if _, err := os.Stat(steer); err != nil {
		t.Error(err)
	}
}

func TestSetupSourceWithoutCommitSaysWhatToDo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // not a git repository
	env := enginetest.Env(t)
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", false, "--source", dir)
	if code != 3 || !hasAll(out+errs, "git tag v0.1.0", "then run livery setup") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}
