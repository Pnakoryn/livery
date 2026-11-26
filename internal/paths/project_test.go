package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
)

func TestProjectPaths(t *testing.T) {
	e := adapter.Env{Home: "/h", Project: "/p", Getenv: func(k string) string {
		return map[string]string{"XDG_CONFIG_HOME": "/x", "XDG_CACHE_HOME": "/c"}[k]
	}}
	if got := ConfigFile(e); got != "/p/.livery/config.json" {
		t.Errorf("ConfigFile = %q", got)
	}
	if got := LockFile(e); got != "/p/.livery/lock.json" {
		t.Errorf("LockFile = %q", got)
	}
	if got := PersonalDir(e); got != "" {
		t.Errorf("PersonalDir = %q, want empty", got)
	}
	if got := CacheDir(e); got != "/c/livery" {
		t.Errorf("CacheDir = %q", got)
	}
}

func mkfile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindProject(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "proj", ".livery", "config.json"))
	nested := filepath.Join(root, "proj", "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindProject(nested); !ok || got != filepath.Join(root, "proj") {
		t.Errorf("FindProject(nested) = %q, %v", got, ok)
	}
	if got, ok := FindProject(filepath.Join(root, "proj")); !ok || got != filepath.Join(root, "proj") {
		t.Errorf("FindProject(root) = %q, %v", got, ok)
	}

	// Inner project wins over an outer one.
	mkfile(t, filepath.Join(nested, ".livery", "config.json"))
	if got, ok := FindProject(nested); !ok || got != nested {
		t.Errorf("FindProject(inner) = %q, %v", got, ok)
	}
}

func TestFindProjectNone(t *testing.T) {
	dir := t.TempDir()
	// A .livery folder without config.json, or with config.json as a folder, is no project.
	if err := os.MkdirAll(filepath.Join(dir, "x", ".livery", "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindProject(filepath.Join(dir, "x")); ok {
		// A folder of that name above t.TempDir() would be a broken test machine.
		t.Errorf("FindProject = %q, true; want false", got)
	}
}

func TestFindProjectLinked(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	mkfile(t, target)
	if err := os.MkdirAll(filepath.Join(dir, "a", ".livery"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "a", ".livery", "config.json")); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindProject(filepath.Join(dir, "a")); ok {
		t.Errorf("linked config.json: FindProject = %q, true", got)
	}
	// A linked .livery folder does not count either.
	mkfile(t, filepath.Join(dir, "other", "config.json"))
	if err := os.MkdirAll(filepath.Join(dir, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "other"), filepath.Join(dir, "b", ".livery")); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindProject(filepath.Join(dir, "b")); ok {
		t.Errorf("linked .livery: FindProject = %q, true", got)
	}
}
