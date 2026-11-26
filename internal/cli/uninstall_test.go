package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/paths"
)

func runUninstall(t *testing.T, env adapter.Env, args ...string) (int, string, string) {
	t.Helper()
	return runCLI(t, env, "", false, append([]string{"uninstall"}, args...)...)
}

func TestUninstallRemovesOwned(t *testing.T) {
	t.Parallel()
	src, env := installed(t, "v0.1.0")
	kiro := filepath.Join(env.Home, ".kiro")
	mcp := filepath.Join(kiro, "settings", "mcp.json")
	if err := os.WriteFile(filepath.Join(kiro, "steering-own.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(mcp)
	if err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(string(b), `"mcpServers": {`, `"mcpServers": {"own":{"command":"x"},`, 1)
	if mine == string(b) {
		t.Fatalf("cannot inject server into %s", b)
	}
	if err := os.WriteFile(mcp, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out, errs := runUninstall(t, env, "--plan")
	if code != 0 || !strings.Contains(out, "remove  ") || !strings.Contains(out, "unset   ") {
		t.Fatalf("plan: %d %s%s", code, out, errs)
	}

	// no network, no config
	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	cfgPath := paths.ConfigFile(env)
	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	code, out, errs = runUninstall(t, env)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.HasPrefix(out, "livery uninstall\n") || strings.Contains(out, "kept    ") {
		t.Errorf("output %q", out)
	}
	if _, err := os.Stat(paths.LockFile(env)); !os.IsNotExist(err) {
		t.Errorf("lock still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kiro, "skills")); !os.IsNotExist(err) {
		t.Errorf("skills still there: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(kiro, "steering-own.md")); string(got) != "mine\n" {
		t.Errorf("own file: %q", got)
	}
	got, _ := os.ReadFile(mcp)
	if !strings.Contains(string(got), `"own"`) || strings.Contains(string(got), "github") {
		t.Errorf("mcp.json: %s", got)
	}

	code, out, _ = runUninstall(t, env)
	if code != 0 || out != "nothing installed\n" {
		t.Errorf("second: %d %q", code, out)
	}
}

func TestUninstallEmptyRootAndReinstall(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	if code, out, errs := runUninstall(t, env); code != 0 || !strings.Contains(out, "kept    ") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	ents, err := os.ReadDir(filepath.Join(env.Home, ".kiro"))
	if err != nil || len(ents) != 0 {
		t.Errorf("root: %v %v", ents, err)
	}
	if _, err := os.Stat(paths.ConfigFile(env)); err != nil {
		t.Errorf("config gone: %v", err)
	}
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("update: %d %s%s", code, out, errs)
	}
	if _, err := os.Stat(paths.LockFile(env)); err != nil {
		t.Errorf("lock not restored: %v", err)
	}
	kiro := filepath.Join(env.Home, ".kiro")
	if _, err := os.Stat(filepath.Join(kiro, "skills", "review", "SKILL.md")); err != nil {
		t.Errorf("skill not restored: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(kiro, "settings", "mcp.json")); !strings.Contains(string(got), "github") {
		t.Errorf("mcp.json entries not restored: %s", got)
	}
}

func TestUninstallLeavesEdited(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	f := filepath.Join(env.Home, ".kiro", "steering", "go.md")
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("layout differs: %v", err)
	}
	if err := os.WriteFile(f, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUninstall(t, env)
	if code != 0 || !strings.Contains(out, "left, edited by you: ~/.kiro/steering/go.md (you changed it, so it stays; delete it to remove it)\n") {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	if got, _ := os.ReadFile(f); string(got) != "edited\n" {
		t.Errorf("file: %q", got)
	}
}

// After KIRO_HOME moved, the files of the old place lie inside no current root; a folder there
// that became a link to a copy keeps its files.
func TestUninstallMovedHomeLinkedOldFolder(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	steering := filepath.Join(env.Home, ".kiro", "steering")
	ents, err := os.ReadDir(steering)
	if err != nil || len(ents) == 0 {
		t.Fatalf("layout differs: %v %v", ents, err)
	}
	copyDir := filepath.Join(env.Home, "dotfiles", "steering")
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, e := range ents {
		data, err := os.ReadFile(filepath.Join(steering, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		want[e.Name()] = string(data)
		if err := os.WriteFile(filepath.Join(copyDir, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(steering); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(copyDir, steering); err != nil {
		t.Fatal(err)
	}
	env.Getenv = func(k string) string {
		if k == "KIRO_HOME" {
			return filepath.Join(env.Home, ".kiro-new")
		}
		return ""
	}

	code, out, errs := runUninstall(t, env)
	if code != 0 || !strings.Contains(out, "left, edited by you: ~/.kiro/steering/") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for name, data := range want {
		if got, err := os.ReadFile(filepath.Join(copyDir, name)); err != nil || string(got) != data {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	if fi, err := os.Lstat(steering); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link: %v", err)
	}
}
