package kiro

import (
	"errors"
	"os"
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

func TestRoot(t *testing.T) {
	for _, c := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"default", nil, "/h/.kiro"},
		{"absolute", map[string]string{"KIRO_HOME": "/x/k"}, "/x/k"},
		{"relative", map[string]string{"KIRO_HOME": "k"}, "/h/k"},
	} {
		if got := New().Root(env(c.vars)); got != c.want {
			t.Errorf("%s: Root = %q, want %q", c.name, got, c.want)
		}
	}
	if New().Name() != "kiro" {
		t.Error("Name")
	}
}

func render(t *testing.T, items ...content.Item) adapter.Plan {
	t.Helper()
	p, err := New().Render(items, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSkill(t *testing.T) {
	p := render(t, content.Item{Kind: content.Skill, Name: "review", Layer: "base", Origin: "skills/review", Files: []content.File{
		{Path: "SKILL.md", Data: []byte("s")},
		{Path: "scripts/run.sh", Data: []byte("r"), Exec: true},
	}})
	if p.Root != "/h/.kiro" || len(p.Files) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	if p.Files[0].Path != "/h/.kiro/skills/review/SKILL.md" || p.Files[0].Exec {
		t.Errorf("file 0 = %+v", p.Files[0])
	}
	if p.Files[1].Path != "/h/.kiro/skills/review/scripts/run.sh" || !p.Files[1].Exec || string(p.Files[1].Data) != "r" {
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
		{"always", content.Instruction{Description: "d", Body: "Use Go.\n\n\n"}, "---\ninclusion: always\n---\n\nUse Go.\n"},
		{"globs", content.Instruction{Globs: []string{"**/*.go", "go.mod"}, Body: "x"},
			"---\ninclusion: fileMatch\nfileMatchPattern: [\"**/*.go\", \"go.mod\"]\n---\n\nx\n"},
		{"braces", content.Instruction{Globs: []string{"**/*.{ts,tsx}"}, Body: "x"},
			"---\ninclusion: fileMatch\nfileMatchPattern: [\"**/*.{ts,tsx}\"]\n---\n\nx\n"},
		{"one glob", content.Instruction{Globs: []string{"a\"<b"}, Body: "x\n"},
			"---\ninclusion: fileMatch\nfileMatchPattern: [\"a\\\"<b\"]\n---\n\nx\n"},
	} {
		p := render(t, content.Item{Kind: content.InstructionKind, Name: "go", Layer: "team:web", Origin: "instructions/go.md", Instruction: c.ins})
		if len(p.Files) != 1 || p.Files[0].Path != "/h/.kiro/steering/go.md" {
			t.Fatalf("%s: plan = %+v", c.name, p)
		}
		if got := string(p.Files[0].Data); got != c.want {
			t.Errorf("%s: data = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMCP(t *testing.T) {
	p := render(t,
		content.Item{Kind: content.MCP, Name: "github", Layer: "base", Origin: "mcp/github.json", Server: []byte(`{"url":"u"}`)},
		content.Item{Kind: content.MCP, Name: "alpha", Layer: "base", Origin: "mcp/alpha.json", Server: []byte(`{"command":"c"}`)},
	)
	if len(p.Entries) != 2 || p.Entries[0].Name != "alpha" {
		t.Fatalf("entries = %+v", p.Entries)
	}
	e := p.Entries[1]
	if e.Path != "/h/.kiro/settings/mcp.json" || e.Key != "mcpServers" || e.Name != "github" || !adaptertest.SameJSON(e.Value, `{"url":"u"}`) || e.Origin != "base: mcp/github.json" {
		t.Errorf("entry = %+v", e)
	}
}

func native(harness, name string) content.Item {
	return content.Item{Kind: content.Native, Name: name, Harness: harness, Layer: "personal", Origin: harness + "/" + name,
		Files: []content.File{{Path: name, Data: []byte("d")}}}
}

func TestNative(t *testing.T) {
	p := render(t, native("kiro", "hooks/lint.json"), native("kiro", "agents/a.json"), native("claude", "settings.json"))
	if len(p.Files) != 2 || p.Files[0].Path != "/h/.kiro/agents/a.json" || p.Files[1].Path != "/h/.kiro/hooks/lint.json" {
		t.Fatalf("files = %+v", p.Files)
	}
	for _, name := range []string{"powers/x/POWER.md", "settings/mcp.json", "steeringx/a.md"} {
		_, err := New().Render([]content.Item{native("kiro", name)}, env(nil))
		var ee *exit.Error
		if !errors.As(err, &ee) || ee.Code != exit.Usage {
			t.Fatalf("%s: err = %v", name, err)
		}
		if !strings.Contains(err.Error(), "personal: kiro/"+name) || !strings.Contains(err.Error(), "steering") {
			t.Errorf("%s: message = %q", name, err)
		}
	}
}

func TestProjectScope(t *testing.T) {
	proj := t.TempDir()
	e := adapter.Env{Home: "/h", Project: proj, Getenv: func(k string) string {
		if k == "KIRO_HOME" {
			return "/x/k"
		}
		return ""
	}}
	if got := New().Root(e); got != proj+"/.kiro" {
		t.Fatalf("Root = %q", got)
	}
	items, err := content.LoadLayer(os.DirFS(contenttest.Base(t, t.TempDir())), "base")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New().Render(items, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) == 0 || len(p.Entries) == 0 {
		t.Fatalf("plan = %+v", p)
	}
	for _, f := range p.Files {
		if !strings.HasPrefix(f.Path, proj+"/") {
			t.Errorf("file outside the project: %s", f.Path)
		}
	}
	for _, en := range p.Entries {
		if en.Path != proj+"/.kiro/settings/mcp.json" {
			t.Errorf("entry %s: path %q", en.Name, en.Path)
		}
	}
}
