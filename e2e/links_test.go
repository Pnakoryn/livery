//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A folder below the harness folder that is a link is the user's: livery writes nothing through it.
func TestJourneyLinkedFolder(t *testing.T) {
	j := newJourney(t)
	target := filepath.Join(filepath.Dir(j.home), "dotfiles-steering")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := j.path(".kiro/steering")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	out, _ := j.expect(0, "setup", "--source", j.src)
	j.has(out,
		"kept, yours: ~/.kiro/steering/go.md",
		"kept, yours: ~/.kiro/steering/commits.md",
		"write   ~/.kiro/skills/review/SKILL.md",
	)
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s: no longer a link (%v)", link, err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("livery wrote into the link target: %v", entries)
	}
}

// A link or folder outside livery.json, base/ and teams/ is the team's business: setup ignores it
// and check reports no link finding for it.
func TestJourneyIgnoresFilesOutsideLayout(t *testing.T) {
	j := newJourney(t)
	writeFile(t, filepath.Join(j.src, "AGENTS.md"), "agents")
	if err := os.Symlink("AGENTS.md", filepath.Join(j.src, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(j.src, "docs", "x.md"), "docs")
	git(t, j.src, "add", "-A")
	git(t, j.src, "commit", "-q", "-m", "extras")
	git(t, j.src, "tag", "v0.2.0")
	j.expect(0, "setup", "--source", j.src)
	out, errOut, code := j.livery("check", j.src)
	if strings.Contains(out+errOut, "CLAUDE.md") {
		t.Errorf("check mentions the root link (exit %d):\n%s%s", code, out, errOut)
	}
}
