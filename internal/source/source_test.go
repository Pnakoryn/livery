package source

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
}

// newRepo builds a repository tagged v0.1.0, v0.2.0, v0.10.0 and one later commit.
func newRepo(t *testing.T) string {
	t.Helper()
	gitEnv(t)
	repo := filepath.Join(t.TempDir(), "shared.git-src")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "base/a.txt", "one", 0o644)
	commit(t, repo, "1")
	git(t, repo, "tag", "v0.1.0")
	write(t, repo, "base/a.txt", "two", 0o644)
	write(t, repo, "base/bin/run.sh", "#!/bin/sh\n", 0o755)
	commit(t, repo, "2")
	git(t, repo, "tag", "v0.2.0")
	write(t, repo, "base/a.txt", "ten", 0o644)
	commit(t, repo, "3")
	git(t, repo, "tag", "v0.10.0")
	write(t, repo, "base/a.txt", "later", 0o644)
	commit(t, repo, "4")
	return repo
}

var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

func wantCode(t *testing.T, err error, code exit.Code) *exit.Error {
	t.Helper()
	var ee *exit.Error
	if !errors.As(err, &ee) || ee.Code != code {
		t.Fatalf("want exit code %d, got %v", code, err)
	}
	return ee
}

func TestResolveChannels(t *testing.T) {
	repo := newRepo(t)
	cache := t.TempDir()

	st, err := Resolve(cache, repo, "stable", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Tag != "v0.10.0" || !hex40.MatchString(st.Commit) {
		t.Fatalf("stable: %+v", st)
	}
	if want := git(t, repo, "rev-parse", "v0.10.0^{commit}"); st.Commit != want {
		t.Fatalf("stable commit %s want %s", st.Commit, want)
	}

	pin, err := Resolve(cache, repo, "v0.2.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := git(t, repo, "rev-parse", "v0.2.0^{commit}"); pin.Tag != "v0.2.0" || pin.Commit != want {
		t.Fatalf("pin: %+v want %s", pin, want)
	}

	next, err := Resolve(cache, repo, "next", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := git(t, repo, "rev-parse", "HEAD"); next.Tag != "" || next.Commit != want || next.Commit == st.Commit {
		t.Fatalf("next: %+v want %s", next, want)
	}
}

func TestResolveErrors(t *testing.T) {
	repo := newRepo(t)
	cache := t.TempDir()

	_, err := Resolve(cache, repo, "nightly", "")
	if ee := wantCode(t, err, exit.Usage); !strings.Contains(ee.Message, `"nightly"`) {
		t.Errorf("message %q does not name the channel", ee.Message)
	}

	_, err = Resolve(cache, repo, "v9.9.9", "")
	if ee := wantCode(t, err, exit.Usage); !strings.Contains(ee.Message, "v9.9.9") {
		t.Fatalf("message: %s", ee.Message)
	}

	missing := filepath.Join(t.TempDir(), "nope")
	_, err = Resolve(cache, missing, "next", "")
	if ee := wantCode(t, err, exit.Environment); !strings.Contains(ee.Message, missing) {
		t.Fatalf("message: %s", ee.Message)
	}
}

func TestResolveNoReleaseTag(t *testing.T) {
	gitEnv(t)
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "a.txt", "x", 0o644)
	commit(t, repo, "1")
	_, err := Resolve(t.TempDir(), repo, "stable", "")
	if ee := wantCode(t, err, exit.Finding); !strings.Contains(ee.Message, "next") {
		t.Fatalf("message: %s", ee.Message)
	}
}

func TestResolveSeesNewTag(t *testing.T) {
	repo := newRepo(t)
	cache := t.TempDir()
	if _, err := Resolve(cache, repo, "stable", ""); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "tag", "v0.11.0")
	r, err := Resolve(cache, repo, "stable", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v0.11.0" {
		t.Fatalf("got %+v", r)
	}
}

func TestExport(t *testing.T) {
	repo := newRepo(t)
	cache := t.TempDir()
	r, err := Resolve(cache, repo, "v0.2.0", "")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := Export(cache, r)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(cache, "tree", r.Commit) {
		t.Fatalf("dir %s", dir)
	}
	b, err := os.ReadFile(filepath.Join(dir, "base/a.txt"))
	if err != nil || string(b) != "two" {
		t.Fatalf("a.txt: %q %v", b, err)
	}
	for name, want := range map[string]os.FileMode{"base/a.txt": 0o644, "base/bin/run.sh": 0o755} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != want {
			t.Fatalf("%s: %v %v", name, fi, err)
		}
	}
	fi, err := os.Stat(filepath.Join(dir, "base/bin"))
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("bin: %v %v", fi, err)
	}

	marker := filepath.Join(dir, "marker")
	write(t, dir, "marker", "m", 0o644)
	dir2, err := Export(cache, r)
	if err != nil || dir2 != dir {
		t.Fatalf("second export: %s %v", dir2, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("marker gone: tree was rewritten")
	}
}

func TestExportRejectsSymlink(t *testing.T) {
	repo := newRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "base"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(repo, "base", "thelink")); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "link")
	cache := t.TempDir()
	r, err := Resolve(cache, repo, "next", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Export(cache, r)
	if ee := wantCode(t, err, exit.Usage); !strings.Contains(ee.Message, "thelink") {
		t.Fatalf("message: %s", ee.Message)
	}
	if _, err := os.Stat(filepath.Join(cache, "tree", r.Commit)); !os.IsNotExist(err) {
		t.Fatalf("tree exists: %v", err)
	}
	ents, _ := os.ReadDir(filepath.Join(cache, "tree"))
	if len(ents) != 0 {
		t.Fatalf("leftover: %v", ents)
	}
}

func TestUnsafeName(t *testing.T) {
	for _, n := range []string{"/etc/passwd", "../x", "a/../b", ""} {
		if !unsafeName(n) {
			t.Errorf("%q should be unsafe", n)
		}
	}
	if unsafeName("a/b.txt") || unsafeName("dir/") {
		t.Error("safe name flagged")
	}
}

func TestCacheName(t *testing.T) {
	if n := cacheName("git@host:team/shared.git"); !regexp.MustCompile(`^shared-[0-9a-f]{12}\.git$`).MatchString(n) {
		t.Fatal(n)
	}
	if n := cacheName("/"); !strings.HasPrefix(n, "src-") {
		t.Fatal(n)
	}
}

func TestResolveRejectsOptionLikeSource(t *testing.T) {
	gitEnv(t)
	_, err := Resolve(t.TempDir(), "--upload-pack=touch /tmp/x", "stable", "")
	if ee := wantCode(t, err, exit.Usage); !strings.Contains(ee.Message, "--upload-pack") {
		t.Errorf("message %q does not name the source", ee.Message)
	}
}

func TestResolveRelativeSource(t *testing.T) {
	repo := newRepo(t)
	t.Chdir(filepath.Dir(repo))
	rel := filepath.Base(repo)
	for _, ch := range []string{"stable", "next"} {
		r, err := Resolve(t.TempDir(), rel, ch, "")
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		if !filepath.IsAbs(r.Source) {
			t.Fatalf("source not absolute: %s", r.Source)
		}
	}
}

func TestExportDirModesIgnoreUmask(t *testing.T) {
	repo := newRepo(t)
	cache := t.TempDir()
	r, err := Resolve(cache, repo, "v0.2.0", "")
	if err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir, err := Export(cache, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Join(dir, "base/bin")} {
		fi, err := os.Stat(d)
		if err != nil || fi.Mode().Perm() != 0o755 {
			t.Fatalf("%s: %v %v", d, fi, err)
		}
	}
}

func TestCanonical(t *testing.T) {
	repo := newRepo(t)
	t.Chdir(filepath.Dir(repo))
	rel := "./" + filepath.Base(repo)
	if got := Canonical(rel); got != repo {
		t.Errorf("Canonical(%q) = %q, want %q", rel, got, repo)
	}
	for _, s := range []string{"missing/dir", "https://example.com/r.git", "git@host:team/r.git", repo, ""} {
		if got := Canonical(s); got != s {
			t.Errorf("Canonical(%q) = %q, want unchanged", s, got)
		}
	}
}

// wantNext fails unless the message names a livery command to run next.
func wantNext(t *testing.T, msg string) {
	t.Helper()
	if !regexp.MustCompile(`livery (update|setup|doctor)`).MatchString(msg) {
		t.Fatalf("message names no next command: %s", msg)
	}
}

func TestResolveErrorsNameNextCommand(t *testing.T) {
	repo := newRepo(t)

	// cannot create cache: the cache directory is below a file.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(filepath.Join(blocker, "cache"), repo, "stable", "")
	ee := wantCode(t, err, exit.Environment)
	if !strings.Contains(ee.Message, "cannot create cache") {
		t.Fatalf("message: %s", ee.Message)
	}
	wantNext(t, ee.Message)

	// cannot fetch: the source does not exist.
	cache := t.TempDir()
	missing := filepath.Join(t.TempDir(), "nope")
	_, err = Resolve(cache, missing, "next", "")
	ee = wantCode(t, err, exit.Environment)
	if !strings.Contains(ee.Message, "no such folder") || strings.Contains(ee.Message, "credential") {
		t.Fatalf("message: %s", ee.Message)
	}
	wantNext(t, ee.Message)

	// cannot fetch: a folder that is no git repository.
	_, err = Resolve(t.TempDir(), t.TempDir(), "next", "")
	ee = wantCode(t, err, exit.Environment)
	if !strings.Contains(ee.Message, "does not appear to be a git repository") || strings.Contains(ee.Message, "and the repository exists") {
		t.Fatalf("message: %s", ee.Message)
	}
	wantNext(t, ee.Message)

	// cannot list tags: a broken setting in the cache makes `git tag -l` fail, fetch still works.
	cache = t.TempDir()
	if _, err := Resolve(cache, repo, "stable", ""); err != nil {
		t.Fatal(err)
	}
	cached, _ := filepath.Glob(filepath.Join(cache, "src", "*"))
	if len(cached) != 1 {
		t.Fatalf("cache: %v", cached)
	}
	git(t, cached[0], "config", "tag.sort", "nonsense")
	_, err = Resolve(cache, repo, "stable", "")
	ee = wantCode(t, err, exit.Environment)
	if !strings.Contains(ee.Message, "cannot list tags") || !strings.Contains(ee.Message, cached[0]) {
		t.Fatalf("message: %s", ee.Message)
	}
	wantNext(t, ee.Message)
}

func TestResolveCannotInitCacheNamesNextCommand(t *testing.T) {
	repo := newRepo(t)
	cache := t.TempDir()
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	// The cache repository directory exists but git cannot initialise it: it is read-only.
	dir := filepath.Join(cache, "src", cacheName(Canonical(repo)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, err := Resolve(cache, repo, "stable", "")
	ee := wantCode(t, err, exit.Environment)
	if !strings.Contains(ee.Message, "cannot create cache") {
		t.Fatalf("message: %s", ee.Message)
	}
	wantNext(t, ee.Message)
}

func TestResolveIgnoresCallerGitDir(t *testing.T) {
	repo := newRepo(t)
	other := t.TempDir()
	git(t, other, "init", "-q", "-b", "side")
	write(t, other, "x.txt", "x", 0o644)
	commit(t, other, "x")
	git(t, other, "tag", "othertag")
	refs := func() string { return git(t, other, "for-each-ref") }
	before := refs()

	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))
	cache := t.TempDir()
	r, err := Resolve(cache, repo, "stable", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v0.10.0" {
		t.Fatalf("got %+v", r)
	}
	if after := refs(); after != before {
		t.Fatalf("caller's repository changed:\nbefore %s\nafter %s", before, after)
	}
}

func TestGitEnvDropsLocalVars(t *testing.T) {
	t.Setenv("GIT_DIR", "/x")
	t.Setenv("GIT_CONFIG_KEY_0", "a")
	t.Setenv("GIT_CONFIG_VALUE_0", "b")
	t.Setenv("GIT_AUTHOR_NAME", "keep")
	for _, kv := range GitEnv("GIT_TERMINAL_PROMPT=0") {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_CONFIG_KEY_") || strings.HasPrefix(kv, "GIT_CONFIG_VALUE_") {
			t.Fatalf("kept %s", kv)
		}
	}
}

func TestUnknownTagListsTen(t *testing.T) {
	var rel []string
	for i := 12; i >= 1; i-- {
		rel = append(rel, fmt.Sprintf("v1.%d.0", i))
	}
	msg := unknownTag("v9.9.9", "src", rel)
	want := "v1.12.0, v1.11.0, v1.10.0, v1.9.0, v1.8.0, v1.7.0, v1.6.0, v1.5.0, v1.4.0, v1.3.0, ...;"
	if !strings.Contains(msg, want) {
		t.Errorf("message %q lacks %q", msg, want)
	}
	if strings.Contains(msg, "v0.2.0") {
		t.Errorf("message lists more than 10: %q", msg)
	}
}

// The message calls the repository "the shared repository"; "source" is only the address.
func TestUnknownTagWords(t *testing.T) {
	for _, rel := range [][]string{nil, {"v1.0.0"}} {
		msg := unknownTag("v9.9.9", "addr", rel)
		if !strings.Contains(msg, "in the shared repository addr") || strings.Contains(msg, "source") {
			t.Errorf("message %q", msg)
		}
	}
}

func TestExportIgnoresFilesOutsideLayout(t *testing.T) {
	repo := newRepo(t)
	if err := os.Symlink("AGENTS.md", filepath.Join(repo, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "docs/x.md", "d", 0o644)
	write(t, repo, "livery.json", `{"schema":1}`, 0o644)
	commit(t, repo, "extras")
	cache := t.TempDir()
	r, err := Resolve(cache, repo, "next", "")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := Export(cache, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"CLAUDE.md", "docs"} {
		if _, err := os.Lstat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s exported: %v", n, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "livery.json")); err != nil {
		t.Error(err)
	}
}

func TestExportRejectsSymlinkInBase(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "base/a.md", "a", 0o644)
	if err := os.Symlink("a.md", filepath.Join(repo, "base", "l.md")); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "link")
	cache := t.TempDir()
	r, err := Resolve(cache, repo, "next", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Export(cache, r)
	if ee := wantCode(t, err, exit.Usage); !strings.Contains(ee.Message, "l.md") {
		t.Errorf("message %q does not name the link", ee.Message)
	}
}

func TestGitReason(t *testing.T) {
	se := "fatal: '/x' does not appear to be a git repository\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n"
	if got, want := gitReason(se), "'/x' does not appear to be a git repository"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := gitReason("hint: x\nerror: boom\nlast\n"); got != "boom" {
		t.Errorf("got %q", got)
	}
	if got := gitReason("one\ntwo\n\n"); got != "two" {
		t.Errorf("got %q", got)
	}
}
