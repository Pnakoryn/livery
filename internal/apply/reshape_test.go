package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/lock"
)

// reshapePlan is a skill with SKILL.md and, as a folder, helper/run.py, or else the file helper.
func reshapePlan(home string, folder bool) []adapter.Plan {
	root := filepath.Join(home, ".h")
	dir := filepath.Join(root, "skills", "x")
	p := adapter.Plan{Root: root, Files: []adapter.File{
		{Path: filepath.Join(dir, "SKILL.md"), Data: []byte("x\n"), Origin: "team: x"},
	}}
	if folder {
		p.Files = append(p.Files,
			adapter.File{Path: filepath.Join(dir, "helper", "run.py"), Data: []byte("print(1)\n"), Origin: "team: x"},
			adapter.File{Path: filepath.Join(dir, "helper", "lib", "util.py"), Data: []byte("u\n"), Origin: "team: x"})
	} else {
		p.Files = append(p.Files, adapter.File{Path: filepath.Join(dir, "helper"), Data: []byte("helper\n"), Origin: "team: x"})
	}
	return []adapter.Plan{p}
}

func opOf(t *testing.T, r Result, path string) Op {
	t.Helper()
	for _, a := range r.Actions {
		if a.Path == path && a.Key == "" {
			return a.Op
		}
	}
	t.Fatalf("%s: no action in %+v", path, r.Actions)
	return ""
}

// converge runs from the plans of from to the ones of to and checks that the second run has
// nothing to do.
func converge(t *testing.T, home string, from, to bool) Result {
	t.Helper()
	first := run(t, reshapePlan(home, from), lock.Lock{}, home)
	second := run(t, reshapePlan(home, to), first.Lock, home)
	third, err := compute(t, reshapePlan(home, to), second.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	if got := report(third, home); got != "up to date\n" {
		t.Errorf("next run:\n%s", got)
	}
	return second
}

func TestFileBecomesFolder(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".h", "skills", "x")
	res := converge(t, home, false, true)
	if op := opOf(t, res, filepath.Join(dir, "helper")); op != Remove {
		t.Errorf("helper: %s, want remove", op)
	}
	for _, p := range []string{filepath.Join(dir, "helper", "run.py"), filepath.Join(dir, "helper", "lib", "util.py")} {
		if op := opOf(t, res, p); op != Write {
			t.Errorf("%s: %s, want write", p, op)
		}
		if _, err := os.ReadFile(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestFolderBecomesFile(t *testing.T) {
	home := t.TempDir()
	helper := filepath.Join(home, ".h", "skills", "x", "helper")
	res := converge(t, home, true, false)
	if op := opOf(t, res, helper); op != Write {
		t.Errorf("helper: %s, want write", op)
	}
	if fi, err := os.Lstat(helper); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("helper: %v, %v; want a regular file", fi, err)
	}
	if b, _ := os.ReadFile(helper); string(b) != "helper\n" {
		t.Errorf("helper: %q", b)
	}
}

// An edited old file stays the user's: nothing is written below it.
func TestFileBecomesFolderEdited(t *testing.T) {
	home := t.TempDir()
	helper := filepath.Join(home, ".h", "skills", "x", "helper")
	first := run(t, reshapePlan(home, false), lock.Lock{}, home)
	put(t, helper, "mine\n")
	res, err := compute(t, reshapePlan(home, true), first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	if op := opOf(t, res, filepath.Join(helper, "run.py")); op != KeptYours {
		t.Errorf("run.py: %s, want %s", op, KeptYours)
	}
	if op := opOf(t, res, helper); op != LeftEdited {
		t.Errorf("helper: %s, want %s", op, LeftEdited)
	}
	if _, err := Execute(res); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(helper); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("helper: %v, %v", fi, err)
	}
	if b, _ := os.ReadFile(helper); string(b) != "mine\n" {
		t.Errorf("helper: %q, want the user's", b)
	}
}

// A folder that also holds a file livery does not own is the user's: the wanted file is kept,
// the foreign file untouched.
func TestFolderBecomesFileForeign(t *testing.T) {
	home := t.TempDir()
	helper := filepath.Join(home, ".h", "skills", "x", "helper")
	first := run(t, reshapePlan(home, true), lock.Lock{}, home)
	foreign := filepath.Join(helper, "notes.txt")
	put(t, foreign, "mine\n")
	res, err := compute(t, reshapePlan(home, false), first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	if op := opOf(t, res, helper); op != KeptYours {
		t.Errorf("helper: %s, want %s", op, KeptYours)
	}
	if _, err := Execute(res); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(foreign); string(b) != "mine\n" {
		t.Errorf("foreign file: %q", b)
	}
	if fi, err := os.Lstat(helper); err != nil || !fi.IsDir() {
		t.Errorf("helper: %v, %v; want the folder", fi, err)
	}
}

// An empty subfolder, or a link, in the old folder keeps the wanted file the user's.
func TestFolderBecomesFileOther(t *testing.T) {
	for name, add := range map[string]func(t *testing.T, helper string){
		"empty folder": func(t *testing.T, helper string) {
			if err := os.Mkdir(filepath.Join(helper, "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"link": func(t *testing.T, helper string) {
			if err := os.Symlink(filepath.Join(helper, "run.py"), filepath.Join(helper, "link")); err != nil {
				t.Fatal(err)
			}
		},
		"edited": func(t *testing.T, helper string) { put(t, filepath.Join(helper, "run.py"), "mine\n") },
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			helper := filepath.Join(home, ".h", "skills", "x", "helper")
			first := run(t, reshapePlan(home, true), lock.Lock{}, home)
			add(t, helper)
			res, err := compute(t, reshapePlan(home, false), first.Lock, home)
			if err != nil {
				t.Fatal(err)
			}
			if op := opOf(t, res, helper); op != KeptYours {
				t.Errorf("helper: %s, want %s", op, KeptYours)
			}
			if _, err := Execute(res); err != nil {
				t.Fatal(err)
			}
			if fi, err := os.Lstat(helper); err != nil || !fi.IsDir() {
				t.Errorf("helper: %v, %v; want the folder", fi, err)
			}
		})
	}
}

// A linked old file is not livery's to remove: the files below its place are kept.
func TestFileBecomesFolderLinked(t *testing.T) {
	home := t.TempDir()
	helper := filepath.Join(home, ".h", "skills", "x", "helper")
	first := run(t, reshapePlan(home, false), lock.Lock{}, home)
	target := filepath.Join(home, "elsewhere")
	put(t, target, "helper\n")
	if err := os.Remove(helper); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, helper); err != nil {
		t.Fatal(err)
	}
	res, err := compute(t, reshapePlan(home, true), first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	if op := opOf(t, res, filepath.Join(helper, "run.py")); op != KeptYours {
		t.Errorf("run.py: %s, want %s", op, KeptYours)
	}
	if _, err := Execute(res); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(helper); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("helper: %v, %v; want the link", fi, err)
	}
}

// When the old folder is still there at the time of the write, the write stops with the
// "changed while livery was running" finding.
func TestFolderBecomesFileChangedMidRun(t *testing.T) {
	home := t.TempDir()
	helper := filepath.Join(home, ".h", "skills", "x", "helper")
	first := run(t, reshapePlan(home, true), lock.Lock{}, home)
	res, err := compute(t, reshapePlan(home, false), first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	hookOnce(t, helper, func() { put(t, filepath.Join(helper, "new.txt"), "mine\n") })
	_, err = Execute(res)
	wantChanged(t, err, helper)
	if !strings.Contains(err.Error(), "changed while livery was running") {
		t.Errorf("err %v", err)
	}
}

// When the old file comes back between its removal and the write below it, the write stops
// with the "changed while livery was running" finding, not a directory error.
func TestFileBecomesFolderChangedMidRun(t *testing.T) {
	home := t.TempDir()
	helper := filepath.Join(home, ".h", "skills", "x", "helper")
	run1 := filepath.Join(helper, "lib", "util.py") // the first write below helper
	first := run(t, reshapePlan(home, false), lock.Lock{}, home)
	res, err := compute(t, reshapePlan(home, true), first.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	hookOnce(t, run1, func() { put(t, helper, "back\n") })
	_, err = Execute(res)
	if err == nil || !strings.Contains(err.Error(), "changed while livery was running") {
		t.Errorf("err %v, want changed while livery was running", err)
	}
}
