//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJourneyProject is the project journey: setup, doctor, update, a hand-edited managed file
// and uninstall, all with --project in a throwaway project folder.
func TestJourneyProject(t *testing.T) {
	j := newJourney(t)
	proj := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	pp := func(p string) string { return filepath.Join(proj, filepath.FromSlash(p)) }
	j.cwd = proj
	run := j.expect
	homeBefore := walk(t, j.home, ".cache/livery")

	// 1. setup
	out, _ := run(0, "setup", "--project", "--source", j.src, "--team", "web",
		"--harness", "kiro", "--harness", "claude", "--harness", "opencode")
	j.has(out, "write   .claude/rules/go.md", "write   .kiro/steering/css.md")
	for _, p := range []string{
		".kiro/skills/review/SKILL.md", ".kiro/steering/go.md", ".kiro/settings/mcp.json",
		".claude/skills/review/SKILL.md", ".claude/rules/go.md", ".mcp.json",
		".opencode/skills/review/SKILL.md", ".opencode/livery/instructions/go.md", "opencode.json",
		".livery/config.json", ".livery/lock.json",
	} {
		if !exists(pp(p)) {
			t.Errorf("%s missing", p)
		}
	}
	var lock struct {
		Files  map[string]string `json:"files"`
		Merged []struct {
			Path string `json:"path"`
		} `json:"merged"`
	}
	if err := json.Unmarshal([]byte(readFile(t, pp(".livery/lock.json"))), &lock); err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for p := range lock.Files {
		paths = append(paths, p)
	}
	for _, m := range lock.Merged {
		paths = append(paths, m.Path)
	}
	if len(lock.Files) == 0 || len(lock.Merged) == 0 {
		t.Errorf("lock has %d files, %d merged", len(lock.Files), len(lock.Merged))
	}
	for _, p := range paths {
		if filepath.IsAbs(p) || strings.HasPrefix(p, "..") || strings.Contains(p, "\\") {
			t.Errorf("lock path %q is not relative", p)
		}
	}
	if _, ok := lock.Files[".claude/rules/go.md"]; !ok {
		t.Errorf("lock lacks .claude/rules/go.md: %v", paths)
	}
	// Nothing new below the home folder except the cache.
	sameWalk(t, "project setup and home", homeBefore, walk(t, j.home, ".cache/livery"))

	// 2. doctor
	run(0, "doctor", "--project")

	// 3. a second release
	commit(t, j.src, "v0.2.0", func() {
		writeFile(t, filepath.Join(j.src, "base/instructions/commits.md"),
			readFile(t, filepath.Join(j.src, "base/instructions/commits.md"))+"\nPrefer one logical change per commit.\n")
	})
	out, _ = run(0, "update", "--project")
	j.has(out, "v0.2.0", "write   .kiro/steering/commits.md", "write   .claude/rules/commits.md")
	if !strings.Contains(readFile(t, pp(".claude/rules/commits.md")), "logical change") {
		t.Error("the instruction was not updated")
	}

	// 4. the managed file edited by hand
	good := readFile(t, pp(".claude/rules/commits.md"))
	writeFile(t, pp(".claude/rules/commits.md"), "mine\n")
	commit(t, j.src, "v0.3.0", func() {
		writeFile(t, filepath.Join(j.src, "base/instructions/commits.md"),
			readFile(t, filepath.Join(j.src, "base/instructions/commits.md"))+"\nKeep the body short.\n")
	})
	out, errs := run(1, "update", "--project")
	j.has(out+errs, ".claude/rules/commits.md", "mine")
	if readFile(t, pp(".claude/rules/commits.md")) != "mine\n" {
		t.Error("a blocked run changed the hand-edited file")
	}
	writeFile(t, pp(".claude/rules/commits.md"), good)
	run(0, "update", "--project")
	if s := readFile(t, pp(".claude/rules/commits.md")); !strings.Contains(s, "body short") {
		t.Errorf("after restoring, update did not apply v0.3.0: %s", s)
	}

	// 5. uninstall
	writeFile(t, pp(".kiro/steering/mine.md"), "mine\n")
	configBefore := readFile(t, pp(".livery/config.json"))
	run(0, "uninstall", "--project")
	for _, p := range []string{
		".kiro/skills/review/SKILL.md", ".kiro/steering/go.md", ".claude/skills/review/SKILL.md",
		".claude/rules/go.md", ".opencode/skills/review/SKILL.md", ".opencode/livery/instructions/go.md",
	} {
		if exists(pp(p)) {
			t.Errorf("%s still there after uninstall", p)
		}
	}
	for _, p := range []string{".kiro/steering/mine.md", ".livery/config.json"} {
		if !exists(pp(p)) {
			t.Errorf("%s was removed by uninstall", p)
		}
	}
	if readFile(t, pp(".livery/config.json")) != configBefore {
		t.Error(".livery/config.json changed")
	}
	for _, p := range []string{".mcp.json", "opencode.json"} {
		if !exists(pp(p)) {
			continue
		}
		if s := readFile(t, pp(p)); strings.Contains(s, "docs.example.com") || strings.Contains(s, "github-mcp") || strings.Contains(s, "livery") {
			t.Errorf("%s still holds livery entries:\n%s", p, s)
		}
	}
	sameWalk(t, "project journey and home", homeBefore, walk(t, j.home, ".cache/livery"))
}

// TestJourneyProjectClone: a committed project cloned by a second person, with another home and
// cache, is healthy and an update changes nothing.
func TestJourneyProjectClone(t *testing.T) {
	j := newJourney(t)
	proj := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, proj, "init", "-q", "-b", "main")
	j.cwd = proj
	j.expect(0, "setup", "--project", "--source", j.src, "--harness", "claude")
	git(t, proj, "add", "-A")
	git(t, proj, "commit", "-q", "-m", "setup")

	clone := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(clone), "clone", "-q", proj, clone)

	j2 := newJourney(t)
	j2.src = j.src
	j2.cwd = clone
	j2.expect(0, "doctor", "--project")
	out, _ := j2.expect(0, "update", "--project")
	j2.has(out, "up to date")

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = clone
	cmd.Env = gitEnv()
	st, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, st)
	}
	if len(st) != 0 {
		t.Errorf("the clone changed after update:\n%s", st)
	}
}
