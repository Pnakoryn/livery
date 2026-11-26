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

func TestSetupProjectFetchErrorHint(t *testing.T) {
	t.Parallel()
	env, _ := projEnv(t)
	missing := filepath.Join(t.TempDir(), "missing")
	code, out, errs := runSetup(t, env, "", false, "--project", "--source", missing, "--harness", "kiro")
	if code != 3 || !strings.Contains(errs, "no such folder") ||
		!strings.HasSuffix(strings.TrimSpace(errs), "then run livery setup --project") ||
		strings.Contains(errs, "livery update") {
		t.Errorf("exit %d:\n%s%s", code, out, errs)
	}
}

func TestUpdateFetchErrorHintMachineScope(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	writeConfig(t, env, filepath.Join(t.TempDir(), "missing"))
	code, out, errs := runUpdate(t, env)
	if code != 3 || !strings.HasSuffix(strings.TrimSpace(errs), "then run livery update") || strings.Contains(errs, "--project") {
		t.Errorf("exit %d:\n%s%s", code, out, errs)
	}
}

func TestSetupRunLockHintNamesSetup(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	if err := os.MkdirAll(paths.CacheDir(env), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(paths.CacheDir(env), "run.lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runSetup(t, env, "", false, "--source", src, "--harness", "kiro")
	if code != 3 || !strings.HasSuffix(strings.TrimSpace(errs), "livery setup") {
		t.Errorf("exit %d:\n%s%s", code, out, errs)
	}
}
