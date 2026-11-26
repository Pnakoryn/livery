package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/paths"
)

// schema1Lock rewrites the lock of env with a lower schema number: schema 1 with one source.
func schema1Lock(t *testing.T, env adapter.Env) []byte {
	t.Helper()
	l := readLock(t, env)
	sources := []map[string]any{{
		"name": "setup", "source": l.Source, "channel": l.Channel, "tag": l.Tag, "commit": l.Commit, "teams": []string{},
	}}
	old := map[string]any{
		"schema": 1, "livery": "0.0.1", "sources": sources,
		"harnesses": l.Harnesses, "files": l.Files, "merged": l.Merged,
	}
	b, err := json.MarshalIndent(old, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LockFile(env), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

// A schema 1 lock stops every command with exit 1 and names the lock and livery setup; the lock stays.
func TestSchema1LockRefused(t *testing.T) {
	t.Parallel()
	_, env := installed(t, "v0.1.0")
	before := schema1Lock(t, env)
	for name, run := range map[string]func(*testing.T, adapter.Env, ...string) (int, string, string){
		"update": runUpdate, "uninstall": runUninstall,
	} {
		code, out, errs := run(t, env)
		if code != 1 || !strings.Contains(errs, paths.LockFile(env)) || !strings.Contains(errs, "livery setup") {
			t.Errorf("%s: exit %d\nstdout: %s\nstderr: %s", name, code, out, errs)
		}
		if after, _ := os.ReadFile(paths.LockFile(env)); !bytes.Equal(before, after) {
			t.Errorf("%s changed the lock:\n%s", name, after)
		}
	}
	code, out, errs := runDoctor(t, env)
	found := false
	for _, l := range strings.Split(out+errs, "\n") {
		if strings.HasPrefix(l, "finding") && strings.Contains(l, "lock") && strings.Contains(l, "older livery") &&
			strings.Contains(l, "remove "+paths.LockFile(env)+", then livery update") {
			found = true
		}
	}
	if code != 1 || !found {
		t.Errorf("doctor: exit %d\n%s%s", code, out, errs)
	}
}
