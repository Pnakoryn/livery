package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
)

// projEnv is an isolated Env whose working directory is a new empty folder below t.TempDir().
func projEnv(t *testing.T) (adapter.Env, string) {
	t.Helper()
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	return env, env.Dir
}

// machineConfig saves a machine config in env's home and returns its content.
func machineConfig(t *testing.T, env adapter.Env, src string) string {
	t.Helper()
	writeConfig(t, env, src)
	b, err := os.ReadFile(paths.ConfigFile(env))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// homeOutsideCache lists the files below env.Home that are not in the cache folder.
func homeOutsideCache(t *testing.T, env adapter.Env) map[string]string {
	t.Helper()
	m := treeExcept(t, paths.CacheDir(env), env.Home)
	delete(m, filepath.Dir(paths.CacheDir(env)))
	return m
}

func setupProject(t *testing.T, env adapter.Env, src string) string {
	t.Helper()
	code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "kiro", "--harness", "claude")
	if code != 0 {
		t.Fatalf("setup --project: exit %d: %s%s", code, out, errs)
	}
	return out
}

func TestSetupProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	cfgBefore := machineConfig(t, env, src)
	homeBefore := homeOutsideCache(t, env)

	out := setupProject(t, env, src)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "project: "+proj {
		t.Errorf("first line %q, want project: %s", lines[0], proj)
	}
	if n := len(lines); n < 2 || lines[n-2] != "commit .livery/ and the files above so your team gets the same setup" || !strings.HasPrefix(lines[n-1], "done    ") {
		t.Errorf("last lines %q", lines[max(0, n-2):])
	}
	for _, p := range []string{".livery/config.json", ".livery/lock.json", ".kiro/skills/review/SKILL.md", ".claude/rules/go.md", ".mcp.json"} {
		if _, err := os.Stat(filepath.Join(proj, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{".livery/config.json", ".livery/lock.json"} {
		b, err := os.ReadFile(filepath.Join(proj, p))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(b), "}\n") {
			t.Errorf("%s has no final newline", p)
		}
	}
	data, err := os.ReadFile(filepath.Join(proj, ".livery/lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("lock is not JSON:\n%s", data)
	}
	l, found, err := lock.ReadIn(filepath.Join(proj, ".livery/lock.json"), lock.Scope{Home: env.Home, Project: proj})
	if err != nil || !found || len(l.Files) == 0 {
		t.Fatalf("lock: found=%v err=%v files=%d", found, err, len(l.Files))
	}
	if strings.Contains(string(data), proj) || strings.Contains(string(data), env.Home) || strings.Contains(string(data), `"~/`) {
		t.Errorf("lock holds absolute paths:\n%s", data)
	}
	if after := homeOutsideCache(t, env); !equal(homeBefore, after) {
		t.Errorf("home changed outside the cache: before %v after %v", homeBefore, after)
	}
	if b, _ := os.ReadFile(paths.ConfigFile(env)); string(b) != cfgBefore {
		t.Errorf("machine config changed: %s", b)
	}

	// Running it again does not create a config, so the commit hint is not printed.
	out = setupProject(t, env, src)
	if strings.Contains(out, "commit .livery/") {
		t.Errorf("second setup printed the commit hint: %s", out)
	}
}

func TestSetupProjectNestedChangesOuter(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	setupProject(t, env, src)
	nested := filepath.Join(proj, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	env.Dir = nested
	code, out, errs := runSetup(t, env, "", false, "--project", "--harness", "kiro")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.HasPrefix(out, "project: "+proj+"\n") {
		t.Errorf("output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(nested, ".livery")); err == nil {
		t.Error("a nested project was created")
	}
	c, _, err := config.Load(filepath.Join(proj, ".livery/config.json"), "livery setup --project")
	if err != nil || len(c.Harnesses) != 1 || c.Harnesses[0] != "kiro" {
		t.Errorf("outer config %+v %v", c, err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".mcp.json")); err == nil {
		t.Error(".mcp.json of claude still there")
	}
}

func TestSetupProjectHarnessDetection(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	withKiro(t, env) // a harness folder in the home does not count for a project
	code, _, errs := runSetup(t, env, "", false, "--project", "--source", src)
	if code != int(exit.Usage) || !strings.Contains(errs, "--harness") {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if _, err := os.Stat(filepath.Join(proj, ".livery")); err == nil {
		t.Error(".livery created on a usage error")
	}

	if err := os.MkdirAll(filepath.Join(proj, ".kiro"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runSetup(t, env, "", false, "--project", "--source", src)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	c, _, err := config.Load(filepath.Join(proj, ".livery/config.json"), "livery setup --project")
	if err != nil || len(c.Harnesses) != 1 || c.Harnesses[0] != "kiro" {
		t.Errorf("config %+v %v", c, err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".kiro/skills/review/SKILL.md")); err != nil {
		t.Error(err)
	}
}

func TestUpdateProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)
	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")

	// update without the flag works on the machine and leaves the project alone.
	projBefore := tree(t, proj)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("machine update: %d %s%s", code, out, errs)
	}
	if !equal(projBefore, tree(t, proj)) {
		t.Error("update without --project changed the project")
	}
	cfgBefore, err := os.ReadFile(paths.ConfigFile(env))
	if err != nil {
		t.Fatal(err)
	}
	machineBefore := homeOutsideCache(t, env)

	code, out, errs := runUpdate(t, env, "--project", "--plan")
	if code != 0 {
		t.Fatalf("plan: %d %s%s", code, out, errs)
	}

	code, out, errs = runUpdate(t, env, "--project")
	if code != 0 {
		t.Fatalf("update --project: %d %s%s", code, out, errs)
	}
	if b, _ := os.ReadFile(filepath.Join(proj, ".kiro/skills/review/SKILL.md")); string(b) != "skill v2\n" {
		t.Errorf("skill: %q", b)
	}
	if !equal(machineBefore, homeOutsideCache(t, env)) {
		t.Error("update --project changed the home outside the cache")
	}
	if b, _ := os.ReadFile(paths.ConfigFile(env)); string(b) != string(cfgBefore) {
		t.Errorf("machine config changed: %s", b)
	}
}

// Outside a project every --project command exits 2 and points to livery setup --project.
func TestProjectCommandsOutsideProject(t *testing.T) {
	t.Parallel()
	env, dir := projEnv(t)
	code, _, errs := runUpdate(t, env, "--project")
	if code != int(exit.Usage) || !strings.Contains(errs, "livery setup --project") || !strings.Contains(errs, dir) {
		t.Errorf("update: exit %d: %s", code, errs)
	}
	for _, args := range [][]string{{"uninstall", "--project"}, {"doctor", "--project"}, {"list", "--project"}} {
		code, _, errs := runCLI(t, env, "", false, args...)
		if code != int(exit.Usage) || !strings.Contains(errs, "livery setup --project") {
			t.Errorf("%v: exit %d: %s", args, code, errs)
		}
	}
}

func TestUninstallProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	cfgBefore := machineConfig(t, env, src)
	setupProject(t, env, src)
	mine := filepath.Join(proj, ".kiro/steering/mine.md")
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	machineBefore := homeOutsideCache(t, env)

	code, out, errs := runUninstall(t, env, "--project")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	for _, p := range []string{".kiro/skills/review/SKILL.md", ".kiro/steering/go.md", ".claude/rules/go.md", ".livery/lock.json"} {
		if _, err := os.Stat(filepath.Join(proj, p)); err == nil {
			t.Errorf("%s still there", p)
		}
	}
	for _, p := range []string{mine, filepath.Join(proj, ".livery/config.json")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if !equal(machineBefore, homeOutsideCache(t, env)) {
		t.Error("uninstall --project changed the home outside the cache")
	}
	if b, _ := os.ReadFile(paths.ConfigFile(env)); string(b) != cfgBefore {
		t.Errorf("machine config changed: %s", b)
	}
}

func TestDoctorProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	cfgBefore := machineConfig(t, env, src)
	setupProject(t, env, src)

	code, out, errs := runDoctor(t, env, "--project")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if first := strings.Split(out, "\n")[0]; first != "project: "+proj {
		t.Errorf("first line %q", first)
	}
	if strings.Contains(out, "harness ") {
		t.Errorf("machine-only harness check ran:\n%s", out)
	}

	skill := filepath.Join(proj, ".kiro/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runDoctor(t, env, "--project")
	if code != int(exit.Finding) || !strings.Contains(out, "edited: .kiro/skills/review/SKILL.md") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if err := os.WriteFile(skill, []byte("skill v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")
	code, out, _ = runDoctor(t, env, "--project")
	if code != 0 || !strings.Contains(out, "info     update: v0.2.0 is available (installed: v0.1.0) -> livery update --project") || strings.Contains(out, "missing") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if b, _ := os.ReadFile(paths.ConfigFile(env)); string(b) != cfgBefore {
		t.Error("machine config changed")
	}
}

func TestListProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env, _ := projEnv(t)
	cfgBefore := machineConfig(t, env, src)
	if code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "kiro", "--team", "web"); code != 0 {
		t.Fatalf("setup: %d %s%s", code, out, errs)
	}
	if b, _ := os.ReadFile(paths.ConfigFile(env)); string(b) != cfgBefore {
		t.Error("machine config changed")
	}
	code, out, errs := runList(t, env, "--project")
	if code != 0 || !strings.Contains(out, "team web *") {
		t.Errorf("list --project: %d %s%s", code, out, errs)
	}
	code, out, _ = runList(t, env)
	if code != 0 || strings.Contains(out, "team web *") {
		t.Errorf("machine list marked the project's team: %s", out)
	}
}

// A hand-edited owned file under update --project names `livery update --project`, and a guided
// setup in a project prints the full diff line with --project.
func checkEditedFileAndFullDiffHints(t *testing.T) {
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)
	edited := filepath.Join(proj, ".claude/rules/go.md")
	if err := os.WriteFile(edited, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env, "--project")
	if code != int(exit.Finding) || !strings.HasSuffix(strings.TrimSpace(errs), `1 edited by you, kept; the rest is applied`) {
		t.Errorf("update: exit %d:\n%s%s", code, out, errs)
	}
	if !hasAll(out, "edited  .claude/rules/go.md", "kept", "delete .claude/rules/go.md", "livery update --project") || strings.Contains(out, "personal") {
		t.Errorf("update: no project edited line:\n%s", out)
	}

	genv := enginetest.Env(t)
	genv = inRepo(t, genv)
	withKiro(t, genv)
	src2 := enginetest.Source(t, teamFiles(), "v0.1.0")
	code, out, errs = runSetup(t, genv, "2\n"+src2+"\n1\n1\ny\n", true)
	if code != 0 || !strings.Contains(out, "full diff: livery setup --project --source "+src2+" --harness kiro --team web --plan\n") {
		t.Errorf("guided setup: exit %d:\n%s%s", code, out, errs)
	}
}

func TestProjectFixHintsKeepScope(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env, proj := projEnv(t)
	if code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "kiro", "--team", "web"); code != 0 {
		t.Fatalf("setup: %d %s%s", code, out, errs)
	}
	// A project config without a source: the no-source errors name --project.
	if err := os.WriteFile(filepath.Join(proj, ".livery/config.json"), []byte(`{"harnesses":["kiro"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list", "--project"}} {
		code, _, errs := runCLI(t, env, "", false, args...)
		if code != int(exit.Usage) || !strings.Contains(errs, "livery setup --project") {
			t.Errorf("%v: exit %d: %s", args, code, errs)
		}
	}
	t.Run("edited file and full diff", checkEditedFileAndFullDiffHints)
}

func TestDoctorProjectMissingFile(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)

	if err := os.Remove(filepath.Join(proj, ".claude/rules/go.md")); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runDoctor(t, env, "--project")
	if code != int(exit.Finding) || !strings.Contains(out, "missing: .claude/rules/go.md -> livery update --project") {
		t.Errorf("exit %d:\n%s%s", code, out, errs)
	}
	if code, out, errs := runUpdate(t, env, "--project"); code != 0 {
		t.Fatalf("update: %d %s%s", code, out, errs)
	}
	if code, out, errs := runDoctor(t, env, "--project"); code != 0 {
		t.Errorf("after update: exit %d:\n%s%s", code, out, errs)
	}
}

func TestDoctorProjectMissingEntry(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)

	if err := os.WriteFile(filepath.Join(proj, ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runDoctor(t, env, "--project")
	if code != int(exit.Finding) || !strings.Contains(out, "missing: .mcp.json mcpServers.github -> livery update --project") {
		t.Errorf("exit %d:\n%s%s", code, out, errs)
	}
}

func TestUpdateProjectOtherVersionKeepsLock(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)
	lockPath := filepath.Join(proj, ".livery", "lock.json")
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var l lock.Lock
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	l.Livery = "v0.0.0-other"
	if err := l.Write(lockPath); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(lockPath)
	if code, out, errs := runUpdate(t, env, "--project"); code != 0 {
		t.Fatalf("update --project: %d %s%s", code, out, errs)
	}
	after, _ := os.ReadFile(lockPath)
	if !bytes.Equal(before, after) {
		t.Errorf("lock rewritten:\n%s\n---\n%s", before, after)
	}
}

// A project lock without the schema key is schema 1: update refuses it and leaves it.
func TestUpdateProjectLockWithoutSchemaKeyRefused(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)
	lockPath := filepath.Join(proj, ".livery", "lock.json")
	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Replace(b, []byte("  \"schema\": 2,\n"), nil, 1)
	if bytes.Equal(before, b) {
		t.Fatalf("no schema key in %s", b)
	}
	if err := os.WriteFile(lockPath, before, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env, "--project")
	if code != 1 || !strings.Contains(errs, "livery setup --project") {
		t.Fatalf("update --project: %d %s%s", code, out, errs)
	}
	if after, _ := os.ReadFile(lockPath); !bytes.Equal(before, after) {
		t.Errorf("lock changed:\n%s", after)
	}
}

func TestProjectRootLineOnUpdateAndUninstall(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	setupProject(t, env, src)

	for name, run := range map[string]func(*testing.T, adapter.Env, ...string) (int, string, string){
		"update":    runUpdate,
		"uninstall": runUninstall,
	} {
		code, out, errs := run(t, env, "--project", "--plan")
		if code != 0 {
			t.Fatalf("%s --project: %d %s%s", name, code, out, errs)
		}
		if first := strings.Split(out, "\n")[0]; first != "project: "+proj {
			t.Errorf("%s --project first line %q, want project: %s", name, first, proj)
		}
		// No machine config: the run may fail, but it prints no project line.
		_, out, _ = run(t, env, "--plan")
		if strings.Contains(out, "project: ") {
			t.Errorf("%s in machine scope prints a project line: %s", name, out)
		}
	}
}

// A locked run under uninstall --project names `livery uninstall --project`.
func TestUninstallProjectRunLockHintKeepsScope(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, _ := projEnv(t)
	machineConfig(t, env, src)
	setupProject(t, env, src)
	penv, err := scoped(env, true, false)
	if err != nil {
		t.Fatal(err)
	}
	release, err := takeRunLock(penv, "update")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	code, out, errs := runUninstall(t, env, "--project")
	if code != int(exit.Environment) || !strings.HasSuffix(strings.TrimSpace(errs), "livery uninstall --project") {
		t.Errorf("uninstall: exit %d:\n%s%s", code, out, errs)
	}
}

func TestUpdateProjectAdoptsAfterLostLock(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	if code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "opencode"); code != 0 {
		t.Fatalf("setup --project: %d %s%s", code, out, errs)
	}
	if err := os.Remove(filepath.Join(proj, ".livery/lock.json")); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env, "--project")
	if code != 0 {
		t.Fatalf("update --project: %d %s%s", code, out, errs)
	}
	var skill, instr, mcp bool
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "adopt ") {
			continue
		}
		if strings.Contains(l, ".opencode/skills/review/SKILL.md") {
			skill = true
		}
		if strings.Contains(l, "instructions[") {
			instr = true
		}
		if strings.Contains(l, "github") {
			mcp = true
		}
	}
	if !skill {
		t.Errorf("no adopt line for .opencode/skills/review/SKILL.md:\n%s", out)
	}
	if !instr {
		t.Errorf("no adopt line with instructions[:\n%s", out)
	}
	if !mcp {
		t.Errorf("no adopt line for the MCP entry:\n%s", out)
	}
	if strings.Contains(out, "kept, yours") {
		t.Errorf("kept, yours in:\n%s", out)
	}
	if code, out, errs := runDoctor(t, env, "--project"); code != 0 {
		t.Errorf("doctor --project: %d %s%s", code, out, errs)
	}
}

func TestProjectDoctorBadLockHint(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env, proj := projEnv(t)
	setupProject(t, env, src)
	if err := os.WriteFile(filepath.Join(proj, ".livery/lock.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, errs := runDoctor(t, env, "--project")
	if all := out + errs; !strings.Contains(all, "restore") || !strings.Contains(all, "from git") {
		t.Errorf("project hint: %s", all)
	}

	menv := enginetest.Env(t)
	machineConfig(t, menv, src)
	if err := os.MkdirAll(filepath.Dir(paths.LockFile(menv)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LockFile(menv), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, errs = runDoctor(t, menv)
	if all := out + errs; strings.Contains(all, "from git") || !strings.Contains(all, "remove "+paths.LockFile(menv)+", then livery update") {
		t.Errorf("machine hint: %s", all)
	}
}

// A layer with a files/ folder is a folder that is no harness: setup exits 2
// naming it.
func TestSetupFilesFolderIsNoHarness(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, map[string]string{
		"livery.json":                 `{"schema":1}`,
		"base/skills/review/SKILL.md": "skill v1\n",
		"base/files/docs/a.md":        "a\n",
	}, "v0.1.0")
	env, proj := projEnv(t)
	code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--channel", "stable", "--harness", "claude")
	if code != int(exit.Usage) || !strings.Contains(errs, `"files"`) {
		t.Fatalf("setup --project: exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(proj, "docs", "a.md")); err == nil {
		t.Errorf("docs/a.md written")
	}
}
