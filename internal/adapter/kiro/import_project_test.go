package kiro

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
)

func TestImportProjectLinkedSkill(t *testing.T) {
	proj := t.TempDir()
	e := adapter.Env{Home: t.TempDir(), Project: proj}
	target := t.TempDir()
	put(t, filepath.Join(target, "SKILL.md"), "s", 0o644)
	if err := os.MkdirAll(filepath.Join(proj, ".kiro", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(proj, ".kiro", "skills", "linked")); err != nil {
		t.Fatal(err)
	}
	found, skipped := doImport(t, e)
	if len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != "symbolic link" {
		t.Errorf("found %v skipped %v", found, skipped)
	}
}

func TestImportProjectLinkedHarnessFolder(t *testing.T) {
	proj := t.TempDir()
	e := adapter.Env{Home: t.TempDir(), Project: proj}
	outside := t.TempDir()
	put(t, filepath.Join(outside, "skills", "s", "SKILL.md"), "s", 0o644)
	if err := os.Symlink(outside, filepath.Join(proj, ".kiro")); err != nil {
		t.Fatal(err)
	}
	found, skipped := doImport(t, e)
	if len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != "symbolic link" {
		t.Errorf("found %v skipped %v", found, skipped)
	}
}
