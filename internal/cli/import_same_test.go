package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sameProject(claudeBody string) map[string]string {
	return map[string]string{
		".kiro/steering/testing.md":          "---\ninclusion: always\n---\n\nRun the tests.\n",
		".claude/rules/testing.md":           claudeBody,
		".kiro/skills/review/SKILL.md":       "review it\n",
		".claude/skills/review/SKILL.md":     "review it\n",
		".kiro/settings/mcp.json":            `{"mcpServers": {"docs": {"url": "https://docs.example.com/mcp", "type": "http"}}}`,
		".mcp.json":                          "{\n  \"mcpServers\": {\n    \"docs\": {\"type\": \"http\",\n \"url\": \"https://docs.example.com/mcp\"}\n  }\n}\n",
		".claude/skills/only/SKILL.md":       "only claude\n",
		".kiro/steering/kiro-only.md":        "kiro only\n",
		".claude/rules/claude-only.md":       "claude only\n",
		".kiro/skills/review/scripts/a.sh":   "#!/bin/sh\n",
		".claude/skills/review/scripts/a.sh": "#!/bin/sh\n",
	}
}

func TestImportProjectSameItemsOnce(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	files := sameProject("Run the tests.\n\n")
	writeFiles(t, proj, files)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, want := range []string{
		"same    instruction/testing: identical in kiro, claude; imported once\n",
		"same    skill/review: identical in kiro, claude; imported once\n",
		"same    mcp/docs: identical in kiro, claude; imported once\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "same    ") != 3 {
		t.Errorf("same lines:\n%s", out)
	}
	for _, rel := range []string{"base/instructions/testing.md", "base/skills/review/SKILL.md", "base/skills/review/scripts/a.sh", "base/mcp/docs.json",
		"base/skills/only/SKILL.md", "base/instructions/kiro-only.md", "base/instructions/claude-only.md"} {
		if _, err := os.Stat(filepath.Join(d, rel)); err != nil {
			t.Errorf("%s: %v\n%s", rel, err, out)
		}
		if n := strings.Count(out, filepath.Join(d, rel)+"\n"); n != 1 {
			t.Errorf("%s named %d times:\n%s", rel, n, out)
		}
	}
}

func TestImportProjectSameNameDiffers(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	files := sameProject("Run all the tests.\n")
	writeFiles(t, proj, files)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d)
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s%s", code, out, errs)
	}
	for _, want := range []string{
		`instruction "testing" differs between`,
		filepath.Join(proj, ".kiro/steering/testing.md"),
		filepath.Join(proj, ".claude/rules/testing.md"),
		"one --from per run",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr misses %q: %s", want, errs)
		}
	}
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Errorf("%s was created: %v", d, err)
	}
}

func TestImportProjectSkillDiffersNamesFile(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	files := sameProject("Run the tests.\n")
	files[".claude/skills/review/scripts/a.sh"] = "#!/bin/sh\necho\n"
	writeFiles(t, proj, files)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d, "--plan")
	if code != 2 || !strings.Contains(errs, filepath.Join(proj, ".kiro/skills/review/scripts/a.sh")+" and "+filepath.Join(proj, ".claude/skills/review/scripts/a.sh")) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}

func TestImportProjectSkillDiffersNamesFirstSortedFile(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	files := sameProject("Run the tests.\n")
	files[".kiro/skills/review/scripts/0.sh"] = "#!/bin/sh\n"
	files[".claude/skills/review/scripts/a.sh"] = "#!/bin/sh\necho\n"
	files[".claude/skills/review/scripts/z.sh"] = "#!/bin/sh\n"
	writeFiles(t, proj, files)
	for i := 0; i < 5; i++ {
		d := filepath.Join(t.TempDir(), "src")
		code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d, "--plan")
		if code != 2 || !strings.Contains(errs, filepath.Join(proj, ".kiro/skills/review/scripts/0.sh")+" and "+filepath.Join(proj, ".claude/skills/review/scripts/0.sh")) {
			t.Fatalf("exit %d: %s%s", code, out, errs)
		}
	}
}

func TestImportProjectMCPDiffers(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	files := sameProject("Run the tests.\n")
	files[".mcp.json"] = `{"mcpServers": {"docs": {"type": "http", "url": "https://other.example.com/mcp"}}}`
	writeFiles(t, proj, files)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d)
	if code != 2 || !strings.Contains(errs, `mcp "docs" differs between `+filepath.Join(proj, ".kiro/settings/mcp.json")+" and "+filepath.Join(proj, ".mcp.json")) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}

func TestImportProjectOneHarnessNoSameLine(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	files := sameProject("Run the tests.\n")
	writeFiles(t, proj, files)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--into", d)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "same    ") && strings.Contains(l, "identical in") {
			t.Errorf("same line with one --from: %s", l)
		}
	}
}

func TestImportProjectSameOnlyNamesOnce(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	files := sameProject("Run the tests.\n")
	writeFiles(t, proj, files)
	d := filepath.Join(t.TempDir(), "src")
	code, out, errs := runImport(t, env, "", false, "--project", "--from", "kiro", "--from", "claude", "--into", d, "--only", "nope/x")
	if code != 2 || strings.Count(errs, "instruction/testing") != 1 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}
