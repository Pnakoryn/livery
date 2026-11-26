//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reshape sets the skill x of the source to SKILL.md and either the file helper or the folder
// helper with run.py.
func reshape(t *testing.T, repo string, folder bool) {
	t.Helper()
	dir := filepath.Join(repo, "base/skills/x")
	if err := os.RemoveAll(filepath.Join(dir, "helper")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: x\ndescription: A skill with a helper.\n---\n\nRun the helper.\n")
	if folder {
		writeFile(t, filepath.Join(dir, "helper/run.py"), "print('helper')\n")
	} else {
		writeFile(t, filepath.Join(dir, "helper"), "helper notes\n")
	}
}

// reshapeJourney installs v0.1.0 with skill x in the first shape, then releases v0.2.0 with the
// other shape and runs one update that must converge.
func reshapeJourney(t *testing.T, folderFirst bool) (*journey, string) {
	t.Helper()
	j := newJourney(t)
	commit(t, j.src, "", func() { reshape(t, j.src, folderFirst) })
	git(t, j.src, "tag", "-f", "v0.1.0")
	j.expect(0, "setup", "--source", j.src)
	commit(t, j.src, "v0.2.0", func() { reshape(t, j.src, !folderFirst) })
	out, _ := j.expect(0, "update")
	if strings.Contains(out, "kept, yours") {
		t.Errorf("update kept a file:\n%s", out)
	}
	return j, out
}

// A release that turns an installed file into a folder converges in one update.
func TestJourneyFileBecomesFolder(t *testing.T) {
	j, out := reshapeJourney(t, false)
	j.has(out, "remove  ~/.kiro/skills/x/helper\n", "write   ~/.kiro/skills/x/helper/run.py\n")
	if got := readFile(t, j.path(".kiro/skills/x/helper/run.py")); got != "print('helper')\n" {
		t.Errorf("helper/run.py: %q", got)
	}
	out, _ = j.expect(0, "update")
	j.has(out, "up to date")
}

// A release that turns an installed folder into a file converges in one update.
func TestJourneyFolderBecomesFile(t *testing.T) {
	j, out := reshapeJourney(t, true)
	j.has(out, "remove  ~/.kiro/skills/x/helper/run.py\n", "write   ~/.kiro/skills/x/helper\n")
	helper := j.path(".kiro/skills/x/helper")
	if fi, err := os.Lstat(helper); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("%s: %v, %v; want a regular file", helper, fi, err)
	}
	if got := readFile(t, helper); got != "helper notes\n" {
		t.Errorf("helper: %q", got)
	}
	out, _ = j.expect(0, "update")
	j.has(out, "up to date")
}
