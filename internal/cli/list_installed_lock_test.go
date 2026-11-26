package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/paths"
)

// itemLines are the lines of a list --installed output without the header and the note.
func itemLines(out string) string {
	lines := strings.Split(out, "\n")
	var keep []string
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "note ") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

func hasSkillNew(out string) bool {
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "skill" && f[1] == "new" {
			return true
		}
	}
	return false
}

// list --installed lists the lock's release from the cache: no fetch, offline, not the config.
func TestListInstalledListsTheLocksRelease(t *testing.T) {
	t.Parallel()
	env, src := installedSetup(t)
	enginetest.Commit(t, src, map[string]string{
		"base/skills/review/SKILL.md": "skill v2\n",
		"base/skills/new/SKILL.md":    "new skill\n",
	}, "v0.2.0")

	code, out, errs := runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "v0.1.0") {
		t.Errorf("header %q", first)
	}
	if hasSkillNew(out) {
		t.Errorf("skill of the newer release listed:\n%s", out)
	}
	l := lineWith(out, "skill", "review")
	if l == "" || strings.HasSuffix(l, "pending") || strings.HasSuffix(l, "edited") || strings.HasSuffix(l, "missing") {
		t.Errorf("review line %q in\n%s", l, out)
	}
	if strings.Contains(out, "is available") {
		t.Errorf("note without a fetch:\n%s", out)
	}
	items := itemLines(out)

	// update --plan fetches: the note appears, the items stay those of v0.1.0.
	if code, o, e := runUpdate(t, env, "--plan"); code != 0 {
		t.Fatalf("update --plan %d: %s%s", code, o, e)
	}
	code, out, errs = runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("after fetch: exit %d: %s%s", code, out, errs)
	}
	if !strings.Contains(out, "note    v0.2.0 is available: livery update\n") {
		t.Errorf("no note:\n%s", out)
	}
	if got := itemLines(out); got != items {
		t.Errorf("items changed after the fetch:\n%s\nwant\n%s", got, items)
	}

	// The source unreachable: still the same list.
	if err := os.Rename(src, src+".gone"); err != nil {
		t.Fatal(err)
	}
	code, out, errs = runList(t, env, "--installed")
	if code != 0 || itemLines(out) != items {
		t.Fatalf("offline: exit %d: %s%s", code, out, errs)
	}

	// The config changed by hand to another team: the lock's team is listed.
	writeConfig(t, env, src, "other")
	code, out, errs = runList(t, env, "--installed")
	if code != 0 || itemLines(out) != items || lineWith(out, "instruction", "commits", "team web") == "" {
		t.Fatalf("config changed: exit %d: %s%s", code, out, errs)
	}
	// No config at all is fine as well.
	if err := os.Remove(paths.ConfigFile(env)); err != nil {
		t.Fatal(err)
	}
	if code, out, errs = runList(t, env, "--installed"); code != 0 || itemLines(out) != items {
		t.Fatalf("no config: exit %d: %s%s", code, out, errs)
	}
}

func TestListInstalledMissingFile(t *testing.T) {
	t.Parallel()
	env, _ := installedSetup(t)
	ins := installedFile(t, env, "commits")
	if err := os.Remove(ins); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if l := lineWith(out, "instruction", "commits"); !strings.HasSuffix(l, "missing") {
		t.Errorf("instruction line %q in\n%s", l, out)
	}
	if l := lineWith(out, "skill", "review"); strings.HasSuffix(l, "missing") || l == "" {
		t.Errorf("review line %q", l)
	}
}

func TestListInstalledPersonalPending(t *testing.T) {
	t.Parallel()
	env, _ := installedSetup(t)
	pd := paths.PersonalDir(env)
	if err := os.WriteFile(filepath.Join(pd, "mcp", "notes.json"), []byte(`{"command":"notes-mcp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runList(t, env, "--installed")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if l := lineWith(out, "mcp", "notes", "personal"); !strings.HasSuffix(l, "pending") {
		t.Errorf("personal line %q in\n%s", l, out)
	}
}

func TestListInstalledCacheRemoved(t *testing.T) {
	t.Parallel()
	env, _ := installedSetup(t)
	if err := os.RemoveAll(paths.CacheDir(env)); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runList(t, env, "--installed")
	if code != 3 || !strings.Contains(errs, "run livery update, then livery list --installed") {
		t.Errorf("exit %d: %s%s", code, out, errs)
	}
	if _, err := os.Stat(paths.CacheDir(env)); err == nil {
		t.Error("list --installed created the cache")
	}
}

func TestListInstalledProjectCacheRemoved(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env, _ := projEnv(t)
	if code, out, errs := runSetup(t, env, "", false, "--project", "--source", src, "--harness", "kiro", "--team", "web"); code != 0 {
		t.Fatalf("setup: %d %s%s", code, out, errs)
	}
	if err := os.RemoveAll(paths.CacheDir(env)); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runList(t, env, "--installed", "--project")
	if code != 3 || !strings.Contains(errs, "run livery update --project, then livery list --installed --project") {
		t.Errorf("exit %d: %s%s", code, out, errs)
	}
}
