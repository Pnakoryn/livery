package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

func files(skill string) map[string]string {
	return map[string]string{
		"livery.json":                 `{"schema":1}`,
		"base/skills/review/SKILL.md": skill,
		"base/instructions/go.md":     "go rules\n",
		"base/mcp/github.json":        `{"command":"gh-mcp"}`,
	}
}

func runUpdate(t *testing.T, env adapter.Env, args ...string) (int, string, string) {
	t.Helper()
	return runCLI(t, env, "", false, append([]string{"update"}, args...)...)
}

func writeConfig(t *testing.T, env adapter.Env, src string, teams ...string) {
	t.Helper()
	c := config.Config{Source: src, Channel: "stable", Teams: teams, Harnesses: []string{"kiro"}}
	if err := c.Save(paths.ConfigFile(env)); err != nil {
		t.Fatal(err)
	}
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func TestUpdateJourney(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)

	code, out, errs := runUpdate(t, env)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !regexp.MustCompile(`^livery update: ` + regexp.QuoteMeta(src) + ` v0\.1\.0 \([0-9a-f]{12}\)\n`).MatchString(out) {
		t.Errorf("header: %q", out)
	}
	if n := strings.Count(out, "write   "); n != 2 {
		t.Errorf("write lines = %d: %s", n, out)
	}
	if n := strings.Count(out, "set     "); n != 1 {
		t.Errorf("set lines = %d: %s", n, out)
	}
	for _, p := range []string{".kiro/skills/review/SKILL.md", ".kiro/steering/go.md", ".kiro/settings/mcp.json"} {
		if _, err := os.Stat(filepath.Join(env.Home, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	mcp, _ := os.ReadFile(filepath.Join(env.Home, ".kiro/settings/mcp.json"))
	if !strings.Contains(string(mcp), "github") {
		t.Errorf("mcp.json: %s", mcp)
	}
	lk, err := os.ReadFile(paths.LockFile(env))
	if err != nil || !strings.Contains(string(lk), `"commit"`) {
		t.Fatalf("lock: %v %s", err, lk)
	}

	// second run: up to date, nothing changes
	before := tree(t, env.Home)
	code, out, _ = runUpdate(t, env)
	if code != 0 || !strings.Contains(out, "up to date") {
		t.Errorf("second run: %d %q", code, out)
	}
	if after := tree(t, env.Home); !equal(before, after) {
		t.Error("second run changed files")
	}

	// lock deleted: adopted again
	if err := os.Remove(paths.LockFile(env)); err != nil {
		t.Fatal(err)
	}
	code, out, errs = runUpdate(t, env)
	if code != 0 || strings.Count(out, "adopt   ") != 3 || strings.Contains(out, "write   ") {
		t.Fatalf("no lock: %d %q %q", code, out, errs)
	}
	if b, _ := os.ReadFile(paths.LockFile(env)); string(b) != string(lk) {
		t.Errorf("lock not restored: %s", b)
	}
}

func TestUpdateNewTagAndEdited(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	enginetest.Commit(t, src, map[string]string{
		"base/skills/review/SKILL.md": "skill v2\n",
		"base/instructions/go.md":     "",
	}, "v0.2.0")

	skill := filepath.Join(env.Home, ".kiro/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != 1 {
		t.Fatalf("edited: exit %d: %s%s", code, out, errs)
	}
	if strings.Contains(out, "edited by you:") || !strings.Contains(out, "SKILL.md") || !strings.Contains(out, "\n+skill v2") {
		t.Errorf("output: %s", out)
	}
	// The rest is applied: the instruction is removed, the lock records v0.2.0.
	if !strings.Contains(out, "remove  ") {
		t.Errorf("no remove line: %s", out)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".kiro/steering/go.md")); err == nil {
		t.Error("steering file still there")
	}
	if b, _ := os.ReadFile(skill); string(b) != "mine\n" {
		t.Errorf("edited skill: %q", b)
	}
	if b, _ := os.ReadFile(paths.LockFile(env)); !strings.Contains(string(b), "v0.2.0") {
		t.Errorf("lock: %s", b)
	}
	want := "edited  ~/.kiro/skills/review/SKILL.md: you changed it; livery kept your version and did not install the team's\n" +
		"        to take the team's version: delete ~/.kiro/skills/review/SKILL.md, then run livery update\n" +
		"        to keep your change: copy the folder ~/.kiro/skills/review to ~/.config/livery/personal/skills/review, delete ~/.kiro/skills/review/SKILL.md, then run livery update\n"
	if !strings.Contains(out, want) {
		t.Errorf("no edited line: %s", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(errs), `1 edited by you, kept; the rest is applied`) {
		t.Errorf("error: %s", errs)
	}

	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	code, out, errs = runUpdate(t, env)
	if code != 0 {
		t.Fatalf("after delete: %d %s%s", code, out, errs)
	}
	if !strings.Contains(out, "v0.2.0") || !strings.Contains(out, "write   ") {
		t.Errorf("output: %s", out)
	}
	if b, _ := os.ReadFile(skill); string(b) != "skill v2\n" {
		t.Errorf("skill: %q", b)
	}
}

func TestUpdateUsage(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	code, _, errs := runUpdate(t, env)
	if code != 2 || !strings.Contains(errs, "livery setup") {
		t.Errorf("no config: %d %q", code, errs)
	}
	code, _, errs = runUpdate(t, env, "extra")
	if code != 2 || !strings.Contains(errs, "extra") {
		t.Errorf("stray: %d %q", code, errs)
	}
	code, _, errs = runUpdate(t, env, "--nope")
	if code != 2 || !strings.Contains(errs, "nope") {
		t.Errorf("flag: %d %q", code, errs)
	}
}

func TestUpdateUnknownTeam(t *testing.T) {
	t.Parallel()
	f := files("skill v1\n")
	f["teams/api/instructions/api.md"] = "api\n"
	src := enginetest.Source(t, f, "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src, "web")
	code, _, errs := runUpdate(t, env)
	if code != 2 || !strings.Contains(errs, "web") || !strings.Contains(errs, "api") {
		t.Errorf("%d %q", code, errs)
	}
}

func TestUpdateMovedTag(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v1.0.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 || strings.Contains(out, "warning: tag") {
		t.Fatalf("first run: exit %d: %s%s", code, out, errs)
	}
	if code, out, _ := runUpdate(t, env); code != 0 || strings.Contains(out, "warning: tag") {
		t.Fatalf("unmoved tag warned: %d %s", code, out)
	}

	enginetest.Commit(t, src, map[string]string{"base/instructions/go.md": "go rules 2\n"}, "")
	cmd := exec.Command("git", "tag", "-f", "v1.0.0")
	cmd.Dir = src
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git tag -f: %v\n%s", err, b)
	}

	re := regexp.MustCompile(`(?m)^warning: tag v1\.0\.0 moved from [0-9a-f]{12} to [0-9a-f]{12} since the last run;`)
	code, out, errs := runUpdate(t, env, "--plan")
	if code != 0 || !re.MatchString(out) {
		t.Fatalf("plan: exit %d: %s%s", code, out, errs)
	}
	if c, doc, derr := runDoctor(t, env); c != 0 || !strings.Contains(doc, "info     tag: v1.0.0 moved from ") {
		t.Fatalf("doctor: exit %d: %s%s", c, doc, derr)
	}
	code, out, errs = runUpdate(t, env)
	if code != 0 || !re.MatchString(out) {
		t.Fatalf("update: exit %d: %s%s", code, out, errs)
	}

	enginetest.Commit(t, src, map[string]string{"base/instructions/go.md": "go rules 3\n"}, "v1.1.0")
	code, out, errs = runUpdate(t, env)
	if code != 0 || strings.Contains(out, "warning: tag") {
		t.Fatalf("new tag: exit %d: %s%s", code, out, errs)
	}
}

// A file already in place with the release's content is adopted by the first setup or update,
// and the next update is up to date.
func TestPrintsAdoptLine(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"setup", "update"} {
		t.Run(cmd, func(t *testing.T) {
			src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
			env := enginetest.Env(t)
			if err := os.MkdirAll(filepath.Join(env.Home, ".kiro/skills/review"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(env.Home, ".kiro/skills/review/SKILL.md"), []byte("skill v1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			var code int
			var out, errs string
			if cmd == "setup" {
				code, out, errs = runSetup(t, env, "", false, "--source", src)
			} else {
				writeConfig(t, env, src)
				code, out, errs = runUpdate(t, env)
			}
			if code != 0 {
				t.Fatalf("exit %d: %s%s", code, out, errs)
			}
			if strings.Count(out, "adopt   ~/.kiro/skills/review/SKILL.md\n") != 1 || strings.Count(out, "adopt   ") != 1 {
				t.Errorf("adopt lines: %s", out)
			}
			if n := strings.Count(out, "write   "); n != 1 {
				t.Errorf("write lines = %d: %s", n, out)
			}
			code, out, errs = runUpdate(t, env)
			if code != 0 || !strings.Contains(out, "up to date") || strings.Contains(out, "adopt") {
				t.Errorf("next update: %d %q %s", code, out, errs)
			}
		})
	}
}
