package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

// gitLog puts a wrapper named git first in PATH; it logs its arguments and runs the realGit git.
func gitLog(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "git.log")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func countFetches(t *testing.T, log string) int {
	t.Helper()
	data, _ := os.ReadFile(log)
	n := 0
	for _, l := range strings.Split(string(data), "\n") {
		for _, w := range strings.Fields(l) {
			if w == "fetch" {
				n++
				break
			}
		}
	}
	return n
}

func TestSetupAndDoctorFetchOnce(t *testing.T) {
	src := enginetest.Source(t, teamFiles(), "v0.1.0")
	env := enginetest.Env(t)
	withKiro(t, env)
	log := gitLog(t)
	if code, out, errs := runSetup(t, env, "", false, "--source", src); code != 0 {
		t.Fatalf("setup exit %d: %s%s", code, out, errs)
	}
	if n := countFetches(t, log); n != 1 {
		t.Fatalf("setup ran %d git fetch, want 1", n)
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runDoctor(t, env); code != 0 {
		t.Fatalf("doctor exit %d: %s%s", code, out, errs)
	}
	if n := countFetches(t, log); n != 1 {
		t.Fatalf("doctor ran %d git fetch, want 1", n)
	}
}
