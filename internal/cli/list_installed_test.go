package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

func installedSetup(t *testing.T) (adapter.Env, string) {
	t.Helper()
	src := enginetest.Source(t, map[string]string{
		"livery.json":                       `{"schema":1}`,
		"base/skills/review/SKILL.md":       "skill v1\n",
		"teams/web/instructions/commits.md": "commit rules\n",
	}, "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	pd := paths.PersonalDir(env)
	if err := os.MkdirAll(filepath.Join(pd, "mcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "mcp", "docs.json"), []byte(`{"command":"docs-mcp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runSetup(t, env, "", false, "--source", src, "--team", "web", "--harness", "kiro"); code != 0 {
		t.Fatalf("setup %d: %s%s", code, out, errs)
	}
	return env, src
}

// installedFile is the path of the file below ~/.kiro whose path holds name, wherever the
// adapter put it; the test fails when there is none.
func installedFile(t *testing.T, env adapter.Env, name string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(filepath.Join(env.Home, ".kiro"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(p, name) {
			found = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("no installed file with %q below ~/.kiro", name)
	}
	return found
}

func lineWith(out string, words ...string) string {
	for _, l := range strings.Split(out, "\n") {
		ok := true
		for _, w := range words {
			if !strings.Contains(l, w) {
				ok = false
			}
		}
		if ok {
			return l
		}
	}
	return ""
}

func TestListInstalled(t *testing.T) {
	t.Parallel()
	env, src := installedSetup(t)
	before := tree(t, env.Home)
	code, out, errs := runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, src) || !strings.Contains(first, "v0.1.0") {
		t.Errorf("header %q", first)
	}
	for _, w := range [][]string{{"skill", "review", "base"}, {"instruction", "commits", "team web"}, {"mcp", "docs", "personal"}} {
		l := lineWith(out, w...)
		if l == "" {
			t.Errorf("no line with %v in\n%s", w, out)
			continue
		}
		if strings.HasSuffix(l, "pending") || strings.HasSuffix(l, "edited") {
			t.Errorf("state word on %q", l)
		}
	}
	if !strings.Contains(out, "yours\n    (nothing)") {
		t.Errorf("yours:\n%s", out)
	}
	if after := tree(t, env.Home); len(after) != len(before) {
		t.Error("files changed")
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Errorf("%s changed", k)
			}
		}
	}
}

func TestListInstalledEditedPendingYours(t *testing.T) {
	t.Parallel()
	env, src := installedSetup(t)
	ins := installedFile(t, env, "commits")
	if err := os.WriteFile(ins, []byte("changed by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(env.Home, ".kiro", "skills", "mine")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("---\nname: mine\ndescription: d\n---\nmine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")

	code, out, errs := runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if l := lineWith(out, "instruction", "commits"); !strings.HasSuffix(l, "edited") {
		t.Errorf("instruction line %q in\n%s", l, out)
	}
	// The newer release was not fetched: no note, and the installed skill has no state.
	if strings.Contains(out, "is available") {
		t.Errorf("note without a fetch:\n%s", out)
	}
	if l := lineWith(out, "skill", "review"); l == "" || strings.HasSuffix(l, "pending") {
		t.Errorf("skill line %q", l)
	}
	i := strings.Index(out, "yours\n")
	if i < 0 {
		t.Fatalf("no yours:\n%s", out)
	}
	yours := out[i:]
	if l := lineWith(yours, "kiro", "mine"); l == "" {
		t.Errorf("mine missing:\n%s", yours)
	}
	if strings.Contains(yours, "review") {
		t.Errorf("installed skill under yours:\n%s", yours)
	}
}

func TestListInstalledUsage(t *testing.T) {
	t.Parallel()
	env, src := installedSetup(t)
	if code, _, errs := runList(t, env, "--installed", "--source", src); code != 2 || !strings.Contains(errs, "takes no --source or --channel") {
		t.Errorf("exit %d: %s", code, errs)
	}
	if code, _, _ := runList(t, env, "--installed", "--channel", "next"); code != 2 {
		t.Errorf("--channel: exit %d", code)
	}
	if code, _, errs := runList(t, enginetest.Env(t), "--installed"); code != 2 || !strings.Contains(errs, "livery setup") {
		t.Errorf("no lock: exit %d: %s", code, errs)
	}
}

func TestListInstalledProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env, proj := projEnv(t)
	if code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--harness", "kiro", "--team", "web"); code != 0 {
		t.Fatalf("setup: %d %s%s", code, out, errs)
	}
	before := tree(t, proj, env.Home)
	code, out, errs := runList(t, env, "--installed", "--project")
	if code != 0 || !strings.Contains(out, "ui") || !strings.Contains(out, "team web") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	after := tree(t, proj, env.Home)
	if len(after) != len(before) {
		t.Error("files changed")
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed", k)
		}
	}
}

// A shared list element (opencode's "instructions" glob) has the origin of only the first
// instruction: it must not put its state on that instruction.
func TestListInstalledSharedListEntryShowsNoState(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, map[string]string{
		"livery.json":                 `{"schema":1}`,
		"base/instructions/alpha.md":  "alpha rules\n",
		"base/instructions/beta.md":   "beta rules\n",
		"base/skills/review/SKILL.md": "skill v1\n",
	}, "v0.1.0")
	env := enginetest.Env(t)
	cfgDir := filepath.Join(env.Home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runSetup(t, env, "", false, "--source", src, "--harness", "opencode"); code != 0 {
		t.Fatalf("setup %d: %s%s", code, out, errs)
	}
	// Drop the instructions entry from the shared config: the list element is now pending.
	cfg := filepath.Join(cfgDir, "opencode.json")
	if err := os.WriteFile(cfg, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, name := range []string{"alpha", "beta"} {
		l := lineWith(out, "instruction", name)
		if l == "" {
			t.Fatalf("no line for %s in\n%s", name, out)
		}
		if strings.HasSuffix(l, "pending") || strings.HasSuffix(l, "edited") || strings.HasSuffix(l, "yours") {
			t.Errorf("state on %q", l)
		}
	}
}
