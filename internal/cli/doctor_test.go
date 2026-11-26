package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

func runDoctor(t *testing.T, env adapter.Env, args ...string) (int, string, string) {
	t.Helper()
	return runCLI(t, env, "", false, append([]string{"doctor"}, args...)...)
}

func installed(t *testing.T, tags ...string) (string, adapter.Env) {
	t.Helper()
	src := enginetest.Source(t, files("skill v1\n"), tags...)
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("update: %d %s%s", code, out, errs)
	}
	return src, env
}

func TestDoctorClean(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	before := tree(t, env.Home)
	code, out, errs := runDoctor(t, env)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(l, "ok       ") {
			t.Errorf("line %q", l)
		}
	}
	if !strings.Contains(out, "ok       update: up to date") {
		t.Errorf("out: %s", out)
	}
	after := tree(t, env.Home)
	cache := paths.CacheDir(env) + string(filepath.Separator)
	for p, v := range before {
		if !strings.HasPrefix(p, cache) && after[p] != v {
			t.Errorf("%s changed", p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok && !strings.HasPrefix(p, cache) {
			t.Errorf("%s created", p)
		}
	}
}

func TestDoctorNoConfig(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	code, out, _ := runDoctor(t, env)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 1 || len(lines) != 2 || !strings.HasPrefix(lines[0], "ok       git:") ||
		!strings.HasPrefix(lines[1], "finding  config:") || !strings.HasSuffix(lines[1], "-> livery setup") {
		t.Errorf("%d %s", code, out)
	}
}

func TestDoctorMissingSource(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	writeConfig(t, env, "/nonexistent/source")
	if err := os.MkdirAll(filepath.Join(env.Home, ".kiro"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env)
	if code != 1 || !strings.Contains(out, "finding  source: ") || !strings.Contains(out, "/nonexistent/source") ||
		strings.Contains(out, "update:") {
		t.Errorf("%d %s", code, out)
	}
	if !strings.Contains(out, "info     lock: nothing installed yet -> livery update") {
		t.Errorf("lock line: %s", out)
	}
}

func TestDoctorHarnessMissing(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	writeConfig(t, env, "/nonexistent/source")
	_, out, _ := runDoctor(t, env)
	if !strings.Contains(out, "finding  harness kiro: ") || !strings.Contains(out, "start kiro once, or livery setup") {
		t.Errorf("%s", out)
	}
}

func TestDoctorEdited(t *testing.T) {
	t.Parallel()
	src, env := installed(t, "v0.1.0")
	skill := filepath.Join(env.Home, ".kiro/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env)
	if code != 1 || !hasAll(out, "finding  edited: ~/.kiro/skills/review/SKILL.md", "delete it", "personal layer", "livery update") {
		t.Errorf("%d %s", code, out)
	}
	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")
	code, out, _ = runDoctor(t, env)
	if code != 1 || !strings.Contains(out, "finding  edited: ") {
		t.Errorf("%d %s", code, out)
	}
}

func TestDoctorDeletedFileIsPending(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	if err := os.Remove(filepath.Join(env.Home, ".kiro/steering/go.md")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env)
	if code != 0 || !strings.Contains(out, "info     update: changes pending -> livery update") {
		t.Errorf("%d %s", code, out)
	}
}

func TestDoctorNewTag(t *testing.T) {
	t.Parallel()
	src, env := installed(t, "v0.1.0")
	enginetest.Commit(t, src, map[string]string{"base/instructions/go.md": "new\n"}, "v0.2.0")
	code, out, _ := runDoctor(t, env)
	if code != 0 || !strings.Contains(out, "info     update: ") {
		t.Errorf("%d %s", code, out)
	}
}

func TestDoctorKeptYours(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	mcp := filepath.Join(env.Home, ".kiro/settings/mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcp, []byte(`{"mcpServers":{"github":{"command":"mine"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env)
	if code != 1 || !strings.Contains(out, "finding  kept, yours: ~/.kiro/settings/mcp.json mcpServers.github -> rename or remove yours, then livery update") {
		t.Errorf("%d %s", code, out)
	}
}

func TestDoctorOverride(t *testing.T) {
	t.Parallel()
	f := files("skill v1\n")
	f["teams/web/skills/review/SKILL.md"] = "team skill\n"
	src := enginetest.Source(t, f, "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src, "web")
	if err := os.MkdirAll(filepath.Join(env.Home, ".kiro"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runDoctor(t, env)
	if code != 0 || strings.Count(out, "info     override: team:web: ") != 1 || !strings.Contains(out, " replaces base: ") {
		t.Errorf("%d %s%s", code, out, errs)
	}
}

func TestDoctorBadLock(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	if err := os.WriteFile(paths.LockFile(env), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env)
	if code != 1 || !strings.Contains(out, "finding  lock: ") || !strings.Contains(out, "then livery update") {
		t.Errorf("%d %s", code, out)
	}
	if strings.Contains(out, "finding  source:") || strings.Contains(out, "install:") {
		t.Errorf("duplicate finding for the bad lock: %s", out)
	}
}

func TestDoctorPersonalLayerNotDirIsNotSource(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	if err := os.MkdirAll(filepath.Dir(paths.PersonalDir(env)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PersonalDir(env), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env)
	if code != 1 || !strings.Contains(out, "finding  install: ") || strings.Contains(out, "check \"source\"") {
		t.Errorf("%d %s", code, out)
	}
}

// newerLock writes a valid lock with "schema": 2 at path and returns its bytes.
func newerLock(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b2 := bytes.Replace(b, []byte("\"schema\": 2"), []byte("\"schema\": 3"), 1)
	if bytes.Equal(b, b2) {
		t.Fatalf("no schema key in %s", b)
	}
	if err := os.WriteFile(path, b2, 0o644); err != nil {
		t.Fatal(err)
	}
	return b2
}

func checkNewerLock(t *testing.T, lockPath string, before []byte, run func(cmd string) (int, string, string)) {
	t.Helper()
	for _, cmd := range []string{"update", "doctor", "uninstall"} {
		code, out, errs := run(cmd)
		all := out + errs
		if code != 1 || !strings.Contains(all, "lock schema 3") || !strings.Contains(all, "upgrade livery") ||
			strings.Contains(all, "remove") || strings.Contains(all, "restore") {
			t.Errorf("%s: exit %d: %s", cmd, code, all)
		}
		after, err := os.ReadFile(lockPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Errorf("%s: lock changed: %v\n%s\n---\n%s", cmd, err, before, after)
		}
	}
}

func TestNewerSchemaLockOnlyUpgradeAdvice(t *testing.T) {
	t.Parallel()
	t.Run("machine", func(t *testing.T) {
		_, env := installed(t, "v0.1.0")
		lockPath := paths.LockFile(env)
		before := newerLock(t, lockPath)
		checkNewerLock(t, lockPath, before, func(cmd string) (int, string, string) {
			switch cmd {
			case "update":
				return runUpdate(t, env)
			case "uninstall":
				return runUninstall(t, env)
			}
			return runDoctor(t, env)
		})
	})
	t.Run("project", func(t *testing.T) {
		src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
		env, proj := projEnv(t)
		machineConfig(t, env, src)
		setupProject(t, env, src)
		lockPath := filepath.Join(proj, ".livery", "lock.json")
		before := newerLock(t, lockPath)
		checkNewerLock(t, lockPath, before, func(cmd string) (int, string, string) {
			switch cmd {
			case "update":
				return runUpdate(t, env, "--project")
			case "uninstall":
				return runUninstall(t, env, "--project")
			}
			return runDoctor(t, env, "--project")
		})
	})
}

// Without git on PATH, update exits 3 naming git and doctor reports a finding for it.
func TestGitMissing(t *testing.T) {
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	t.Setenv("PATH", t.TempDir())

	code, out, errs := runUpdate(t, env)
	if code != 3 || !strings.Contains(out+errs, "git") {
		t.Errorf("update: exit %d, want 3 naming git: %s%s", code, out, errs)
	}
	code, o, e := runDoctor(t, env)
	if code != 1 || !regexp.MustCompile(`(?m)^finding\s+git\b`).MatchString(o) {
		t.Errorf("doctor: exit %d, want 1 with a git finding: %s%s", code, o, e)
	}
}

func TestPersonalFrontmatterErrorNamesFileAndKey(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	f := filepath.Join(paths.PersonalDir(env), "instructions", "testing.md")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("---\ninclusion: always\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runUpdate(t, env)
	if code != 2 || !strings.Contains(errs, "personal/instructions/testing.md:2") ||
		!strings.Contains(errs, `"inclusion"`) || !strings.Contains(errs, "livery update") ||
		strings.HasPrefix(errs, "livery: personal:") {
		t.Errorf("update %d: %s", code, errs)
	}
	code, out, _ := runDoctor(t, env)
	i, j := strings.Index(out, "ok       source:"), strings.Index(out, "finding  personal:")
	if code != 1 || i < 0 || j < 0 || i > j {
		t.Errorf("doctor %d: %s", code, out)
	}
}

func TestDoctorNewReleaseIsAvailable(t *testing.T) {
	t.Parallel()
	src, env := installed(t, "v0.1.0")
	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")
	code, out, _ := runDoctor(t, env)
	if code != 0 || !strings.Contains(out, "ok       installed: v0.1.0 (") ||
		!strings.Contains(out, "update: v0.2.0 is available (installed: v0.1.0) -> livery update") {
		t.Errorf("doctor %d: %s", code, out)
	}
}

func TestDoctorEditedEntrySaysRemoveTheEntry(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	mcp := filepath.Join(env.Home, ".kiro/settings/mcp.json")
	if err := os.WriteFile(mcp, []byte(`{"mcpServers":{"github":{"command":"mine"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runDoctor(t, env)
	if code != 1 || !hasAll(out, "finding  edited: ~/.kiro/settings/mcp.json mcpServers.github", "remove the entry") || strings.Contains(out, "delete it") {
		t.Errorf("%d %s", code, out)
	}
}

func TestDoctorPersonalNativeRenderErrorIsPersonal(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	f := filepath.Join(paths.PersonalDir(env), "kiro", "bogus", "x.md")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != 2 || strings.Contains(errs, "livery: personal:") {
		t.Errorf("update %d: %s%s", code, out, errs)
	}
	code, out, _ = runDoctor(t, env)
	if code != 1 || !strings.Contains(out, "finding  personal: ") || strings.Contains(out, "finding  install") {
		t.Errorf("doctor %d: %s", code, out)
	}
}

func TestDoctorPinnedOlderThanInstalled(t *testing.T) {
	t.Parallel()
	src, env := installed(t, "v0.1.0")
	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("update %d: %s%s", code, out, errs)
	}
	cfg := loadCfg(t, env)
	cfg.Channel = "v0.1.0"
	if err := cfg.Save(paths.ConfigFile(env)); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runDoctor(t, env)
	if !hasAll(out, "update: ", "pinned", "v0.1.0", "v0.2.0", "livery update") || strings.Contains(out, "is available") {
		t.Errorf("%s", out)
	}
}
