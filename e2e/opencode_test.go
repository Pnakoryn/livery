//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// opencode: setup into a pre-existing opencode.json keeps the user's keys, uninstall restores them.
func TestJourneyOpencode(t *testing.T) {
	j := newJourney(t)
	xdg := j.path(".xdg")
	j.env = []string{"XDG_CONFIG_HOME=" + xdg}
	cfg := filepath.Join(xdg, "opencode", "opencode.json")
	writeFile(t, cfg, `{"theme": "x", "instructions": ["~/mine.md"]}`+"\n")

	out, _ := j.expect(0, "setup", "--source", j.src, "--harness", "opencode")
	j.has(out, "note    opencode: instruction go applies to every file (opencode has no file patterns)")
	if out2, _ := j.expect(0, "update", "--plan"); out2 == "" {
		t.Error("no output")
	} else {
		j.has(out2, "note    opencode: instruction go applies to every file")
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFile(t, cfg)), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["theme"]) != `"x"` {
		t.Errorf("theme = %s", got["theme"])
	}
	var ins []string
	if err := json.Unmarshal(got["instructions"], &ins); err != nil || len(ins) != 2 || ins[0] != "~/mine.md" {
		t.Errorf("instructions = %s (%v)", got["instructions"], err)
	}
	var mcp struct {
		Servers map[string]json.RawMessage `json:"servers"`
	}
	if err := json.Unmarshal(got["mcp"], &mcp); err != nil || len(mcp.Servers) != 2 {
		t.Fatalf("mcp = %s (%v)", got["mcp"], err)
	}
	if compact(t, mcp.Servers["github"]) != `{"command":["github-mcp","stdio"],"environment":{"GITHUB_TOKEN":"{env:GITHUB_TOKEN}"},"type":"local"}` {
		t.Errorf("github = %s", mcp.Servers["github"])
	}
	if compact(t, mcp.Servers["docs"]) != `{"type":"remote","url":"https://docs.example.com/mcp"}` {
		t.Errorf("docs = %s", mcp.Servers["docs"])
	}
	for _, p := range []string{"skills/review/SKILL.md", "livery/instructions/go.md", "livery/instructions/commits.md"} {
		if !exists(filepath.Join(xdg, "opencode", p)) {
			t.Errorf("%s missing", p)
		}
	}
	j.expect(0, "doctor")

	// A lost lock: update adopts what is already on disk, the list element too.
	if err := os.Remove(filepath.Join(xdg, "livery", "lock.json")); err != nil {
		t.Fatal(err)
	}
	out, _ = j.expect(0, "update")
	j.has(out, "adopt   ")
	j.has(out, "instructions[")
	j.expect(0, "doctor")

	j.expect(0, "uninstall")
	data := readFile(t, cfg)
	var after map[string]any
	if err := json.Unmarshal([]byte(data), &after); err != nil {
		t.Fatal(err)
	}
	for k, v := range after {
		switch k {
		case "theme", "instructions":
		default:
			if !onlyEmpty(v) {
				t.Errorf("leftover %s: %v", k, v)
			}
		}
	}
	if ins, _ := after["instructions"].([]any); len(ins) != 1 || ins[0] != "~/mine.md" || after["theme"] != "x" {
		t.Errorf("after uninstall: %s", data)
	}
	for _, p := range []string{"skills/review/SKILL.md", "livery/instructions/go.md", "livery/instructions/commits.md"} {
		if _, err := os.Lstat(filepath.Join(xdg, "opencode", p)); err == nil {
			t.Errorf("%s still there", p)
		}
	}
}

// onlyEmpty reports whether v is an object holding nothing but empty objects.
func onlyEmpty(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, c := range m {
		if !onlyEmpty(c) {
			return false
		}
	}
	return true
}

func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// opencode: the diff of an edited MCP entry does not print the user's values in environment.
func TestJourneyOpencodeMasksEditedEntry(t *testing.T) {
	j := newJourney(t)
	xdg := j.path(".xdg")
	j.env = []string{"XDG_CONFIG_HOME=" + xdg}
	cfg := filepath.Join(xdg, "opencode", "opencode.json")
	j.expect(0, "setup", "--source", j.src, "--harness", "opencode")

	data := readFile(t, cfg)
	if !strings.Contains(data, "{env:GITHUB_TOKEN}") {
		t.Fatalf("no github token reference in %s", data)
	}
	edited := strings.Replace(data, "{env:GITHUB_TOKEN}", "s3cr3tTok", 1)
	writeFile(t, cfg, edited)

	out, errs := j.expect(1, "update")
	if strings.Contains(out+errs, "s3cr3tTok") {
		t.Errorf("the user's value is printed:\n%s\n%s", out, errs)
	}
	j.has(out, `"GITHUB_TOKEN": "***"`, "{env:GITHUB_TOKEN}")
	if got := readFile(t, cfg); got != edited {
		t.Errorf("the file changed:\n%s", got)
	}
}
