package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

// outside returns a new folder below t.TempDir(), outside the project, holding one file.
func outside(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// tree maps every file, folder and link below the roots to "<mode> <content>", "dir" or
// "-> target"; a link is not followed. treeExcept leaves out the folder skip.
func tree(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	return treeExcept(t, "", roots...)
}

func treeExcept(t *testing.T, skip string, roots ...string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case skip != "" && p == skip:
				return fs.SkipDir
			case d.Type()&fs.ModeSymlink != 0:
				target, err := os.Readlink(p)
				m[p] = "-> " + target
				return err
			case d.IsDir():
				m[p] = "dir"
				return nil
			}
			b, err := os.ReadFile(p)
			info, ierr := d.Info()
			if err == nil {
				err = ierr
			}
			if err == nil {
				m[p] = fmt.Sprintf("%04o %s", info.Mode().Perm(), b)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return m
}

// fileData maps the files in a tree to their content; folders and links are left out.
func fileData(m map[string]string) map[string]string {
	out := map[string]string{}
	for p, v := range m {
		if v != "dir" && !strings.HasPrefix(v, "-> ") {
			out[p] = v[strings.IndexByte(v, ' ')+1:]
		}
	}
	return out
}

// unchanged fails when dir no longer holds what before (from tree) says.
func unchanged(t *testing.T, what string, dir string, before map[string]string) {
	t.Helper()
	if after := tree(t, dir); !equal(before, after) {
		t.Errorf("%s: %s changed:\nbefore %v\nafter  %v", what, dir, before, after)
	}
}

func TestSetupProjectLinkedHarnessFolderIsKept(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	other := outside(t)
	symlink(t, other, filepath.Join(proj, ".claude"))
	before := tree(t, other)

	code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "claude")
	if code != 0 {
		t.Fatalf("setup --project: exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "kept, yours: .claude/rules/go.md") {
		t.Errorf("output does not keep the linked file:\n%s", out)
	}
	unchanged(t, "setup --project", other, before)
	if fi, err := os.Lstat(filepath.Join(proj, ".claude")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".claude is no longer a link: %v", err)
	}
	if code, out, errs := runUpdate(t, env, "--project"); code != 0 {
		t.Errorf("update --project: exit %d: %s%s", code, out, errs)
	}
	unchanged(t, "update --project", other, before)
	if code, out, errs := runUninstall(t, env, "--project"); code != 0 {
		t.Errorf("uninstall --project: exit %d: %s%s", code, out, errs)
	}
	unchanged(t, "uninstall --project", other, before)
}

// An installed harness folder replaced by a link to a copy: update and uninstall change nothing
// below the link, though the copy holds the files livery installed.
func TestUpdateUninstallProjectLinkedHarnessFolder(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	setupProject(t, env, src)
	other := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Rename(filepath.Join(proj, ".claude"), other); err != nil {
		t.Fatal(err)
	}
	symlink(t, other, filepath.Join(proj, ".claude"))
	before := tree(t, other)
	if len(before) == 0 {
		t.Fatal("setup --project installed nothing in .claude")
	}

	enginetest.Commit(t, src, map[string]string{"base/instructions/go.md": "go rules v2\n"}, "v0.2.0")
	isLink := func(what string) {
		t.Helper()
		if fi, err := os.Lstat(filepath.Join(proj, ".claude")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s: .claude is no longer a link: %v", what, err)
		}
	}
	steering := filepath.Join(proj, ".kiro", "steering", "go.md")
	code, out, errs := runUpdate(t, env, "--project")
	if code != 0 {
		t.Errorf("update --project: exit %d, want 0: %s%s", code, out, errs)
	}
	unchanged(t, "update --project", other, before)
	isLink("update --project")
	if b, err := os.ReadFile(steering); err != nil || !strings.Contains(string(b), "go rules v2\n") {
		t.Errorf("update --project: regular .kiro folder not updated: %q, %v", b, err)
	}

	code, out, errs = runUninstall(t, env, "--project")
	if code != 0 {
		t.Errorf("uninstall --project: exit %d, want 0: %s%s", code, out, errs)
	}
	unchanged(t, "uninstall --project", other, before)
	isLink("uninstall --project")
	if _, err := os.Lstat(steering); !os.IsNotExist(err) {
		t.Errorf("uninstall --project: %s is still there: %v", steering, err)
	}
	for p, c := range tree(t, filepath.Join(proj, ".kiro")) {
		if c != "dir" {
			t.Errorf("uninstall --project: regular .kiro folder not emptied: %s (%s)", p, c)
		}
	}
}

// A shared JSON file reached through a linked harness folder stops the run.
func TestSetupProjectLinkedSharedFileFolderBlocks(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	other := outside(t)
	symlink(t, other, filepath.Join(proj, ".kiro"))
	before := tree(t, other)

	code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "kiro")
	if code != 1 {
		t.Fatalf("setup --project: exit %d, want 1: %s%s", code, out, errs)
	}
	if !strings.Contains(errs, filepath.Join(proj, ".kiro")+": is a symbolic link") {
		t.Errorf("stderr does not name the link:\n%s", errs)
	}
	unchanged(t, "setup --project", other, before)
}

// linkedLivery makes the working directory's .livery (or a file in it, rel) a link to another
// project's, and returns that project and a check that it and the working directory are as
// before.
func linkedLivery(t *testing.T, env adapter.Env, src, rel string) (link string, check func(string)) {
	t.Helper()
	target := t.TempDir()
	tenv := env
	tenv.Dir = target
	setupProject(t, tenv, src)
	link = filepath.Join(env.Dir, rel)
	if rel != ".livery" {
		if err := os.MkdirAll(filepath.Join(env.Dir, ".livery"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	symlink(t, filepath.Join(target, rel), link)
	beforeTarget := tree(t, target)
	beforeDir := tree(t, env.Dir)
	return link, func(what string) {
		t.Helper()
		unchanged(t, what, target, beforeTarget)
		unchanged(t, what, env.Dir, beforeDir)
		entries, err := os.ReadDir(env.Dir)
		if err != nil || len(entries) != 1 || entries[0].Name() != ".livery" {
			t.Errorf("%s: working directory holds %v (%v), want only .livery", what, entries, err)
		}
	}
}

func TestProjectLinkedLiveryRefused(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	for _, rel := range []string{".livery", ".livery/config.json", ".livery/lock.json"} {
		t.Run(strings.ReplaceAll(rel, "/", "_"), func(t *testing.T) {
			env, _ := projEnv(t)
			link, check := linkedLivery(t, env, src, rel)
			runs := map[string]func() (int, string, string){
				"setup --project": func() (int, string, string) {
					return runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "claude")
				},
				"update --project":    func() (int, string, string) { return runUpdate(t, env, "--project") },
				"doctor --project":    func() (int, string, string) { return runDoctor(t, env, "--project") },
				"uninstall --project": func() (int, string, string) { return runUninstall(t, env, "--project") },
			}
			for what, run := range runs {
				code, out, errs := run()
				if code != 1 {
					t.Errorf("%s: exit %d, want 1: %s%s", what, code, out, errs)
				}
				if !strings.Contains(errs, link+": is a symbolic link") || !strings.Contains(errs, "replace the link with a real folder or file") {
					t.Errorf("%s: stderr does not name %s:\n%s", what, link, errs)
				}
				check(what)
			}
		})
	}
}
