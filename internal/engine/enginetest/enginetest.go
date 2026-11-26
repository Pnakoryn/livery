// Package enginetest holds test helpers: a fixture shared repository and an isolated Env.
package enginetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
)

// Main is the body of a TestMain: it isolates every git call of the test process (livery's
// own and the tests') from the developer's git configuration, then runs the tests.
//
//	func TestMain(m *testing.M) { enginetest.Main(m) }
func Main(m *testing.M) {
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_AUTHOR_DATE": "2020-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com", "GIT_COMMITTER_DATE": "2020-01-01T00:00:00Z",
	} {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, repo string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if c == "" {
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			continue
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, ".sh") {
			mode = 0o755
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(full, mode); err != nil {
			t.Fatal(err)
		}
	}
}

// Source creates a git repository in t.TempDir() holding files (path -> content; a path
// ending in ".sh" is made executable), commits, tags the commit with each tag, and returns its path.
func Source(t *testing.T, files map[string]string, tags ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, files)
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "fixture")
	for _, tag := range tags {
		git(t, repo, "tag", tag)
	}
	return repo
}

// Commit writes files (empty content deletes the path), commits, and tags when tag is not empty.
func Commit(t *testing.T, repo string, files map[string]string, tag string) {
	t.Helper()
	write(t, repo, files)
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "change")
	if tag != "" {
		git(t, repo, "tag", tag)
	}
}

// Env returns an adapter.Env with a new temporary home and no environment variables.
func Env(t *testing.T) adapter.Env {
	t.Helper()
	return adapter.Env{Home: t.TempDir(), Getenv: func(string) string { return "" }}
}
