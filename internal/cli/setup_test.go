package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

func runSetup(t *testing.T, env adapter.Env, stdin string, interactive bool, args ...string) (int, string, string) {
	t.Helper()
	return runCLI(t, env, stdin, interactive, append([]string{"setup"}, args...)...)
}

func teamFiles() map[string]string {
	f := files("skill v1\n")
	f["teams/web/skills/ui/SKILL.md"] = "ui skill\n"
	return f
}

// inRepo makes env.Dir a folder inside a git repository, so setup asks the scope question.
func inRepo(t *testing.T, env adapter.Env) adapter.Env {
	t.Helper()
	env.Dir = t.TempDir()
	if err := os.Mkdir(filepath.Join(env.Dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return env
}

func withKiro(t *testing.T, env adapter.Env) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(env.Home, ".kiro"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func loadCfg(t *testing.T, env adapter.Env) config.Config {
	t.Helper()
	c, found, err := config.Load(paths.ConfigFile(env), "livery setup")
	if err != nil || !found {
		t.Fatalf("config: found=%v err=%v", found, err)
	}
	return c
}

func TestSetupNonInteractive(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", false, "--source", src)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	c := loadCfg(t, env)
	if c.Channel != "stable" || len(c.Teams) != 0 || len(c.Harnesses) != 1 || c.Harnesses[0] != "kiro" || c.Source != src {
		t.Fatalf("config %+v", c)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".kiro", "skills", "review", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestSetupUsageErrors(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	cases := []struct {
		name  string
		kiro  bool
		args  []string
		wants []string
	}{
		{"no source", true, nil, []string{"--source"}},
		{"no harness folder", false, []string{"--source", src}, []string{"--harness", "kiro"}},
		{"unknown team", true, []string{"--source", src, "--team", "nope"}, []string{"nope", "web"}},
		{"unknown harness", true, []string{"--source", src, "--harness", "cursor"}, []string{"kiro"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := enginetest.Env(t)
			if c.kiro {
				withKiro(t, env)
			}
			code, _, errs := runSetup(t, env, "", false, c.args...)
			if code != 2 {
				t.Fatalf("exit %d: %s", code, errs)
			}
			for _, w := range c.wants {
				if !strings.Contains(errs, w) {
					t.Errorf("%q missing %q", errs, w)
				}
			}
		})
	}
}

func TestSetupNoScopeQuestionOutsideRepo(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	code, out, errs := runSetup(t, env, src+"\n\n\ny\n", true)
	if code != 0 || strings.Contains(out, "Scope (") ||
		!strings.Contains(out, "Address of your team's shared repository") || !strings.Contains(out, "Coding agents to set up") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, `--team "" --plan`+"\n") {
		t.Errorf("no empty --team in the hint:\n%s", out)
	}
}

func TestSetupScopeQuestionFirstInRepo(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := inRepo(t, enginetest.Env(t))
	withKiro(t, env)
	sub := filepath.Join(env.Dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	env.Dir = sub
	code, out, errs := runSetup(t, env, "1\n"+src+"\n\n1\ny\n", true)
	if code != 0 || !strings.HasPrefix(out, "Set up: 1. this machine") || !strings.Contains(out, "Scope (1 or 2) [1]: ") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}

func TestSetupScopeInvalidTwice(t *testing.T) {
	t.Parallel()
	env := inRepo(t, enginetest.Env(t))
	code, _, errs := runSetup(t, env, "3\n3\n", true)
	if code != 2 || !strings.Contains(errs, "answer 1 for this machine or 2 for the project") || strings.Contains(errs, "pass --project") {
		t.Errorf("exit %d: %s", code, errs)
	}
}

func TestSetupHintRunsWithoutTerminal(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	_, out, _ := runSetup(t, env, src+"\n\n1\nn\n", true)
	var hint string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "full diff: livery setup ") {
			hint = strings.TrimPrefix(l, "full diff: livery setup ")
		}
	}
	if hint == "" {
		t.Fatalf("no hint:\n%s", out)
	}
	code, out2, errs := runSetup(t, env, "", false, strings.Fields(hint)...)
	if code != 0 {
		t.Fatalf("hint %q: exit %d: %s%s", hint, code, out2, errs)
	}
}

func TestArgQuote(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"a/b-c.d": "a/b-c.d", "a b": "'a b'", "it's": `'it'\''s'`, "": "''"} {
		if got := argQuote(in); got != want {
			t.Errorf("argQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetupInteractiveInvalidTwice(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env = inRepo(t, env)
	withKiro(t, env)
	code, _, errs := runSetup(t, env, "\n\n\n", true)
	if code != 2 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	code, _, errs = runSetup(t, env, "\nx\n9\n", true, "--source", src)
	if code != 2 {
		t.Fatalf("exit %d: %s", code, errs)
	}
}

func TestSetupPlanPrintsDiff(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", false, "--source", src, "--plan")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "\n--- now/") || !strings.Contains(out, "@@") {
		t.Errorf("setup --plan prints no diff:\n%s", out)
	}
	saved := false
	for _, l := range strings.Split(out, "\n") {
		if l == "{" {
			t.Errorf("setup --plan prints the config over several lines:\n%s", out)
		}
		if strings.HasPrefix(l, "config  would save ") && strings.Contains(l, `"source":`) && strings.Contains(l, `"harnesses":`) {
			saved = true
		}
	}
	if !saved {
		t.Errorf("no line `config  would save ...` with the source:\n%s", out)
	}
}

func TestSetupChannelPlanAndKeep(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	if code, out, errs := runSetup(t, env, "", false, "--source", src, "--team", "web"); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	enginetest.Commit(t, src, map[string]string{"base/instructions/new.md": "new\n"}, "")
	code, out, errs := runSetup(t, env, "", false, "--channel", "next", "--plan")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "livery setup: "+src+" next (") {
		t.Errorf("no header with the next channel: %s", out)
	}
	if !regexp.MustCompile(`(?m)^write   .*new\.md$`).MatchString(out) {
		t.Errorf("no pending write line for new.md: %s", out)
	}
	if loadCfg(t, env).Channel != "stable" {
		t.Error("plan changed the config")
	}
	if code, out, errs := runSetup(t, env, "", false, "--channel", "next"); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	c := loadCfg(t, env)
	if c.Channel != "next" || c.Source != src || len(c.Teams) != 1 || c.Teams[0] != "web" || c.Harnesses[0] != "kiro" {
		t.Fatalf("config %+v", c)
	}
}

func TestSetupFlagErrors(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--source", src, "--harness", ""}, "--harness"},
		{[]string{"--source", src, "--harness", "nope"}, "--harness"},
		{[]string{"--source", src, "--channel", "nightly"}, "--channel"},
		{[]string{"--source", src, "--team", "Bad_Team"}, "--team"},
	}
	for _, tc := range tests {
		code, _, errs := runSetup(t, env, "", false, tc.args...)
		if code != 2 || !strings.Contains(errs, tc.want) || strings.Contains(errs, "config.json") {
			t.Errorf("%v: exit %d: %s", tc.args, code, errs)
		}
	}
}

func TestSetupPromptErrorsNameFlag(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	code, _, errs := runSetup(t, env, "1\n", true)
	if code != 2 || !strings.Contains(errs, "--source") {
		t.Errorf("EOF: exit %d: %s", code, errs)
	}
	code, _, errs = runSetup(t, env, "\n\n", true)
	if code != 2 || !strings.Contains(errs, "--source") {
		t.Errorf("invalid: exit %d: %s", code, errs)
	}
}

func collidingFiles() map[string]string {
	f := files("skill v1\n")
	f["teams/a/skills/x/SKILL.md"] = "from a\n"
	f["teams/b/skills/x/SKILL.md"] = "from b\n"
	return f
}

func TestSetupRejectedKeepsConfig(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, collidingFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	if code, out, errs := runSetup(t, env, "", false, "--source", src); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	cfgBefore, _ := os.ReadFile(paths.ConfigFile(env))
	lockBefore, _ := os.ReadFile(paths.LockFile(env))
	kiroBefore := tree(t, filepath.Join(env.Home, ".kiro"))

	code, out, errs := runSetup(t, env, "", false, "--team", "a", "--team", "b")
	if code != 1 {
		t.Fatalf("collision: exit %d: %s%s", code, out, errs)
	}
	cfgAfter, _ := os.ReadFile(paths.ConfigFile(env))
	lockAfter, _ := os.ReadFile(paths.LockFile(env))
	if string(cfgAfter) != string(cfgBefore) || string(lockAfter) != string(lockBefore) {
		t.Error("config or lock changed by a rejected setup")
	}
	if !equal(kiroBefore, tree(t, filepath.Join(env.Home, ".kiro"))) {
		t.Error("installed files changed")
	}

	// an edited owned file is skipped; the channel change is still saved and applied
	skill := filepath.Join(env.Home, ".kiro/skills/review/SKILL.md")
	if err := os.WriteFile(skill, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "")
	code, out, errs = runSetup(t, env, "", false, "--channel", "next")
	if code != 1 {
		t.Fatalf("edited: exit %d: %s%s", code, out, errs)
	}
	if b, _ := os.ReadFile(paths.ConfigFile(env)); !strings.Contains(string(b), `"channel": "next"`) {
		t.Errorf("config not saved: %s", b)
	}
	if b, _ := os.ReadFile(skill); string(b) != "mine\n" {
		t.Errorf("edited skill: %q", b)
	}
	if !strings.Contains(out, "edited  ~/.kiro/skills/review/SKILL.md: you changed it;") || !strings.Contains(errs, "1 edited by you, kept") {
		t.Errorf("output: %s%s", out, errs)
	}
}

func TestSetupRelativeSourceIsSavedAbsolute(t *testing.T) {
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	t.Chdir(filepath.Dir(src))
	rel := "./" + filepath.Base(src)

	code, out, errs := runSetup(t, env, "", false, "--source", rel, "--plan")
	if code != 0 {
		t.Fatalf("plan: exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, `"source":"`+src+`"`) {
		t.Errorf("plan does not show the absolute source: %s", out)
	}
	if _, err := os.Stat(paths.ConfigFile(env)); !os.IsNotExist(err) {
		t.Errorf("plan wrote the config: %v", err)
	}

	if code, out, errs := runSetup(t, env, "", false, "--source", rel); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if c := loadCfg(t, env); c.Source != src {
		t.Fatalf("saved source %q, want %q", c.Source, src)
	}
	t.Chdir(t.TempDir())
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("update elsewhere: exit %d: %s%s", code, out, errs)
	}
}

func TestSetupGuidedMachine(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env = inRepo(t, env)
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "1\n"+src+"\n\n1\ny\n", true)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	i1 := strings.Index(out, "Address of your team's shared repository (git URL or folder): ")
	i2 := strings.Index(out, "Coding agents to set up (numbers, comma-separated) [1]: ")
	i3 := strings.Index(out, "Teams to add to the base setup (numbers, comma-separated, empty for none): ")
	if i1 < 0 || i2 < i1 || i3 < i2 {
		t.Fatalf("prompts out of order:\n%s", out)
	}
	c := loadCfg(t, env)
	if c.Source != src || len(c.Harnesses) != 1 || c.Harnesses[0] != "kiro" || len(c.Teams) != 1 || c.Teams[0] != "web" {
		t.Fatalf("config %+v", c)
	}
	if _, err := os.Stat(paths.LockFile(env)); err != nil {
		t.Fatal(err)
	}
	a, w := strings.Index(out, "Apply? [Y/n] "), strings.Index(out, "write   ")
	if a < 0 || w < 0 || w > a {
		t.Errorf("plan lines not before Apply?:\n%s", out)
	}
	before := out[:a]
	if !strings.Contains(before, "full diff: livery setup --source "+src+" --harness kiro --team web --plan\n") {
		t.Errorf("no hint line before Apply?:\n%s", out)
	}
	if strings.Contains(before, "--- now/") || strings.Contains(before, "@@") {
		t.Errorf("diff shown before Apply?:\n%s", out)
	}
}

func TestSetupGuidedProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env = inRepo(t, env)
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "2\n"+src+"\n1\n1\ny\n", true)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(env.Dir, ".livery", "config.json")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(paths.ConfigFile(env)); !os.IsNotExist(err) {
		t.Errorf("machine config written: %v", err)
	}
	if !strings.Contains(out, "project: ") || !strings.Contains(out, "commit .livery/") {
		t.Errorf("output:\n%s", out)
	}
}

func TestSetupGuidedDecline(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env = inRepo(t, env)
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "1\n"+src+"\n\n1\nn\n", true)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "nothing written") {
		t.Errorf("output:\n%s", out)
	}
	if _, err := os.Stat(paths.ConfigFile(env)); !os.IsNotExist(err) {
		t.Errorf("config written: %v", err)
	}
	if _, err := os.Stat(paths.LockFile(env)); !os.IsNotExist(err) {
		t.Errorf("lock written: %v", err)
	}
	if got := fileData(tree(t, filepath.Join(env.Home, ".kiro"))); len(got) != 0 {
		t.Errorf("files written: %v", got)
	}
}

func TestSetupNoScopeQuestionWithConfig(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	if code, out, errs := runSetup(t, env, "", false, "--source", src); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	code, out, errs := runSetup(t, env, "", true)
	if code != 0 || strings.Contains(out, "Scope (") || strings.Contains(out, "Apply?") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}

func TestSetupAllFlagsAsksNothing(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", true, "--source", src, "--harness", "kiro", "--team", "web")
	if code != 0 || strings.Contains(out, "Apply?") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	loadCfg(t, env)
}

func TestSetupHarnessQuestionOnlyConfirms(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	code, out, errs := runSetup(t, env, "1\ny\n", true, "--project", "--source", src, "--team", "web")
	if code != 0 || !strings.Contains(out, "Apply? [Y/n] ") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}

func TestSetupHintInsideProject(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env.Dir = t.TempDir()
	withKiro(t, env)
	if err := os.MkdirAll(filepath.Join(env.Dir, ".livery"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, ".livery", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runSetup(t, env, "", false, "--source", src)
	if code != 0 || strings.Count(out, "note    this folder is a livery project (") != 1 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
}
