package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/lock"
)

func run(t *testing.T, plans []adapter.Plan, old lock.Lock, home string) Result {
	t.Helper()
	res, err := compute(t, plans, old, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(res); err != nil {
		t.Fatal(err)
	}
	return res
}

func report(r Result, home string) string {
	var b bytes.Buffer
	r.Home = home
	Report(&b, r, false)
	return b.String()
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s: still there (%v)", path, err)
	}
}

func skillPlan(home string, withB bool) adapter.Plan {
	root := filepath.Join(home, ".h")
	p := adapter.Plan{Root: root, Files: []adapter.File{
		{Path: filepath.Join(root, "skills", "a", "SKILL.md"), Data: []byte("a\n"), Origin: "team: a"},
		{Path: filepath.Join(root, "skills", "a", "run.sh"), Data: []byte("#!/bin/sh\n"), Exec: true, Origin: "team: a"},
	}}
	if withB {
		p.Files = append(p.Files, adapter.File{Path: filepath.Join(root, "skills", "b", "deep", "SKILL.md"), Data: []byte("b\n"), Origin: "team: b"})
	}
	return p
}

func TestExecuteFreshInstall(t *testing.T) {
	home := t.TempDir()
	plans := []adapter.Plan{skillPlan(home, true)}
	res := run(t, plans, lock.Lock{}, home)

	for _, f := range plans[0].Files {
		data, err := os.ReadFile(f.Path)
		if err != nil || string(data) != string(f.Data) {
			t.Fatalf("%s: %q, %v", f.Path, data, err)
		}
		want := os.FileMode(0o644)
		if f.Exec {
			want = 0o755
		}
		if got := mode(t, f.Path); got != want {
			t.Errorf("%s: mode %v, want %v", f.Path, got, want)
		}
	}
	wantReport := "write   ~/.h/skills/a/SKILL.md\nwrite   ~/.h/skills/a/run.sh\nwrite   ~/.h/skills/b/deep/SKILL.md\n"
	if got := report(res, home); got != wantReport {
		t.Errorf("report:\n%s\nwant:\n%s", got, wantReport)
	}

	// Second run with the new lock: nothing to do.
	again := run(t, plans, res.Lock, home)
	if again.Changes() || report(again, home) != "up to date\n" {
		t.Errorf("second run: %q", report(again, home))
	}
	// Interrupted run (lock not written): everything is adopted, with a line each.
	adopted := run(t, plans, lock.Lock{}, home)
	for _, a := range adopted.Actions {
		if a.Op != Adopt {
			t.Errorf("interrupted run: %s %s", a.Op, a.Path)
		}
	}
	if want := strings.ReplaceAll(wantReport, "write   ", "adopt   "); report(adopted, home) != want {
		t.Errorf("interrupted run report: %q", report(adopted, home))
	}
}

func TestExecuteRemovePrunesInsideRoot(t *testing.T) {
	home := t.TempDir()
	res := run(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	root := filepath.Join(home, ".h")
	sibling := filepath.Join(root, "skills", "mine.md")
	put(t, sibling, "mine\n")

	res = run(t, []adapter.Plan{skillPlan(home, false)}, res.Lock, home)
	if got, want := report(res, home), "remove  ~/.h/skills/b/deep/SKILL.md\n"; got != want {
		t.Errorf("report %q, want %q", got, want)
	}
	gone(t, filepath.Join(root, "skills", "b"))
	if _, err := os.Stat(sibling); err != nil {
		t.Error(err)
	}

	// Dropping everything leaves the root and the user's file.
	res = run(t, []adapter.Plan{{Root: root}}, res.Lock, home)
	gone(t, filepath.Join(root, "skills", "a"))
	if _, err := os.Stat(sibling); err != nil {
		t.Error(err)
	}
	if len(res.Lock.Files) != 0 {
		t.Errorf("lock files: %v", res.Lock.Files)
	}

	// With the user's file gone too, the root stays even when empty.
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}
	pruneDirs(filepath.Join(root, "skills", "x"), []string{root}, "")
	gone(t, filepath.Join(root, "skills"))
	if _, err := os.Stat(root); err != nil {
		t.Errorf("root removed: %v", err)
	}
}

func TestExecuteRemoveOutsideRoots(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, "state.json")
	plans := []adapter.Plan{{Root: filepath.Join(home, ".h"), Files: []adapter.File{{Path: state, Data: []byte("{}\n")}}}}
	res := run(t, plans, lock.Lock{}, home)
	res = run(t, []adapter.Plan{{Root: filepath.Join(home, ".h")}}, res.Lock, home)
	if got := report(res, home); got != "remove  ~/state.json\n" {
		t.Errorf("report %q", got)
	}
	gone(t, state)
	if _, err := os.Stat(home); err != nil {
		t.Errorf("home removed: %v", err)
	}
}

func TestExecuteSkipsEditedAppliesRest(t *testing.T) {
	home := t.TempDir()
	res := run(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	edited := filepath.Join(home, ".h", "skills", "a", "SKILL.md")
	put(t, edited, "hand edit\n")

	next := skillPlan(home, false)
	next.Files[0].Data = []byte("a v2\n")
	next.Files[1].Data = []byte("#!/bin/sh\necho new\n")
	res2, err := compute(t, []adapter.Plan{next}, res.Lock, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Blocked()) != 1 {
		t.Fatalf("blocked: %+v", res2.Blocked())
	}
	now, err := Execute(res2)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(edited); string(got) != "hand edit\n" {
		t.Errorf("edited file: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".h", "skills", "a", "run.sh")); string(got) != "#!/bin/sh\necho new\n" {
		t.Errorf("run.sh: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(home, ".h", "skills", "b")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("skill b not removed: %v", err)
	}
	k := key(res2.Scope(), edited)
	if now.Files[k] != res.Lock.Files[k] {
		t.Errorf("edited file record %q, want the old %q", now.Files[k], res.Lock.Files[k])
	}
	if !strings.Contains(report(res2, home), "edited by you: ~/.h/skills/a/SKILL.md\n") {
		t.Errorf("report: %s", report(res2, home))
	}

	// The next run finds it edited again and writes nothing.
	res3, err := compute(t, []adapter.Plan{next}, now, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.Blocked()) != 1 || strings.Contains(report(res3, home), "write") {
		t.Errorf("second run: %s", report(res3, home))
	}
}

func entryPlan(home string, entries ...adapter.Entry) []adapter.Plan {
	return []adapter.Plan{{Root: filepath.Join(home, ".h"), Entries: entries}}
}

func TestExecuteEntryKeepsModeAndKeys(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".h", "settings.json")
	put(t, path, `{"theme": "dark"}`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	e := adapter.Entry{Path: path, Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`)}
	res := run(t, entryPlan(home, e), lock.Lock{}, home)
	if got := report(res, home); got != "set     ~/.h/settings.json servers.x\n" {
		t.Errorf("report %q", got)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("mode %v", got)
	}
	var doc map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["theme"] != "dark" || doc["servers"].(map[string]any)["x"] == nil {
		t.Errorf("doc %s", data)
	}

	// Unset with an unrelated key: the file stays with that key and an empty object.
	res = run(t, entryPlan(home), res.Lock, home)
	if got := report(res, home); got != "unset   ~/.h/settings.json servers.x\n" {
		t.Errorf("report %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc = nil
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["theme"] != "dark" || len(doc["servers"].(map[string]any)) != 0 || len(doc) != 2 {
		t.Errorf("doc %s", data)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("mode %v", got)
	}
}

func TestExecuteEntryNewFileRemovedOnUnset(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "settings", "mcp.json")
	e := adapter.Entry{Path: path, Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`)}
	res := run(t, entryPlan(home, e), lock.Lock{}, home)
	if got := mode(t, path); got != 0o600 {
		t.Errorf("new file mode %v", got)
	}
	if got := mode(t, filepath.Dir(path)); got != 0o755 {
		t.Errorf("new dir mode %v", got)
	}
	res = run(t, entryPlan(home), res.Lock, home)
	if len(res.Lock.Merged) != 0 {
		t.Errorf("lock merged: %v", res.Lock.Merged)
	}
	gone(t, path)
	gone(t, filepath.Dir(path))
	if _, err := os.Stat(root); err != nil {
		t.Errorf("root removed: %v", err)
	}
}

func TestReportKept(t *testing.T) {
	r := Result{Home: "/home/u", Actions: []Action{
		{Op: Unchanged, Path: "/home/u/.h/x"},
		{Op: KeptYours, Path: "/home/u/.h/a"},
		{Op: KeptYours, Path: "/home/u/.h/s.json", Key: "k", Name: "n"},
		{Op: LeftEdited, Path: "/home/u/.h/b"},
	}}
	kept := " (the team's version is not installed; to install it, rename or delete yours, then run livery update)\n"
	want := "kept, yours: ~/.h/a" + kept + "kept, yours: ~/.h/s.json k.n" + kept + "left, edited by you: ~/.h/b (no longer in the release; the file is yours now, delete it to remove it)\n"
	if got := report(r, "/home/u"); got != want {
		t.Errorf("report:\n%s\nwant:\n%s", got, want)
	}
}

func TestDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := Result{Home: "/home/u", Actions: []Action{
		{Op: Write, Path: "/home/u/.h/a.md", Old: []byte("old line\n"), New: []byte("new line\n")},
		{Op: Write, Path: "/home/u/.h/new.md", New: []byte("fresh\n")},
		{Op: SetEntry, Path: "/home/u/.h/s.json", Key: "servers", Name: "x", New: []byte(`{"cmd":"x"}`)},
		{Op: Unchanged, Path: "/home/u/.h/same.md", Old: []byte("same\n"), New: []byte("same\n")},
	}}
	var b bytes.Buffer
	if err := Diff(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"\n-old line\n", "\n+new line\n", "now/~/.h/a.md", "livery/~/.h/a.md", "+fresh\n", "livery/~/.h/s.json servers.x", `+{"cmd":"x"}`} {
		if !strings.Contains(out, want) {
			t.Errorf("diff lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "same") || strings.Contains(out, tmp) {
		t.Errorf("diff:\n%s", out)
	}
	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

// A user's diff.external must not replace the unified diff, and a binary file is named by
// its destination.
func TestDiffIgnoresExternalDiffAndNamesBinary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[diff]\n\texternal = echo EXTERNAL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := Result{Home: "/home/u", Actions: []Action{
		{Op: Write, Path: "/home/u/.h/a.md", Old: []byte("old line\n"), New: []byte("new line\n")},
		{Op: Write, Path: "/home/u/.h/i.png", Old: []byte("\x89PNG\x00\x01"), New: []byte("\x89PNG\x00\x02")},
	}}
	var b bytes.Buffer
	if err := Diff(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"\n-old line\n", "\n+new line\n", "Binary files now/~/.h/i.png and livery/~/.h/i.png differ\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "EXTERNAL") || strings.Contains(out, tmp) {
		t.Errorf("diff:\n%s", out)
	}
}

func TestExecuteAdoptDoesNotMutate(t *testing.T) {
	home := t.TempDir()
	plans := []adapter.Plan{skillPlan(home, true)}
	run(t, plans, lock.Lock{}, home)
	var paths []string
	for _, f := range plans[0].Files {
		paths = append(paths, f.Path)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, p := range paths {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	// No lock: every identical file is adopted.
	res, err := compute(t, plans, lock.Lock{}, home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { beforeMutation = nil })
	beforeMutation = func(a Action) { t.Errorf("mutation attempted: %s %s", a.Op, a.Path) }
	if _, err := Execute(res); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || !fi.ModTime().Equal(old) {
			t.Errorf("%s touched: %v, %v", p, fi, err)
		}
	}
}

func TestDiffMasksUserValuesOfEntry(t *testing.T) {
	old := []byte(`{
  "command": "run-new",
  "args": ["--tok", "s3cr3tArg"],
  "env": {
    "TOKEN": "s3cr3tTok",
    "REGION": "eu"
  },
  "headers": {"Authorization": "Bearer s3cr3tTok"}
}`)
	want := []byte(`{
  "command": "run",
  "args": ["--tok", "x"],
  "env": {
    "TOKEN": "${TOKEN}",
    "REGION": "eu"
  },
  "headers": {"Authorization": "Bearer ${TOKEN}"}
}`)
	r := Result{Home: "/home/u", Actions: []Action{{Op: Edited, Path: "/home/u/.h/m.json", Key: "mcp", Name: "x", Old: old, New: want}}}
	var b bytes.Buffer
	if err := Diff(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "s3cr3tTok") {
		t.Errorf("secret printed:\n%s", out)
	}
	for _, w := range []string{"-    \"TOKEN\": \"***\",\n", "+    \"TOKEN\": \"${TOKEN}\",\n", "     \"REGION\": \"eu\"\n", "-  \"command\": \"run-new\",\n", "s3cr3tArg"} {
		if !strings.Contains(out, w) {
			t.Errorf("diff lacks %q:\n%s", w, out)
		}
	}
	if got := maskValues([]byte("[1]"), nil); string(got) != "[1]" {
		t.Errorf("non-object changed: %s", got)
	}
}

// A changed value of an entry that spans several lines shows as one changed line, whatever the
// formatting of either side.
func TestDiffEntryOneChangedValueIsOneLine(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	old := []byte("{\n    \"command\": \"my-gh\",\n    \"args\": [\"--a\", \"--b\"],\n    \"url\": \"https://x\"\n}")
	want := []byte(`{"command":"gh-mcp","args":["--a","--b"],"url":"https://x"}`)
	r := Result{Home: "/home/u", Actions: []Action{{Op: Edited, Path: "/home/u/.h/m.json", Key: "mcp", Name: "x", Old: old, New: want}}}
	var b bytes.Buffer
	if err := Diff(&b, r); err != nil {
		t.Fatal(err)
	}
	minus, plus := 0, 0
	for _, l := range strings.Split(b.String(), "\n") {
		switch {
		case strings.HasPrefix(l, "---"), strings.HasPrefix(l, "+++"):
		case strings.HasPrefix(l, "-"):
			minus++
		case strings.HasPrefix(l, "+"):
			plus++
		}
	}
	if minus != 1 || plus != 1 || !strings.Contains(b.String(), "\n-  \"command\": \"my-gh\",\n+  \"command\": \"gh-mcp\",\n") {
		t.Errorf("want one - and one + line:\n%s", b.String())
	}
}
