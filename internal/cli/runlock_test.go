package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

func TestRunLock(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)

	// Two sequential runs: the first releases the lock.
	for i := 0; i < 2; i++ {
		if code, out, errs := runUpdate(t, env); code != 0 {
			t.Fatalf("update %d: exit %d: %s%s", i, code, out, errs)
		}
	}

	path := filepath.Join(paths.CacheDir(env), "run.lock")
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("run.lock: %v", err)
	}
	defer func() { _ = f.Close() }()
	// flock locks belong to the open file description, so a second open in this process
	// conflicts like another process would.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	skill := filepath.Join(env.Home, ".kiro", "skills", "review", "SKILL.md")
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	before := tree(t, env.Home)
	code, out, errs := runUpdate(t, env)
	if code != 3 || !hasAll(errs, path, "in progress", "livery update") {
		t.Errorf("update: exit %d: %s%s", code, out, errs)
	}
	if after := tree(t, env.Home); !equal(before, after) {
		t.Errorf("update wrote while locked")
	}
	if code, _, errs := runUninstall(t, env); code != 3 || !hasAll(errs, path, "in progress", "livery uninstall") {
		t.Errorf("uninstall: exit %d: %s", code, errs)
	}

	if code, out, errs := runUpdate(t, env, "--plan"); code != 0 || !strings.Contains(out, "write   ~/.kiro/skills/review/SKILL.md") {
		t.Errorf("update --plan: exit %d: %s%s", code, out, errs)
	}
	if code, out, errs := runDoctor(t, env); code == 3 || strings.Contains(errs, "another livery run") {
		t.Errorf("doctor: exit %d: %s%s", code, out, errs)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("update after unlock: exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("skill not restored: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("run.lock removed: %v", err)
	}
}
