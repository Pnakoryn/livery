//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tagV2(t *testing.T, j *journey) {
	commit(t, j.src, "v0.2.0", func() {
		writeFile(t, filepath.Join(j.src, "base/instructions/commits.md"), "Version two of the commit rules.\n")
	})
}

// TestTryRelease: the header shows old -> new, the go-back line is printed after an applied change
// and the printed command works.
func TestTryRelease(t *testing.T) {
	j := newJourney(t)
	out, _ := j.expect(0, "setup", "--source", j.src)
	if strings.Contains(out, "->") || strings.Contains(out, "to go back") {
		t.Errorf("first install shows an arrow or go-back line:\n%s", out)
	}
	tagV2(t, j)

	out, _ = j.expect(0, "update", "--plan")
	j.has(out, "v0.1.0 -> v0.2.0")
	if strings.Contains(out, "to go back") {
		t.Errorf("--plan prints the go-back line:\n%s", out)
	}

	out, _ = j.expect(0, "update")
	j.has(out, "v0.1.0 -> v0.2.0", "to go back: livery setup --channel v0.1.0")
	out, _ = j.expect(0, "update")
	if strings.Contains(out, "->") && strings.Contains(strings.SplitN(out, "\n", 2)[0], "->") || strings.Contains(out, "to go back") {
		t.Errorf("second update shows an arrow or go-back line:\n%s", out)
	}

	out, _ = j.expect(0, "setup", "--channel", "v0.1.0")
	j.has(out, "v0.2.0 -> v0.1.0", "fixed on v0.1.0: livery update will not install newer releases", "--channel stable")
	if strings.Contains(out, "to go back") {
		t.Errorf("a tag channel prints the go-back line:\n%s", out)
	}
	if strings.Contains(readFile(t, j.path(".kiro/steering/commits.md")), "Version two") {
		t.Errorf("going back did not install v0.1.0 content")
	}
}

func TestTryReleaseProject(t *testing.T) {
	j := newJourney(t)
	proj := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	j.cwd = proj
	j.expect(0, "setup", "--project", "--source", j.src, "--harness", "kiro")
	tagV2(t, j)
	out, _ := j.expect(0, "update", "--project")
	j.has(out, "livery setup --project --channel v0.1.0")
}

func TestUnknownTagLists(t *testing.T) {
	j := newJourney(t)
	tagV2(t, j)
	_, stderr := j.expect(2, "setup", "--source", j.src, "--channel", "v9.9.9")
	j.has(stderr, "the releases, newest first: v0.2.0, v0.1.0")

	bare := filepath.Join(t.TempDir(), "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	copyTree(t, "testdata/source", bare)
	git(t, bare, "init", "-q", "-b", "main")
	git(t, bare, "add", "-A")
	git(t, bare, "commit", "-q", "-m", "x")
	_, stderr = j.expect(2, "setup", "--source", bare, "--channel", "v9.9.9")
	j.has(stderr, "it has no release yet")
	_, stderr = j.expect(1, "setup", "--source", bare, "--channel", "stable")
	j.has(stderr, "has no release yet")
}

func TestDoctorPinnedInfo(t *testing.T) {
	j := newJourney(t)
	j.expect(0, "setup", "--source", j.src, "--channel", "v0.1.0")
	tagV2(t, j)
	out, _ := j.expect(0, "doctor")
	j.has(out, "info     release: v0.2.0 is the newest release")
	j.expect(0, "setup", "--channel", "stable")
	out, _ = j.expect(0, "doctor")
	if strings.Contains(out, "is the newest release") {
		t.Errorf("stable prints the info line:\n%s", out)
	}
}
