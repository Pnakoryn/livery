package claude

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter/adaptertest"
	"github.com/Pnakoryn/livery/internal/content/contenttest"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

func env(vars map[string]string) adapter.Env {
	return adapter.Env{Home: "/h", Getenv: func(k string) string { return vars[k] }}
}

func render(t *testing.T, e adapter.Env, items ...content.Item) adapter.Plan {
	t.Helper()
	p, err := New().Render(items, e)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRoot(t *testing.T) {
	for _, c := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"default", nil, "/h/.claude"},
		{"absolute", map[string]string{"CLAUDE_CONFIG_DIR": "/x/c"}, "/x/c"},
		{"relative", map[string]string{"CLAUDE_CONFIG_DIR": "c"}, "/h/c"},
	} {
		if got := New().Root(env(c.vars)); got != c.want {
			t.Errorf("%s: Root = %q, want %q", c.name, got, c.want)
		}
	}
	if New().Name() != "claude" {
		t.Error("Name")
	}
}

func TestSkill(t *testing.T) {
	p := render(t, env(nil), content.Item{Kind: content.Skill, Name: "review", Layer: "base", Origin: "skills/review", Files: []content.File{
		{Path: "SKILL.md", Data: []byte("s")},
		{Path: "scripts/run.sh", Data: []byte("r"), Exec: true},
	}})
	if p.Root != "/h/.claude" || len(p.Files) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	if p.Files[0].Path != "/h/.claude/skills/review/SKILL.md" || p.Files[0].Exec {
		t.Errorf("file 0 = %+v", p.Files[0])
	}
	if p.Files[1].Path != "/h/.claude/skills/review/scripts/run.sh" || !p.Files[1].Exec || string(p.Files[1].Data) != "r" {
		t.Errorf("file 1 = %+v", p.Files[1])
	}
	if p.Files[0].Origin != "base: skills/review" {
		t.Errorf("origin = %q", p.Files[0].Origin)
	}
}

func TestInstruction(t *testing.T) {
	for _, c := range []struct {
		name string
		ins  content.Instruction
		want string
	}{
		{"always", content.Instruction{Description: "d", Body: "Use Go.\n\n\n"}, "Use Go.\n"},
		{"globs", content.Instruction{Globs: []string{"**/*.go", "go.mod"}, Body: "x"},
			"---\npaths:\n  - \"**/*.go\"\n  - \"go.mod\"\n---\n\nx\n"},
		{"braces", content.Instruction{Globs: []string{"**/*.{ts,tsx}"}, Body: "x"},
			"---\npaths:\n  - \"**/*.{ts,tsx}\"\n---\n\nx\n"},
		{"quoting", content.Instruction{Globs: []string{"a\"<b"}, Body: "x\n"},
			"---\npaths:\n  - \"a\\\"<b\"\n---\n\nx\n"},
	} {
		p := render(t, env(nil), content.Item{Kind: content.InstructionKind, Name: "go", Layer: "base", Origin: "instructions/go.md", Instruction: c.ins})
		if len(p.Files) != 1 || p.Files[0].Path != "/h/.claude/rules/go.md" {
			t.Fatalf("%s: plan = %+v", c.name, p)
		}
		if got := string(p.Files[0].Data); got != c.want {
			t.Errorf("%s: data = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMCP(t *testing.T) {
	for _, c := range []struct {
		name, server, path, want string
		vars                     map[string]string
	}{
		{"stdio", `{"command":"gh-mcp","args":["serve"]}`, "/h/.claude.json", `{"args":["serve"],"command":"gh-mcp","type":"stdio"}`, nil},
		{"http", `{"url":"https://x"}`, "/h/.claude.json", `{"type":"http","url":"https://x"}`, nil},
		{"config dir", `{"command":"a"}`, "/x/c/.claude.json", `{"command":"a","type":"stdio"}`, map[string]string{"CLAUDE_CONFIG_DIR": "/x/c"}},
		{"keeps type", `{"type":"sse","url":"https://x?a=1&b=<2>"}`, "/h/.claude.json", `{"type":"sse","url":"https://x?a=1&b=<2>"}`, nil},
	} {
		p := render(t, env(c.vars), content.Item{Kind: content.MCP, Name: "github", Layer: "base", Origin: "mcp/github.json", Server: []byte(c.server)})
		if len(p.Entries) != 1 {
			t.Fatalf("%s: plan = %+v", c.name, p)
		}
		e := p.Entries[0]
		if e.Path != c.path || e.Key != "mcpServers" || e.Name != "github" || !adaptertest.SameJSON(e.Value, c.want) {
			t.Errorf("%s: entry = %+v (value %s)", c.name, e, e.Value)
		}
	}
}

func native(harness, name string) content.Item {
	return content.Item{Kind: content.Native, Name: name, Harness: harness, Layer: "team:web", Origin: harness + "/" + name,
		Files: []content.File{{Path: name, Data: []byte("---\nname: r\n---\nbody\n")}}}
}

func TestNative(t *testing.T) {
	for _, c := range []struct {
		name string
		vars map[string]string
		root string
	}{
		{"default", nil, "/h/.claude"},
		{"config dir", map[string]string{"CLAUDE_CONFIG_DIR": "/x/c"}, "/x/c"},
	} {
		p := render(t, env(c.vars), native("claude", "agents/reviewer.md"), native("claude", "agents/sub/a.md"), native("kiro", "hooks/a.json"))
		if len(p.Files) != 2 || len(p.Entries) != 0 {
			t.Fatalf("%s: plan = %+v", c.name, p)
		}
		f := p.Files[0]
		if f.Path != c.root+"/agents/reviewer.md" || string(f.Data) != "---\nname: r\n---\nbody\n" || f.Origin != "team:web: claude/agents/reviewer.md" {
			t.Errorf("%s: file 0 = %+v", c.name, f)
		}
		if p.Files[1].Path != c.root+"/agents/sub/a.md" {
			t.Errorf("%s: file 1 = %+v", c.name, p.Files[1])
		}
	}
	for _, name := range []string{"commands/x.md", "settings.json", "agentsx/a.md", "agents"} {
		_, err := New().Render([]content.Item{native("claude", name)}, env(nil))
		var ee *exit.Error
		if !errors.As(err, &ee) || ee.Code != exit.Usage {
			t.Fatalf("%s: err = %v", name, err)
		}
		want := "team:web: claude/" + name + ": only files below agents/ are copied for Claude Code; move it there or remove it"
		if err.Error() != want {
			t.Errorf("%s: message = %q", name, err)
		}
	}
}

func TestProjectScope(t *testing.T) {
	proj := t.TempDir()
	e := adapter.Env{Home: "/h", Project: proj, Getenv: func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return "/x/c"
		}
		return ""
	}}
	if got := New().Root(e); got != proj+"/.claude" {
		t.Fatalf("Root = %q", got)
	}
	items, err := content.LoadLayer(os.DirFS(contenttest.Base(t, t.TempDir())), "base")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New().Render(append(items, native("claude", "agents/reviewer.md")), e)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) == 0 || len(p.Entries) == 0 {
		t.Fatalf("plan = %+v", p)
	}
	if !slices.ContainsFunc(p.Files, func(f adapter.File) bool { return f.Path == proj+"/.claude/agents/reviewer.md" }) {
		t.Errorf("no subagent in the project: %+v", p.Files)
	}
	for _, f := range p.Files {
		if !strings.HasPrefix(f.Path, proj+"/") {
			t.Errorf("file outside the project: %s", f.Path)
		}
	}
	for _, en := range p.Entries {
		if en.Path != proj+"/.mcp.json" {
			t.Errorf("entry %s: path %q", en.Name, en.Path)
		}
	}
}
