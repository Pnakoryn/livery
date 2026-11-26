package cli

import (
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

func unsetFiles() map[string]string {
	f := files("skill v1\n")
	f["base/mcp/docs.json"] = `{"url":"https://docs.example","env":{"TOKEN":"${DOCS_TOKEN}"},"headers":{"Authorization":"Bearer ${DOCS_KEY}"}}`
	f["base/mcp/plain.json"] = `{"command":"plain-mcp","args":["${FALLBACK:-x}"]}`
	return f
}

func linesWith(out, sub string) []string {
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, sub) {
			res = append(res, l)
		}
	}
	return res
}

func TestUnsetVariablesNotes(t *testing.T) {
	t.Parallel()
	const want = "note    mcp docs: DOCS_KEY, DOCS_TOKEN are not set in this shell; set them before you start your coding agent"
	src := enginetest.Source(t, unsetFiles(), "v0.1.0")
	env := enginetest.Env(t)

	code, out, errs := runSetup(t, env, "", false, "--source", src, "--harness", "kiro")
	if code != 0 {
		t.Fatalf("setup: %d %s%s", code, out, errs)
	}
	if got := linesWith(out, "not set in this shell"); len(got) != 1 || got[0] != want {
		t.Errorf("setup notes = %q, want %q\n%s", got, want, out)
	}
	if got := linesWith(out, "mcp plain"); len(got) != 0 {
		t.Errorf("a line for plain: %q", got)
	}

	// Nothing to do: update stays quiet.
	code, out, errs = runUpdate(t, env)
	if code != 0 || strings.Contains(out, "not set in this shell") {
		t.Errorf("update with nothing to do: %d %s%s", code, out, errs)
	}

	// The entry changes: the note is back, also with --plan.
	f := unsetFiles()
	f["base/mcp/docs.json"] = `{"url":"https://docs.example/v2","env":{"TOKEN":"${DOCS_TOKEN}"}}`
	enginetest.Commit(t, src, f, "v0.2.0")
	_, out, _ = runUpdate(t, env, "--plan")
	const one = "note    mcp docs: DOCS_TOKEN is not set in this shell; set it before you start your coding agent"
	if got := linesWith(out, "not set in this shell"); len(got) != 1 || got[0] != one {
		t.Errorf("update --plan notes = %q\n%s", got, out)
	}
	code, out, errs = runUpdate(t, env)
	if code != 0 {
		t.Fatalf("update: %d %s%s", code, out, errs)
	}
	if got := linesWith(out, "not set in this shell"); len(got) != 1 || got[0] != one {
		t.Errorf("update notes = %q\n%s", got, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "info") {
			t.Errorf("info line in update: %q", l)
		}
	}

	code, out, errs = runDoctor(t, env)
	if code != 0 {
		t.Fatalf("doctor: %d %s%s", code, out, errs)
	}
	got := linesWith(out, "mcp docs:")
	if len(got) != 1 || !strings.HasPrefix(got[0], "info     mcp docs:") || !strings.Contains(got[0], " -> set it before") {
		t.Errorf("doctor lines = %q\n%s", got, out)
	}

	env.Getenv = func(k string) string {
		if strings.HasPrefix(k, "DOCS_") {
			return "s3cret-token"
		}
		return ""
	}
	code, out, _ = runDoctor(t, env)
	if code != 0 || len(linesWith(out, "mcp docs:")) != 0 || strings.Contains(out, "s3cret-token") {
		t.Errorf("all set: %d %s", code, out)
	}
}

func TestSetupWithVariablesSetPrintsNoNote(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, unsetFiles(), "v0.1.0")
	env := enginetest.Env(t)
	env.Getenv = func(k string) string {
		if k == "DOCS_TOKEN" || k == "DOCS_KEY" {
			return "s3cret-token"
		}
		return ""
	}
	code, out, errs := runSetup(t, env, "", false, "--source", src, "--harness", "kiro")
	if code != 0 {
		t.Fatalf("setup: %d %s%s", code, out, errs)
	}
	if strings.Contains(out, "not set in this shell") || strings.Contains(out+errs, "s3cret-token") {
		t.Errorf("output:\n%s%s", out, errs)
	}
}
