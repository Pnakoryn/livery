package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

const credAddr = "https://s3cr3tTok@host.invalid/x.git"

// setup refuses a source address with credentials: exit 2, nothing saved, the address not printed.
func TestSetupRefusesCredentials(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	withKiro(t, env)
	code, out, errs := runSetup(t, env, "", false, "--source", credAddr, "--harness", "kiro")
	if code != int(exit.Usage) || strings.Contains(out+errs, "s3cr3tTok") || !strings.Contains(errs, "credential helper") ||
		!strings.Contains(errs, "then run livery setup --source") {
		t.Fatalf("exit %d:\nstdout: %s\nstderr: %s", code, out, errs)
	}
	for _, p := range []string{paths.ConfigFile(env), paths.LockFile(env)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s exists", p)
		}
	}
	if files := fileData(tree(t, env.Home)); len(files) != 0 {
		t.Errorf("files written: %v", files)
	}
}

func TestSetupProjectRefusesCredentials(t *testing.T) {
	t.Parallel()
	env, proj := projEnv(t)
	code, out, errs := runSetup(t, env, "", false, "--project", "--source", credAddr, "--harness", "kiro")
	if code != int(exit.Usage) || strings.Contains(out+errs, "s3cr3tTok") || !strings.Contains(errs, "credential helper") ||
		!strings.Contains(errs, "livery setup --project --source") {
		t.Fatalf("exit %d:\nstdout: %s\nstderr: %s", code, out, errs)
	}
	for _, p := range []string{filepath.Join(proj, ".livery", "config.json"), filepath.Join(proj, ".livery", "lock.json")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s exists", p)
		}
	}
	if files := fileData(tree(t, env.Home)); len(files) != 0 {
		t.Errorf("files written in home: %v", files)
	}
}

// A hand-edited config with credentials in "source": update exits 2, doctor reports a finding;
// neither prints the address.
func TestConfigWithCredentials(t *testing.T) {
	t.Parallel()
	env := enginetest.Env(t)
	withKiro(t, env)
	cfgPath := paths.ConfigFile(env)
	c := config.Config{Source: "https://u:s3cr3tTok@host.invalid/x.git", Channel: "stable", Teams: []string{}, Harnesses: []string{"kiro"}}
	if err := c.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != int(exit.Usage) || strings.Contains(out+errs, "s3cr3tTok") || !strings.Contains(errs, cfgPath) ||
		!strings.Contains(errs, "credential helper") {
		t.Errorf("update: exit %d:\nstdout: %s\nstderr: %s", code, out, errs)
	}
	code, out, errs = runDoctor(t, env)
	if code != int(exit.Finding) || strings.Contains(out+errs, "s3cr3tTok") || !strings.Contains(out, "finding  config: ") ||
		!strings.Contains(out, "credential helper") {
		t.Errorf("doctor: exit %d:\nstdout: %s\nstderr: %s", code, out, errs)
	}
	if _, err := os.Stat(paths.LockFile(env)); err == nil {
		t.Error("lock written")
	}
}
