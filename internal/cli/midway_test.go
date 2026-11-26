package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
)

const (
	skillFile = "~/.kiro/skills/review/SKILL.md"
	steerFile = "~/.kiro/steering/go.md"
)

// readOnly makes the folder ~/<rel> read-only (creating it) so a later write there fails, and
// returns a function that makes it writable again. It skips the test when running as root.
func readOnly(t *testing.T, env adapter.Env, rel string) func() {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(env.Home, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	return func() {
		t.Helper()
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func readLock(t *testing.T, env adapter.Env) lock.Lock {
	t.Helper()
	l, found, err := lock.ReadIn(paths.LockFile(env), lock.ScopeOf(env))
	if err != nil || !found {
		t.Fatalf("lock: found %v, %v", found, err)
	}
	return l
}

func fileNames(l lock.Lock) []string {
	var names []string
	for p := range l.Files {
		names = append(names, p)
	}
	slices.Sort(names)
	return names
}

// failedFirstInstall runs a first update that writes the skill and the MCP entry, then stops at
// the read-only steering folder. It returns the source and the function that fixes the folder.
func failedFirstInstall(t *testing.T) (string, adapter.Env, func()) {
	t.Helper()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	fix := readOnly(t, env, filepath.Join(".kiro", "steering"))
	code, out, errs := runUpdate(t, env)
	if code != 3 {
		t.Fatalf("update with a read-only folder: exit %d, want 3\n%s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".kiro", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("no file was written before the failure: %v", err)
	}
	return src, env, fix
}

// A failed first install records what it wrote; uninstall then removes it.
func TestFailedFirstInstallIsRecorded(t *testing.T) {
	t.Parallel()
	_, env, _ := failedFirstInstall(t)
	l := readLock(t, env)
	if got := fileNames(l); !slices.Equal(got, []string{skillFile}) {
		t.Errorf("lock files %v, want only %s", got, skillFile)
	}
	if len(l.Merged) != 1 || l.Merged[0].Entries["github"] == "" {
		t.Errorf("lock merged %+v, want the github entry", l.Merged)
	}
	if l.Tag != "v0.1.0" || l.Commit == "" {
		t.Errorf("lock tag %q commit %q", l.Tag, l.Commit)
	}

	code, out, errs := runUninstall(t, env)
	if code != 0 {
		t.Fatalf("uninstall: exit %d\n%s%s", code, out, errs)
	}
	if !strings.Contains(out, "remove  "+skillFile) || !strings.Contains(out, "unset   ") {
		t.Errorf("uninstall output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".kiro", "skills")); !os.IsNotExist(err) {
		t.Errorf("skills still there: %v", err)
	}
	if _, err := os.Stat(paths.LockFile(env)); !os.IsNotExist(err) {
		t.Errorf("lock still there: %v", err)
	}
}

// After a failed first install the source moves on and changes a file already written: the
// retry updates it like any owned file.
func TestFailedFirstInstallThenNewCommit(t *testing.T) {
	t.Parallel()
	src, env, fix := failedFirstInstall(t)
	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")
	fix()
	code, out, errs := runUpdate(t, env)
	if code != 0 {
		t.Fatalf("retry: exit %d\n%s%s", code, out, errs)
	}
	for _, bad := range []string{"kept, yours", "edited by you"} {
		if strings.Contains(out, bad) {
			t.Errorf("retry reports %q:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "write   "+skillFile) || !strings.Contains(out, "write   "+steerFile) {
		t.Errorf("retry output:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(env.Home, ".kiro", "skills", "review", "SKILL.md")); string(b) != "skill v2\n" {
		t.Errorf("skill %q", b)
	}
	if l := readLock(t, env); l.Tag != "v0.2.0" || len(l.Files) != 2 {
		t.Errorf("lock %+v", l)
	}
}

// A failed update of an existing install: what was written has its new checksum in the lock,
// the rest its old one, and the lock keeps the old commit.
func TestFailedUpdateIsRecorded(t *testing.T) {
	t.Parallel()
	src, env := installed(t, "v0.1.0")
	old := readLock(t, env)
	enginetest.Commit(t, src, map[string]string{
		"base/skills/review/SKILL.md": "skill v2\n",
		"base/instructions/go.md":     "go rules v2\n",
	}, "v0.2.0")
	fix := readOnly(t, env, filepath.Join(".kiro", "steering"))
	code, out, errs := runUpdate(t, env)
	if code != 3 {
		t.Fatalf("update with a read-only folder: exit %d, want 3\n%s%s", code, out, errs)
	}
	l := readLock(t, env)
	if got, want := l.Files[skillFile], lock.Sum([]byte("skill v2\n")); got != want {
		t.Errorf("written file in the lock: %q, want the new %q", got, want)
	}
	if got, want := l.Files[steerFile], old.Files[steerFile]; got != want || want == "" {
		t.Errorf("unwritten file in the lock: %q, want the old %q", got, want)
	}
	if l.Commit != old.Commit || l.Tag != old.Tag {
		t.Errorf("lock moved to %s %s, want the old %s %s", l.Tag, l.Commit, old.Tag, old.Commit)
	}

	fix()
	code, out, errs = runUpdate(t, env)
	if code != 0 {
		t.Fatalf("retry: exit %d\n%s%s", code, out, errs)
	}
	if strings.Contains(out, skillFile) || !strings.Contains(out, "write   "+steerFile) {
		t.Errorf("retry output:\n%s", out)
	}
	if l := readLock(t, env); l.Tag != "v0.2.0" {
		t.Errorf("lock tag after the retry: %q", l.Tag)
	}
}

// An uninstall that fails halfway drops what it removed from the lock; the retry removes the
// rest and the lock.
func TestFailedUninstallIsRecorded(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	fix := readOnly(t, env, filepath.Join(".kiro", "steering"))
	code, out, errs := runUninstall(t, env)
	if code != 3 {
		t.Fatalf("uninstall with a read-only folder: exit %d, want 3\n%s%s", code, out, errs)
	}
	l := readLock(t, env)
	if got := fileNames(l); !slices.Equal(got, []string{steerFile}) {
		t.Errorf("lock files %v, want only %s", got, steerFile)
	}
	if len(l.Merged) != 0 {
		t.Errorf("lock merged %+v, want none", l.Merged)
	}

	fix()
	code, out, errs = runUninstall(t, env)
	if code != 0 {
		t.Fatalf("retry: exit %d\n%s%s", code, out, errs)
	}
	if !strings.Contains(out, "remove  "+steerFile) {
		t.Errorf("retry output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".kiro", "steering", "go.md")); !os.IsNotExist(err) {
		t.Errorf("go.md still there: %v", err)
	}
	if _, err := os.Stat(paths.LockFile(env)); !os.IsNotExist(err) {
		t.Errorf("lock still there: %v", err)
	}
}

// A lock without the schema key (v0.1.0) is refused with exit 1 and left as it is.
func TestUpdateAfterV010Install(t *testing.T) {
	t.Parallel()
	src, env := installed(t, "v0.1.0")
	l := readLock(t, env)
	s := l
	old := map[string]any{
		"livery": l.Livery, "source": src, "channel": "stable", "tag": s.Tag, "commit": s.Commit,
		"teams": []string{}, "harnesses": []string{"kiro"}, "files": l.Files, "merged": l.Merged,
	}
	b, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LockFile(env), b, 0o644); err != nil {
		t.Fatal(err)
	}
	oldCfg := `{"source":` + strconv.Quote(src) + `,"channel":"stable","teams":[],"harnesses":["kiro"]}`
	if err := os.WriteFile(paths.ConfigFile(env), []byte(oldCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	before, _ := os.ReadFile(paths.LockFile(env))
	code, out, errs := runUpdate(t, env)
	if code != 1 || !strings.Contains(errs, paths.LockFile(env)) || !strings.Contains(errs, "livery setup") {
		t.Fatalf("update: exit %d\n%s%s", code, out, errs)
	}
	if after, _ := os.ReadFile(paths.LockFile(env)); !bytes.Equal(before, after) {
		t.Errorf("lock changed:\n%s", after)
	}
}
