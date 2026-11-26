package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

const sourcesConfig = `{"sources":[{"name":"a","source":"x","channel":"stable","teams":[]}],"harnesses":["kiro"]}`

func TestSetupWritesFlatConfig(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	if code, out, errs := runSetup(t, env, "", false, "--source", src, "--harness", "kiro"); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	data, err := os.ReadFile(paths.ConfigFile(env))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"channel", "harnesses", "source", "teams"}) {
		t.Errorf("keys %v:\n%s", keys, data)
	}
}

func TestUpdateRefusesSourcesConfig(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	cfg := paths.ConfigFile(env)
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(sourcesConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runUpdate(t, env)
	if code != int(exit.Usage) || !strings.Contains(errs, cfg) {
		t.Errorf("exit %d: %s", code, errs)
	}

	penv, proj := projEnv(t)
	pcfg := filepath.Join(proj, ".livery", "config.json")
	if err := os.MkdirAll(filepath.Dir(pcfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pcfg, []byte(sourcesConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs = runUpdate(t, penv, "--project")
	if code != int(exit.Usage) || !strings.Contains(errs, "livery setup --project") {
		t.Errorf("--project: exit %d: %s", code, errs)
	}
}

// setup does not start from a config with "sources": it exits 2 and leaves the file as it is.
func TestSetupRefusesSourcesConfig(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	cfg := paths.ConfigFile(env)
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(sourcesConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runSetup(t, env, "", false, "--source", src, "--harness", "kiro")
	if code != int(exit.Usage) || !strings.Contains(errs, cfg) {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if got, _ := os.ReadFile(cfg); string(got) != sourcesConfig {
		t.Errorf("config changed:\n%s", got)
	}
}

func TestSourceCommandIsUnknown(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	code, _, errs := runCLI(t, env, "", false, "source", "list")
	if code != int(exit.Usage) || !strings.Contains(errs, `unknown command "source"`) {
		t.Errorf("exit %d: %s", code, errs)
	}
	code, out, _ := runCLI(t, env, "", false, "help")
	if code != 0 {
		t.Fatalf("help: exit %d", code)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "source ") {
			t.Errorf("help lists the source command: %q", l)
		}
	}
}
