package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/lock"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestSetupEndsWithSavedAndDone(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", false, "--source", src)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "saved   ") || !strings.HasPrefix(lastLine(out), "done    ") {
		t.Errorf("first setup output: %s", out)
	}
	code, out, errs = runSetup(t, env, "", false, "--source", src)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if strings.Contains(out, "saved   ") || strings.Contains(out, "done    ") {
		t.Errorf("unchanged setup prints saved or done: %s", out)
	}
}

func TestUpdateDoneAndPlanLines(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	code, out, errs := runUpdate(t, env, "--plan")
	if code != 0 || lastLine(out) != planLine {
		t.Fatalf("plan: exit %d: %s%s", code, out, errs)
	}
	code, out, errs = runUpdate(t, env)
	if code != 0 || !strings.HasPrefix(lastLine(out), "done    ") {
		t.Fatalf("update: exit %d: %s%s", code, out, errs)
	}
	code, out, _ = runUpdate(t, env)
	if code != 0 || strings.Contains(out, "done    ") || !strings.Contains(out, "up to date") {
		t.Errorf("second update: %d %s", code, out)
	}
}

func TestUpdateAndUninstallProjectNote(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	env.Dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(env.Dir, ".livery"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, ".livery", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runUpdate(t, env)
	if strings.Count(out, "note    this folder is a livery project (") != 1 || !strings.Contains(out, "livery update --project") {
		t.Errorf("update: %s", out)
	}
	_, out, _ = runUninstall(t, env)
	if strings.Count(out, "note    this folder is a livery project (") != 1 || !strings.Contains(out, "livery uninstall --project") {
		t.Errorf("uninstall: %s", out)
	}
}

func TestUninstallKeptPersonalAndDone(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	personal := paths.PersonalDir(env)
	if err := os.MkdirAll(personal, 0o755); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runUninstall(t, env, "--plan")
	if lastLine(out) != planLine {
		t.Errorf("uninstall --plan: %s", out)
	}
	code, out, errs := runUninstall(t, env)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if n := strings.Count(out, "kept    "); n != 2 {
		t.Errorf("kept lines = %d: %s", n, out)
	}
	last := lastLine(out)
	if !strings.HasPrefix(last, "done    ") || !strings.Contains(last, "to install again: livery update") {
		t.Errorf("last line %q", last)
	}
}

func TestSetupDoneCountMatchesListAndNamesConfig(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", false, "--source", src)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	listed := 0
	for _, l := range strings.Split(out, "\n") {
		for _, verb := range []string{"write   ", "remove  ", "adopt   ", "set     ", "unset   "} {
			if strings.HasPrefix(l, verb) {
				listed++
			}
		}
	}
	want := fmt.Sprintf("done    %d change(s) applied", listed)
	if listed == 0 || lastLine(out) != want {
		t.Errorf("last line %q, want %q (list lines %d):\n%s", lastLine(out), want, listed, out)
	}
	if !strings.Contains(out, "saved   "+lock.ScopeOf(env).Show(paths.ConfigFile(env))+"\n") {
		t.Errorf("no saved line naming the config:\n%s", out)
	}
}

func TestSetupProjectHintComesBeforeDone(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--harness", "kiro")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.HasPrefix(lastLine(out), "done    ") || !strings.Contains(out, "commit .livery/") {
		t.Errorf("done is not last, or no hint:\n%s", out)
	}
}
