package lock

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func TestScopeStoreLoad(t *testing.T) {
	m := Scope{Home: "/home/u"}
	if got, err := m.Store("/home/u/.kiro/a"); err != nil || got != "~/.kiro/a" {
		t.Errorf("machine Store = %q, %v", got, err)
	}
	if got := m.Load("~/.kiro/a"); got != "/home/u/.kiro/a" {
		t.Errorf("machine Load = %q", got)
	}
	p := Scope{Home: "/home/u", Project: "/w/p"}
	if got, err := p.Store("/w/p/.kiro/skills/review/SKILL.md"); err != nil || got != ".kiro/skills/review/SKILL.md" {
		t.Errorf("project Store = %q, %v", got, err)
	}
	if got := p.Load(".kiro/skills/review/SKILL.md"); got != "/w/p/.kiro/skills/review/SKILL.md" {
		t.Errorf("project Load = %q", got)
	}
	if got := p.Show("/home/u/x"); got != "/home/u/x" {
		t.Errorf("project Show outside = %q", got)
	}
	for _, abs := range []string{"/w/p", "/w/px/a", "/w/a", "/home/u/.kiro/a", "rel/a"} {
		if _, err := p.Store(abs); err == nil || !strings.Contains(err.Error(), "refusing to write outside the project") {
			t.Errorf("Store(%q) err = %v", abs, err)
		}
	}
}

func TestReadInRefusesProjectPaths(t *testing.T) {
	scope := Scope{Home: "/home/u", Project: "/w/p"}
	for _, bad := range []string{"~/x", "/abs/x", "../x", "a/../x", "./x", ""} {
		for _, merged := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "lock.json")
			l := Lock{Livery: "t", Source: "s", Commit: "c", Files: map[string]string{"ok/a": "sha256:0"}}
			if merged {
				l.Merged = []Merged{{Path: bad, Key: "k", Entries: map[string]string{"n": "sha256:0"}}}
			} else {
				l.Files[bad] = "sha256:0"
			}
			if err := l.Write(path); err != nil {
				t.Fatal(err)
			}
			_, _, err := ReadIn(path, scope)
			var e *exit.Error
			if !errors.As(err, &e) || e.Code != exit.Finding || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "not a livery lock") {
				t.Errorf("%q (merged %v): err = %v", bad, merged, err)
			}
			// The machine scope accepts the same lock.
			if _, _, err := ReadIn(path, Scope{Home: "/home/u"}); err != nil {
				t.Errorf("machine ReadIn %q: %v", bad, err)
			}
		}
	}
}

func TestReadInRefusesReservedProjectPaths(t *testing.T) {
	scope := Scope{Home: "/home/u", Project: "/w/p"}
	for _, bad := range []string{".git/hooks/x", ".livery/x", ".git", ".GIT/config"} {
		for _, merged := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "lock.json")
			l := Lock{Livery: "t", Source: "s", Commit: "c", Files: map[string]string{"ok/a": "sha256:0"}}
			if merged {
				l.Merged = []Merged{{Path: bad, Key: "k", Entries: map[string]string{"n": "sha256:0"}}}
			} else {
				l.Files[bad] = "sha256:0"
			}
			if err := l.Write(path); err != nil {
				t.Fatal(err)
			}
			_, _, err := ReadIn(path, scope)
			var e *exit.Error
			if !errors.As(err, &e) || e.Code != exit.Finding || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), bad) {
				t.Errorf("%q (merged %v): err = %v", bad, merged, err)
			}
		}
	}
	// A folder that only starts like a reserved one is fine.
	path := filepath.Join(t.TempDir(), "lock.json")
	l := Lock{Livery: "t", Source: "s", Commit: "c", Files: map[string]string{".github/x": "sha256:0", ".livery-x/y": "sha256:0"}}
	if err := l.Write(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadIn(path, scope); err != nil {
		t.Errorf("ReadIn: %v", err)
	}
}
