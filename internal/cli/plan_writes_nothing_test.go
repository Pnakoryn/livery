package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

var planChange = regexp.MustCompile(`(?m)^(write|remove|set|unset)  `)

// One row per command and scope that takes --plan: in a state where the command has something to
// change, --plan exits 0, shows a change and writes nothing outside the cache folder.
func TestPlanWritesNothing(t *testing.T) {
	t.Parallel()
	// ready sets up a state with something to change, and returns the folders to compare and the
	// command to run.
	type row struct {
		name  string
		ready func(t *testing.T) (env adapter.Env, roots []string, run func(adapter.Env) (int, string, string))
	}
	machine := func(t *testing.T) (adapter.Env, string) {
		env := enginetest.Env(t)
		return env, enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	}
	rows := []row{
		{"setup", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			env, src := machine(t)
			withKiro(t, env)
			return env, []string{env.Home}, func(e adapter.Env) (int, string, string) {
				return runSetup(t, e, "", false, "--source", src, "--harness", "kiro", "--plan")
			}
		}},
		{"setup --project", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			env, proj := projEnv(t)
			src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
			return env, []string{env.Home, proj}, func(e adapter.Env) (int, string, string) {
				return runSetup(t, e, "", false, "--project", "--source", src, "--harness", "kiro", "--plan")
			}
		}},
		{"update", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			env, src := machine(t)
			writeConfig(t, env, src)
			return env, []string{env.Home}, func(e adapter.Env) (int, string, string) {
				return runUpdate(t, e, "--plan")
			}
		}},
		{"update --project", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			env, proj := projEnv(t)
			src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
			setupProject(t, env, src)
			enginetest.Commit(t, src, map[string]string{"base/skills/review/SKILL.md": "skill v2\n"}, "v0.2.0")
			return env, []string{env.Home, proj}, func(e adapter.Env) (int, string, string) {
				return runUpdate(t, e, "--project", "--plan")
			}
		}},
		{"uninstall", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			_, env := installed(t, "v0.1.0")
			return env, []string{env.Home}, func(e adapter.Env) (int, string, string) {
				return runUninstall(t, e, "--plan")
			}
		}},
		{"uninstall --project", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			env, proj := projEnv(t)
			src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
			setupProject(t, env, src)
			return env, []string{env.Home, proj}, func(e adapter.Env) (int, string, string) {
				return runUninstall(t, e, "--project", "--plan")
			}
		}},
		{"import", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			env := enginetest.Env(t)
			kiroFolder(t, env, "")
			out := t.TempDir()
			return env, []string{env.Home, out}, func(e adapter.Env) (int, string, string) {
				return runImport(t, e, "", false, "--from", "kiro", "--into", filepath.Join(out, "src"), "--plan")
			}
		}},
		{"import --project", func(t *testing.T) (adapter.Env, []string, func(adapter.Env) (int, string, string)) {
			env, proj := projEnv(t)
			writeFiles(t, filepath.Join(proj, ".kiro"), map[string]string{"skills/review/SKILL.md": "review skill\n"})
			out := t.TempDir()
			return env, []string{env.Home, proj, out}, func(e adapter.Env) (int, string, string) {
				return runImport(t, e, "", false, "--project", "--from", "kiro", "--into", filepath.Join(out, "src"), "--plan")
			}
		}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			env, roots, run := r.ready(t)
			cache := paths.CacheDir(env)
			if err := os.MkdirAll(cache, 0o755); err != nil {
				t.Fatal(err)
			}
			before := treeExcept(t, cache, roots...)
			code, out, errs := run(env)
			if code != 0 || !planChange.MatchString(out) {
				t.Fatalf("exit %d, want 0 and a write, remove, set or unset line:\n%s%s", code, out, errs)
			}
			if after := treeExcept(t, cache, roots...); !equal(before, after) {
				t.Errorf("--plan changed the files:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}
