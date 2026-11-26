package apply

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
)

// linked creates link -> target (a folder holding files) and returns a check that the link is
// still in place and the target is unchanged.
func linked(t *testing.T, link, target string, files map[string]string) func() {
	t.Helper()
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		put(t, filepath.Join(target, name), data)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, target)
	return func() {
		t.Helper()
		fi, err := os.Lstat(link)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s: no longer a link (%v)", link, err)
		}
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Fatalf("%s: points to %q (%v), want %q", link, got, err, target)
		}
		if after := snapshot(t, target); after != before {
			t.Fatalf("link target changed:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	}
}

func TestLinkedFolderWantedFileIsKept(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	check := linked(t, filepath.Join(root, "rules"), filepath.Join(home, "elsewhere"), map[string]string{"other.md": "other\n"})
	path := filepath.Join(root, "rules", "a.md")
	plans := []adapter.Plan{{Root: root, Files: []adapter.File{{Path: path, Data: []byte(wantData), Origin: "o"}}}}

	res := run(t, plans, lock.Lock{}, home)
	if len(res.Actions) != 1 || res.Actions[0].Op != KeptYours {
		t.Fatalf("actions %+v", res.Actions)
	}
	if len(res.Lock.Files) != 0 {
		t.Errorf("lock files %v", res.Lock.Files)
	}
	gone(t, filepath.Join(home, "elsewhere", "a.md"))
	check()
}

func TestLinkedFolderOwnedFileIsLeft(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	// The folder became a link to a copy holding the file livery installed before.
	check := linked(t, filepath.Join(root, "rules"), filepath.Join(home, "elsewhere"), map[string]string{"a.md": oldData})
	old := lock.Lock{Files: map[string]string{"~/.h/rules/a.md": lock.Sum([]byte(oldData))}}

	res := run(t, []adapter.Plan{{Root: root}}, old, home)
	if len(res.Actions) != 1 || res.Actions[0].Op != LeftEdited {
		t.Fatalf("actions %+v", res.Actions)
	}
	if len(res.Lock.Files) != 0 || res.Changes() {
		t.Errorf("lock files %v, changes %v", res.Lock.Files, res.Changes())
	}
	check()
}

func TestLinkedFolderSharedFileIsRefused(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	link := filepath.Join(root, "settings")
	check := linked(t, link, filepath.Join(home, "elsewhere"), map[string]string{"mcp.json": `{"servers":{}}`})
	plans := []adapter.Plan{{Root: root, Entries: []adapter.Entry{{Path: filepath.Join(link, "mcp.json"), Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`), Origin: "o"}}}}

	_, err := compute(t, plans, lock.Lock{}, home)
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Finding {
		t.Fatalf("want a Finding, got %v", err)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, link+":") || strings.Contains(msg, "mcp.json") || !strings.Contains(msg, "livery update") {
		t.Errorf("message %q", msg)
	}
	check()
}

func TestLinkedRootIsUsed(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	realDir := filepath.Join(home, "dotfiles", "h")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, root); err != nil {
		t.Fatal(err)
	}
	res := run(t, []adapter.Plan{skillPlan(home, true)}, lock.Lock{}, home)
	if _, err := os.Stat(filepath.Join(realDir, "skills", "b", "deep", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	res = run(t, []adapter.Plan{skillPlan(home, false)}, res.Lock, home)
	if got, want := report(res, home), "remove  ~/.h/skills/b/deep/SKILL.md\n"; got != want {
		t.Errorf("report %q, want %q", got, want)
	}
	gone(t, filepath.Join(realDir, "skills", "b"))
	if fi, err := os.Lstat(root); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("root link: %v", err)
	}

	// A shared JSON file inside the linked root is edited as usual.
	plans := []adapter.Plan{{Root: root, Entries: []adapter.Entry{{Path: filepath.Join(root, "mcp.json"), Key: "servers", Name: "x", Value: json.RawMessage(`{"cmd":"x"}`), Origin: "o"}}}}
	run(t, plans, lock.Lock{}, home)
	if data, err := os.ReadFile(filepath.Join(realDir, "mcp.json")); err != nil || !strings.Contains(string(data), `"x"`) {
		t.Errorf("mcp.json %q, %v", data, err)
	}
}

func TestPruneDirsKeepsLink(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".h")
	link := filepath.Join(root, "a")
	target := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(filepath.Join(target, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	check := linked(t, link, target, nil)
	// The last file of <root>/a/b went; b itself sits behind the link, as does the file.
	pruneDirs(filepath.Join(link, "b", "f.md"), []string{root}, "")
	check()

	// A link directly holding the removed file is not removed either.
	pruneDirs(filepath.Join(link, "f.md"), []string{root}, "")
	check()
}
