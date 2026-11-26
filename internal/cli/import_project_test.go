package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

const projMCP = `{"mcpServers": {"docs": {"url": "https://docs.example.com/mcp"}}}`

func TestImportProjectHarnesses(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	writeFiles(t, proj, map[string]string{
		".kiro/steering/a.md":       "always on\n",
		".claude/skills/s/SKILL.md": "skill s\n",
		".mcp.json":                 projMCP,
	})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, rel := range []string{"base/instructions/a.md", "base/skills/s/SKILL.md", "base/mcp/docs.json"} {
		if _, err := os.Stat(filepath.Join(d, rel)); err != nil {
			t.Errorf("%s: %v\n%s", rel, err, out)
		}
	}
	if !strings.Contains(out, "livery setup --project --source <the address") || strings.Contains(out, "--source "+d) {
		t.Errorf("next steps:\n%s", out)
	}
}

func TestImportProjectRoundTrip(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	writeFiles(t, proj, map[string]string{
		".kiro/steering/a.md":       "---\ninclusion: always\n---\n\nalways on\n",
		".claude/skills/s/SKILL.md": "skill s\n",
		".mcp.json":                 "{\n  \"mcpServers\": {\n    \"docs\": {\n      \"type\": \"http\",\n      \"url\": \"https://docs.example.com/mcp\"\n    }\n  }\n}\n",
	})
	d := filepath.Join(t.TempDir(), "src")
	if code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d); code != 0 {
		t.Fatalf("import: %d %s%s", code, out, errs)
	}
	srcFiles := map[string]string{}
	for p, c := range fileData(tree(t, d)) {
		rel, _ := filepath.Rel(d, p)
		srcFiles[filepath.ToSlash(rel)] = c
	}
	src := enginetest.Source(t, srcFiles, "v0.1.0")
	before := fileData(tree(t, proj))
	env2, proj2 := projEnv(t)
	if code, out, errs := runSetup(t, env2, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "kiro", "--harness", "claude"); code != 0 {
		t.Fatalf("setup: %d %s%s", code, out, errs)
	}
	after := fileData(tree(t, proj2))
	for p, c := range before {
		rel, _ := filepath.Rel(proj, p)
		if got, ok := after[filepath.Join(proj2, rel)]; !ok || got != c {
			t.Errorf("%s: got %q (present %v), want %q", rel, got, ok, c)
		}
	}
}

func TestImportProjectLeavesOutInstalled(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	setupProject(t, env, src)
	writeFiles(t, proj, map[string]string{".claude/skills/mine/SKILL.md": "mine\n"})
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d)
	if code != 0 || !strings.Contains(out, "left out:") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	got := fileData(tree(t, d))
	if _, ok := got[filepath.Join(d, "base/skills/mine/SKILL.md")]; !ok {
		t.Fatalf("written: %v", got)
	}
	for p := range got {
		if strings.Contains(p, "base/skills/s/") || strings.Contains(p, "skills/review") {
			t.Errorf("installed item imported: %s", p)
		}
	}
}

// --file and --seed are gone: they are unknown flags.
func TestImportFileSeedFlagsGone(t *testing.T) {
	t.Parallel()
	env, _ := projEnv(t)
	for _, fl := range []string{"file", "seed"} {
		code, out, errs := runImport(t, env, "", false, "--project", "--"+fl, "x", "--into", filepath.Join(t.TempDir(), "src"))
		if code != 2 || !strings.Contains(errs, "-"+fl) {
			t.Errorf("--%s: exit %d: %s%s", fl, code, out, errs)
		}
	}
}

// import --project needs a harness as machine scope does: without --from and a terminal it
// exits 2 naming --from.
func TestImportProjectNeedsFrom(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	writeFiles(t, proj, map[string]string{".claude/rules/a.md": "rule\n"})
	code, out, errs := runImport(t, env, "", false, "--project", "--into", filepath.Join(t.TempDir(), "src"))
	if code != 2 || !strings.Contains(errs, "--from") {
		t.Errorf("exit %d: %s%s", code, out, errs)
	}
}
