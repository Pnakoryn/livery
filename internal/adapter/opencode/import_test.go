package opencode

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/content/contenttest"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/adapter/adaptertest"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

func impEnv(t *testing.T) (adapter.Env, string, string) {
	t.Helper()
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "xdg")
	e := env(home, map[string]string{"XDG_CONFIG_HOME": xdg})
	return e, filepath.Join(xdg, "opencode"), home
}

func write(t *testing.T, p, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runImport(t *testing.T, e adapter.Env) ([]adapter.Found, []adapter.Skipped, error) {
	t.Helper()
	return New().(adapter.Importer).Import(e)
}

func names(f []adapter.Found, k content.Kind) string {
	var n []string
	for _, x := range f {
		if x.Item.Kind == k {
			n = append(n, x.Item.Name)
		}
	}
	return strings.Join(n, ",")
}

// TestImportRoundTrip renders content, writes it to disk and imports it again, once for the
// machine scope and once for the project scope.
func TestImportRoundTrip(t *testing.T) {
	src := contenttest.Base(t, t.TempDir())
	read := func(p string) []byte {
		b, err := os.ReadFile(filepath.Join(src, p))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	skillMd, check := read("skills/review/SKILL.md"), read("skills/review/scripts/check.sh")
	docs, gh := read("mcp/docs.json"), read("mcp/github.json")
	in := []content.Item{
		{Kind: content.Skill, Name: "review", Layer: "base", Origin: "skills/review", Files: []content.File{
			{Path: "SKILL.md", Data: skillMd}, {Path: "scripts/check.sh", Data: check, Exec: true}}},
		{Kind: content.InstructionKind, Name: "commits", Layer: "base", Origin: "i/commits.md",
			Instruction: content.Instruction{Body: "x"}},
		{Kind: content.MCP, Name: "docs", Layer: "base", Origin: "mcp/docs.json", Server: docs},
		{Kind: content.MCP, Name: "github", Layer: "base", Origin: "mcp/github.json", Server: gh},
	}
	machineEnv, machineRoot, _ := impEnv(t)
	pEnv, proj := projEnv(t)
	scopes := []struct {
		name  string
		env   adapter.Env
		cfg   string // the opencode.json that holds the servers
		inDir string // when set, every server path is below it
	}{
		{"machine", machineEnv, filepath.Join(machineRoot, "opencode.json"), ""},
		{"project", pEnv, filepath.Join(proj, "opencode.json"), proj},
	}
	for _, sc := range scopes {
		t.Run(sc.name, func(t *testing.T) {
			plan, err := New().Render(in, sc.env)
			if err != nil {
				t.Fatal(err)
			}
			servers := map[string]json.RawMessage{}
			var globs []string
			for _, f := range plan.Files {
				write(t, f.Path, string(f.Data))
				if f.Exec {
					if err := os.Chmod(f.Path, 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, en := range plan.Entries {
				if en.List {
					globs = append(globs, en.Name)
				} else {
					servers[en.Name] = en.Value
				}
			}
			cfg, _ := json.Marshal(map[string]any{"instructions": globs, "mcp": map[string]any{"servers": servers}})
			write(t, sc.cfg, string(cfg))

			found, skipped, err := runImport(t, sc.env)
			if err != nil {
				t.Fatal(err)
			}
			if len(skipped) != 0 {
				t.Errorf("skipped %v", skipped)
			}
			if got := names(found, content.InstructionKind); got != "" {
				t.Errorf("own instructions imported: %q", got)
			}
			var gotDocs, gotGH string
			for _, f := range found {
				switch {
				case f.Item.Kind == content.Skill:
					if len(f.Item.Files) != 2 || f.Item.Files[1].Path != "scripts/check.sh" || !f.Item.Files[1].Exec ||
						string(f.Item.Files[0].Data) != string(skillMd) {
						t.Errorf("skill %+v", f.Item)
					}
				case f.Item.Name == "docs":
					gotDocs = string(f.Item.Server)
				case f.Item.Name == "github":
					gotGH = string(f.Item.Server)
					if f.Key != "mcp.servers" || f.Name != "github" {
						t.Errorf("ref %q %q", f.Key, f.Name)
					}
				}
				if f.Item.Kind == content.MCP && sc.inDir != "" && !strings.HasPrefix(f.Path, sc.inDir) {
					t.Errorf("path %s", f.Path)
				}
			}
			same := func(a, b []byte) bool {
				var x, y any
				return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && jsonEq(x, y)
			}
			if !same([]byte(gotDocs), docs) || !same([]byte(gotGH), gh) || !strings.Contains(gotGH, "${GITHUB_TOKEN}") {
				t.Errorf("servers %s %s", gotDocs, gotGH)
			}
		})
	}
}

func jsonEq(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestImportInstructions(t *testing.T) {
	e, root, home := impEnv(t)
	write(t, filepath.Join(home, "rules", "team.md"), "team rules\n")
	write(t, filepath.Join(root, "opencode.json"),
		`{"instructions": ["~/rules/team.md", "docs/*.md", "https://example.com/x.md"]}`)
	found, skipped, err := runImport(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if names(found, content.InstructionKind) != "team" || found[0].Item.Instruction.Body != "team rules\n" {
		t.Errorf("found %+v", found)
	}
	if len(skipped) != 2 {
		t.Errorf("skipped %v", skipped)
	}
}

func TestImportDeprecatedServers(t *testing.T) {
	e, root, _ := impEnv(t)
	write(t, filepath.Join(root, "opencode.json"), `{"mcp": {
  "timeout": 5000,
  "old": {"type": "local", "command": ["x", "a"], "environment": {"T": "{env:TOK}"}},
  "dup": {"type": "remote", "url": "https://old.example"},
  "servers": {"dup": {"type": "remote", "url": "https://new.example", "headers": {"A": "Bearer {env:TOK}"}},
              "off": {"type": "local", "command": ["y"], "disabled": true},
              "off2": {"type": "local", "command": ["y"], "enabled": false},
              "extra": {"type": "local", "command": ["z"], "timeout": 3}}}}`)
	found, skipped, err := runImport(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(found, content.MCP); got != "dup,extra,old" {
		t.Errorf("servers %q", got)
	}
	for _, f := range found {
		switch f.Item.Name {
		case "old":
			if !adaptertest.SameJSON(f.Item.Server, `{"command":"x","args":["a"],"env":{"T":"${TOK}"}}`) || f.Key != "mcp" {
				t.Errorf("old %s %s", f.Item.Server, f.Key)
			}
		case "dup":
			if !strings.Contains(string(f.Item.Server), "new.example") || !strings.Contains(string(f.Item.Server), "${TOK}") {
				t.Errorf("dup %s", f.Item.Server)
			}
		}
	}
	if len(skipped) != 4 {
		t.Errorf("skipped %v", skipped)
	}
}

func TestImportAgentsMd(t *testing.T) {
	e, root, _ := impEnv(t)
	write(t, filepath.Join(root, "AGENTS.md"), "mine\n")
	found, skipped, err := runImport(t, e)
	if err != nil || len(found) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "personal notes") {
		t.Errorf("%v %v %v", found, skipped, err)
	}
}

func TestImportComment(t *testing.T) {
	e, root, _ := impEnv(t)
	write(t, filepath.Join(root, "opencode.json"), "{\n// c\n\"mcp\": {}}")
	_, _, err := runImport(t, e)
	var ee *exit.Error
	if !errors.As(err, &ee) || ee.Code != 1 || !strings.Contains(err.Error(), "opencode.json") {
		t.Errorf("err %v", err)
	}
}

func TestImportMissingRoot(t *testing.T) {
	e, _, _ := impEnv(t)
	f, s, err := runImport(t, e)
	if f != nil || s != nil || err != nil {
		t.Errorf("%v %v %v", f, s, err)
	}
}

func TestImportDropsKeysOfOtherType(t *testing.T) {
	e, root, _ := impEnv(t)
	write(t, filepath.Join(root, "opencode.json"), `{"mcp": {"servers": {
  "loc": {"type": "local", "command": ["x"], "url": "https://u", "headers": {"A": "b"}},
  "rem": {"type": "remote", "url": "https://r", "command": ["y"], "environment": {"A": "b"}}}}}`)
	found, skipped, err := runImport(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(found, content.MCP); got != "loc,rem" {
		t.Errorf("servers %q", got)
	}
	var all []string
	for _, s := range skipped {
		all = append(all, s.Reason)
	}
	j := strings.Join(all, "\n")
	for _, w := range []string{"server loc: imported without headers, url", "server rem: imported without command, environment"} {
		if !strings.Contains(j, w) {
			t.Errorf("missing %q in %v", w, all)
		}
	}
}

func TestImportUnreadableSkillsFolderIsSkipped(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every folder")
	}
	e, root, _ := impEnv(t)
	write(t, filepath.Join(root, "opencode.json"), `{"mcp": {"servers": {"loc": {"type": "local", "command": ["x"]}}}}`)
	skills := filepath.Join(root, "skills")
	write(t, filepath.Join(skills, "review", "SKILL.md"), "x\n")
	if err := os.Chmod(skills, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })
	found, skipped, err := runImport(t, e)
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if names(found, content.MCP) != "loc" {
		t.Errorf("found %v", found)
	}
	if len(skipped) != 1 || skipped[0].Path != skills || !strings.HasPrefix(skipped[0].Reason, "cannot read: ") {
		t.Errorf("skipped %v", skipped)
	}
}
