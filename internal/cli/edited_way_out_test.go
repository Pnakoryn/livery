package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine/enginetest"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

// editedInstruction installs files(), edits the installed instruction and returns its path.
func editedInstruction(t *testing.T) (adapter.Env, string) {
	t.Helper()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	ins := filepath.Join(env.Home, ".kiro/steering/go.md")
	b, err := os.ReadFile(ins)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ins, append(b, "my own rule\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	return env, ins
}

// editedBlock returns the line starting with "edited  " and the lines after it; it fails
// unless there is exactly one such line and no "edited by you:" line.
func editedBlock(t *testing.T, out string) []string {
	t.Helper()
	lines := strings.Split(out, "\n")
	at, n := -1, 0
	for i, l := range lines {
		if strings.HasPrefix(l, "edited  ") {
			at, n = i, n+1
		}
		if strings.HasPrefix(l, "edited by you:") {
			t.Errorf("edited by you: line in an applying run:\n%s", out)
		}
	}
	if n != 1 {
		t.Fatalf("%d edited lines, want 1:\n%s", n, out)
	}
	return lines[at:]
}

func TestEditedInstructionTakeTheTeamsVersion(t *testing.T) {
	t.Parallel()
	env, ins := editedInstruction(t)
	code, out, errs := runUpdate(t, env)
	if code != int(exit.Finding) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	block := editedBlock(t, out)
	if len(block) < 3 || !strings.Contains(block[1], "to take the team's version: delete ") ||
		!strings.Contains(block[2], "the only frontmatter keys are description and globs") {
		t.Errorf("block:\n%s", strings.Join(block, "\n"))
	}
	if strings.Contains(out, "up to date") {
		t.Errorf("up to date with an edited item:\n%s", out)
	}
	if err := os.Remove(ins); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("after delete: exit %d: %s%s", code, out, errs)
	}
	if b, _ := os.ReadFile(ins); !strings.Contains(string(b), "go rules") || strings.Contains(string(b), "my own rule") {
		t.Errorf("installed: %q", b)
	}
}

func TestEditedInstructionKeepYourChange(t *testing.T) {
	t.Parallel()
	env, ins := editedInstruction(t)
	code, out, errs := runUpdate(t, env)
	if code != int(exit.Finding) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	block := editedBlock(t, out)
	personal := filepath.Join(paths.PersonalDir(env), "instructions", "go.md")
	if len(block) < 3 || !strings.Contains(block[2], "put your text into ~/.config/livery/personal/instructions/go.md ") {
		t.Fatalf("block:\n%s", strings.Join(block, "\n"))
	}
	if err := os.MkdirAll(filepath.Dir(personal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personal, []byte("go rules\nmy own rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(ins); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("after keep: exit %d: %s%s", code, out, errs)
	}
	if b, _ := os.ReadFile(ins); !strings.Contains(string(b), "my own rule") {
		t.Errorf("installed: %q", b)
	}
}

func TestEditedMCPEntryNamesTheEntryAndKeys(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	if code, out, errs := runUpdate(t, env); code != 0 {
		t.Fatalf("%d %s%s", code, out, errs)
	}
	mcp := filepath.Join(env.Home, ".kiro/settings/mcp.json")
	b, err := os.ReadFile(mcp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcp, []byte(strings.Replace(string(b), "gh-mcp", "my-gh", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != int(exit.Finding) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	block := strings.Join(editedBlock(t, out), "\n")
	for _, w := range []string{`remove the entry "github" from ~/.kiro/settings/mcp.json`, "command, args, env, url, headers", "~/.config/livery/personal/mcp/github.json"} {
		if !strings.Contains(block, w) {
			t.Errorf("block lacks %q:\n%s", w, block)
		}
	}
}

func TestKeptYoursSaysTheTeamsVersionIsNotInstalled(t *testing.T) {
	t.Parallel()
	src := enginetest.Source(t, files("skill v1\n"), "v0.1.0")
	env := enginetest.Env(t)
	writeConfig(t, env, src)
	mine := filepath.Join(env.Home, ".kiro/steering/go.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runUpdate(t, env)
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	want := "kept, yours: ~/.kiro/steering/go.md (the team's version is not installed; to install it, rename or delete yours, then run livery update)\n"
	if !strings.Contains(out, want) {
		t.Errorf("output: %s", out)
	}
}
