package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

func runList(t *testing.T, env adapter.Env, args ...string) (int, string, string) {
	t.Helper()
	return runCLI(t, env, "", false, append([]string{"list"}, args...)...)
}

func listFiles() map[string]string {
	f := files("skill\n")
	f["base/kiro/hooks/lint.json"] = "{}"
	f["teams/web/instructions/css.md"] = "css\n"
	f["teams/data/skills/sql/SKILL.md"] = "sql\n"
	f["teams/empty/README.md"] = "x"
	return f
}

func TestListSourceNoConfig(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, listFiles(), "v1.0.0")
	env := enginetest.Env(t)
	code, out, errs := runList(t, env, "--source", src)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, want := range []string{"  base\n", "    skill        review\n", "    mcp          github\n",
		"    kiro         hooks/lint.json\n", "  team web\n", "  team data\n", "    instruction  css\n", "  team empty\n    (empty)\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "*") {
		t.Errorf("unexpected mark:\n%s", out)
	}
}

func TestListMarksChosenTeamAndWritesNothing(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, listFiles(), "v1.0.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	if code, out, errs := runSetup(t, env, "", false, "--source", src, "--team", "web"); code != 0 {
		t.Fatalf("setup %d: %s%s", code, out, errs)
	}
	cfgBefore, _ := os.ReadFile(paths.ConfigFile(env))
	lockBefore, _ := os.ReadFile(paths.LockFile(env))
	code, out, errs := runList(t, env)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "  team web *\n") || strings.Count(out, " *") != 1 {
		t.Errorf("marks wrong:\n%s", out)
	}
	cfgAfter, _ := os.ReadFile(paths.ConfigFile(env))
	lockAfter, _ := os.ReadFile(paths.LockFile(env))
	if !bytes.Equal(cfgBefore, cfgAfter) || !bytes.Equal(lockBefore, lockAfter) {
		t.Error("config or lock changed")
	}
}

func TestListUsageErrors(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	if code, _, _ := runList(t, env); code != 2 {
		t.Errorf("no config: exit %d", code)
	}
	if code, _, _ := runList(t, env, "--channel", "next"); code != 2 {
		t.Errorf("--channel alone: exit %d", code)
	}
}
