package cli

import (
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

func TestUninstallPlanPrintsNoDiff(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	code, out, errs := runUninstall(t, env, "--plan")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "remove  ") || !strings.Contains(out, "unset   ") {
		t.Errorf("no remove or unset line:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "@@") {
			t.Errorf("diff line %q in\n%s", l, out)
		}
	}
}

func TestUpdatePlanDiffsOnlyChangedFile(t *testing.T) {
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
	code, out, errs := runUpdate(t, env, "--plan")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if n := strings.Count("\n"+out, "\n--- now/"); n != 1 || !strings.Contains(out, "--- now/~/.kiro/skills/review/SKILL.md") {
		t.Errorf("want one diff, of SKILL.md (%d):\n%s", n, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "diff --git") || strings.HasPrefix(l, "index ") {
			t.Errorf("git header %q in\n%s", l, out)
		}
	}
}

func TestPlanQuotesEntryNameWithDot(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, map[string]string{
		"livery.json":              `{"schema":1}`,
		"base/mcp/Jira.Cloud.json": `{"command":"jira-mcp"}`,
		"base/mcp/docs.json":       `{"command":"docs-mcp"}`,
	}, "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	code, out, errs := runUpdate(t, env, "--plan")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if lineWith(out, "set     ", `mcpServers["Jira.Cloud"]`) == "" {
		t.Errorf("no line with mcpServers[\"Jira.Cloud\"]:\n%s", out)
	}
	if lineWith(out, "set     ", "mcpServers.docs") == "" {
		t.Errorf("no line with mcpServers.docs:\n%s", out)
	}
}

func TestListInstalledLongNameAligns(t *testing.T) {
	t.Parallel()
	const long = "a-skill-with-a-thirty-char-nam"
	if len(long) != 30 {
		t.Fatalf("name has %d characters", len(long))
	}
	src := enginetest.Source(t, map[string]string{
		"livery.json":                       `{"schema":1}`,
		"base/skills/review/SKILL.md":       "skill v1\n",
		"base/skills/" + long + "/SKILL.md": "long\n",
		"teams/web/instructions/commits.md": "commit rules\n",
	}, "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src, "web")
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	code, out, errs := runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	offsets := map[int]bool{}
	for _, name := range []string{"review", long, "commits"} {
		l := lineWith(out, name)
		if l == "" {
			t.Fatalf("no line with %s:\n%s", name, out)
		}
		i := strings.Index(l, name) + len(name)
		offsets[i+len(l[i:])-len(strings.TrimLeft(l[i:], " "))] = true
	}
	if len(offsets) != 1 {
		t.Errorf("layer column at offsets %v:\n%s", offsets, out)
	}
}
