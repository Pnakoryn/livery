//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newHome makes an empty throwaway home with .kiro/ and .claude/ and no source repository.
func newHome(t *testing.T) *journey {
	t.Helper()
	j := newJourney(t)
	j.src = ""
	return j
}

func writeExec(t *testing.T, p, content string) {
	t.Helper()
	writeFile(t, p, content)
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// tagRepo makes dir a git repository with one commit tagged v0.1.0.
func tagRepo(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "v0.1.0")
	git(t, dir, "tag", "v0.1.0")
}

// A hand-made Kiro setup is imported, checked, tagged and installed in a second home; the result
// equals the original.
func TestJourneyImportCheckSetup(t *testing.T) {
	a := newHome(t)
	b := newHome(t)
	repo := filepath.Join(t.TempDir(), "shared")

	skill := ".kiro/skills/review/"
	writeFile(t, a.path(skill+"SKILL.md"), "---\nname: review\ndescription: Review a change\n---\nRead the diff first.\n")
	writeExec(t, a.path(skill+"scripts/check.sh"), "#!/bin/sh\necho checked\n")
	writeFile(t, a.path(".kiro/steering/style.md"), "---\ninclusion: always\n---\nKeep functions short.\n")
	writeFile(t, a.path(".kiro/steering/go.md"), "---\ninclusion: fileMatch\nfileMatchPattern: \"**/*.go\"\n---\nUse gofmt.\n")
	writeFile(t, a.path(".kiro/hooks/lint.kiro.hook"), "{\"name\":\"lint\",\"when\":{\"type\":\"fileEdited\"}}\n")
	writeFile(t, a.path(".kiro/settings/mcp.json"),
		`{"mcpServers":{"docs":{"command":"docs-server","args":["--stdio"],"env":{"LOG_LEVEL":"debug"}}}}`+"\n")

	a.expect(0, "import", "--from", "kiro", "--into", repo, "--plan")
	if exists(filepath.Join(repo, "base")) {
		t.Fatal("import --plan wrote into the folder")
	}
	a.expect(0, "import", "--from", "kiro", "--into", repo)
	a.expect(0, "check", repo)
	tagRepo(t, repo)

	b.expect(0, "setup", "--source", repo, "--harness", "kiro", "--harness", "claude")

	// Skill: content and executable bit.
	for _, f := range []string{"SKILL.md", "scripts/check.sh"} {
		want := readFile(t, a.path(skill+f))
		if got := readFile(t, b.path(skill+f)); got != want {
			t.Errorf("%s: got %q, want %q", f, got, want)
		}
		if ma, mb := mode(t, a.path(skill+f))&0o111 != 0, mode(t, b.path(skill+f))&0o111 != 0; ma != mb {
			t.Errorf("%s: executable bit %v, want %v", f, mb, ma)
		}
	}
	if got, want := readFile(t, b.path(".kiro/hooks/lint.kiro.hook")), readFile(t, a.path(".kiro/hooks/lint.kiro.hook")); got != want {
		t.Errorf("hook: got %q, want %q", got, want)
	}
	// Steering: same mode, globs and body.
	style := readFile(t, b.path(".kiro/steering/style.md"))
	if strings.Contains(style, "fileMatch") || !strings.Contains(style, "Keep functions short.") {
		t.Errorf("style.md: %q", style)
	}
	goMd := readFile(t, b.path(".kiro/steering/go.md"))
	for _, w := range []string{"inclusion: fileMatch", "**/*.go", "Use gofmt."} {
		if !strings.Contains(goMd, w) {
			t.Errorf("go.md lacks %q:\n%s", w, goMd)
		}
	}
	// MCP: the value became ${NAME}.
	var cfg struct {
		Servers map[string]struct {
			Command string            `json:"command"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, b.path(".kiro/settings/mcp.json"))), &cfg); err != nil {
		t.Fatal(err)
	}
	docs, ok := cfg.Servers["docs"]
	if !ok || docs.Command != "docs-server" || len(docs.Env) != 1 {
		t.Fatalf("mcp.json: %+v", cfg)
	}
	for k, v := range docs.Env {
		if v != "${"+k+"}" || strings.Contains(v, "debug") {
			t.Errorf("env %s = %q, want ${%s}", k, v, k)
		}
	}
	// Claude Code: the skill and both rules.
	for _, f := range []string{".claude/skills/review/SKILL.md", ".claude/skills/review/scripts/check.sh", ".claude/rules/style.md", ".claude/rules/go.md"} {
		if !exists(b.path(f)) {
			t.Errorf("%s is missing", f)
		}
	}
	for _, f := range []string{"SKILL.md", "scripts/check.sh"} {
		want := readFile(t, a.path(skill+f))
		if got := readFile(t, b.path(".claude/skills/review/"+f)); got != want {
			t.Errorf("claude %s: got %q, want %q", f, got, want)
		}
	}
	claudeGo := readFile(t, b.path(".claude/rules/go.md"))
	for _, w := range []string{"paths:", "**/*.go", "Use gofmt."} {
		if !strings.Contains(claudeGo, w) {
			t.Errorf("claude go.md lacks %q:\n%s", w, claudeGo)
		}
	}
	b.expect(0, "doctor")
}

// A Claude Code rule with paths becomes a Kiro steering file with globs, through a team layer.
func TestJourneyClaudeOriginTeam(t *testing.T) {
	a := newHome(t)
	b := newHome(t)
	repo := filepath.Join(t.TempDir(), "shared")

	writeFile(t, a.path(".claude/skills/triage/SKILL.md"), "---\nname: triage\ndescription: Triage a bug\n---\nAsk for steps.\n")
	writeFile(t, a.path(".claude/rules/ts.md"), "---\npaths:\n  - \"src/**/*.ts\"\n---\nPrefer const.\n")

	a.expect(0, "import", "--from", "claude", "--into", repo, "--team", "web")
	a.expect(0, "check", repo)
	tagRepo(t, repo)

	b.expect(0, "setup", "--source", repo, "--harness", "kiro", "--team", "web")
	if !exists(b.path(".kiro/skills/triage/SKILL.md")) {
		t.Error("skill is not installed")
	}
	ts := readFile(t, b.path(".kiro/steering/ts.md"))
	for _, w := range []string{"inclusion: fileMatch", "src/**/*.ts", "Prefer const."} {
		if !strings.Contains(ts, w) {
			t.Errorf("ts.md lacks %q:\n%s", w, ts)
		}
	}
}

// A malformed instruction whose frontmatter holds a token never prints the token, in check or in
// import (where the file is skipped).
func TestJourneyMalformedInstructionKeepsTokenOut(t *testing.T) {
	const token = "ghp_0123456789abcdefghijklmnopqrstuvwxyz"
	j := newHome(t)

	repo := filepath.Join(t.TempDir(), "shared")
	writeFile(t, filepath.Join(repo, "livery.json"), "{\"schema\": 1}\n")
	writeFile(t, filepath.Join(repo, "base/instructions/bad.md"), "---\n"+token+"\n---\nbody\n")
	tagRepo(t, repo)
	out, errOut, code := j.livery("check", repo)
	if code == 0 {
		t.Fatalf("check passed on a malformed instruction:\n%s%s", out, errOut)
	}
	if strings.Contains(out+errOut, token) {
		t.Errorf("check printed the token:\n%s%s", out, errOut)
	}
	if !strings.Contains(out+errOut, "bad.md:2") {
		t.Errorf("check output lacks path and line:\n%s%s", out, errOut)
	}

	writeFile(t, j.path(".claude/rules/bad.md"), "---\n"+token+"\n---\nbody\n")
	into := filepath.Join(t.TempDir(), "imported")
	out, errOut, _ = j.livery("import", "--from", "claude", "--into", into)
	if strings.Contains(out+errOut, token) {
		t.Errorf("import printed the token:\n%s%s", out, errOut)
	}
	if !strings.Contains(out+errOut, "bad.md") {
		t.Errorf("import output does not name the skipped file:\n%s%s", out, errOut)
	}
}

// Standard output on /dev/null (a character device, not a terminal) does not make setup prompt.
func TestSetupStdoutDevNull(t *testing.T) {
	j := newHome(t)
	j.src = newSource(t)
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	cmd := exec.Command(binary, "setup", "--source", j.src, "--harness", "claude")
	cmd.Dir = j.home
	cmd.Env = []string{
		"HOME=" + j.home,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	}
	var errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = null, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup with stdout on /dev/null: %v\nstderr:\n%s", err, errb.String())
	}
}
