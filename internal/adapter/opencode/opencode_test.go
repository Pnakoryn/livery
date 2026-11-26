package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter/adaptertest"
	"github.com/Pnakoryn/livery/internal/content/contenttest"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

func env(home string, vars map[string]string) adapter.Env {
	return adapter.Env{Home: home, Getenv: func(k string) string { return vars[k] }}
}

func items() []content.Item {
	return []content.Item{
		{Kind: content.Skill, Name: "review", Layer: "base", Origin: "skills/review", Files: []content.File{
			{Path: "SKILL.md", Data: []byte("s")}, {Path: "scripts/c.sh", Data: []byte("r"), Exec: true}}},
		{Kind: content.InstructionKind, Name: "commits", Layer: "base", Origin: "instructions/commits.md",
			Instruction: content.Instruction{Body: "Be brief.\n\n"}},
		{Kind: content.InstructionKind, Name: "go", Layer: "base", Origin: "instructions/go.md",
			Instruction: content.Instruction{Globs: []string{"**/*.go"}, Body: "Use stdlib."}},
		{Kind: content.MCP, Name: "docs", Layer: "base", Origin: "mcp/docs.json", Server: []byte(`{"url":"https://d.example/mcp"}`)},
		{Kind: content.MCP, Name: "github", Layer: "base", Origin: "mcp/github.json",
			Server: []byte(`{"command":"github-mcp","args":["stdio"],"env":{"GITHUB_TOKEN":"${GITHUB_TOKEN}"}}`)},
	}
}

func TestRender(t *testing.T) {
	base, err := content.LoadLayer(os.DirFS(contenttest.Base(t, t.TempDir())), "base")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New().Render(base, env("/h", nil))
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != "/h/.config/opencode" {
		t.Errorf("root %q", p.Root)
	}
	var paths []string
	for _, f := range p.Files {
		paths = append(paths, f.Path)
	}
	want := "/h/.config/opencode/livery/instructions/commits.md /h/.config/opencode/livery/instructions/go.md " +
		"/h/.config/opencode/skills/review/SKILL.md /h/.config/opencode/skills/review/scripts/check.sh"
	if strings.Join(paths, " ") != want {
		t.Errorf("files %v", paths)
	}
	if !strings.HasPrefix(string(p.Files[0].Data), "Write commit subjects") || !strings.HasSuffix(string(p.Files[0].Data), "characters.\n") {
		t.Errorf("body %q", p.Files[0].Data)
	}
	// The fixture script is not executable; the plan must carry the loaded flag through.
	var wantExec bool
	for _, it := range base {
		for _, f := range it.Files {
			if f.Path == "scripts/check.sh" {
				wantExec = f.Exec
			}
		}
	}
	if p.Files[3].Exec != wantExec {
		t.Errorf("exec bit %v, loaded %v", p.Files[3].Exec, wantExec)
	}
	var lists, servers int
	for _, e := range p.Entries {
		cfg := "/h/.config/opencode/opencode.json"
		if e.Path != cfg {
			t.Errorf("entry path %q", e.Path)
		}
		switch {
		case e.List:
			lists++
			if e.Key != "instructions" || e.Name != "~/.config/opencode/livery/instructions/*.md" {
				t.Errorf("list entry %+v", e)
			}
		case e.Key == "mcp.servers":
			servers++
			if e.Name == "github" {
				w := `{"command":["github-mcp","stdio"],"environment":{"GITHUB_TOKEN":"{env:GITHUB_TOKEN}"},"type":"local"}`
				if !adaptertest.SameJSON(e.Value, w) {
					t.Errorf("github = %s", e.Value)
				}
			}
			if e.Name == "docs" && !adaptertest.SameJSON(e.Value, `{"type":"remote","url":"https://docs.example.com/mcp"}`) {
				t.Errorf("docs = %s", e.Value)
			}
		}
	}
	if lists != 1 || servers != 2 {
		t.Errorf("lists %d servers %d", lists, servers)
	}
	if len(p.Notes) != 1 || p.Notes[0] != "opencode: instruction go applies to every file (opencode has no file patterns)" {
		t.Errorf("notes %v", p.Notes)
	}
}

func TestRemoteHeaders(t *testing.T) {
	p, err := New().Render([]content.Item{{Kind: content.MCP, Name: "r", Layer: "base", Origin: "mcp/r.json",
		Server: []byte(`{"url":"https://x","headers":{"Authorization":"Bearer ${TOKEN}"}}`)}}, env("/h", nil))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"headers":{"Authorization":"Bearer {env:TOKEN}"},"type":"remote","url":"https://x"}`
	if !adaptertest.SameJSON(p.Entries[0].Value, want) {
		t.Errorf("got %s", p.Entries[0].Value)
	}
}

func TestDefaultValueIsUsage(t *testing.T) {
	_, err := New().Render([]content.Item{{Kind: content.MCP, Name: "r", Layer: "team:x", Origin: "mcp/r.json",
		Server: []byte(`{"command":"p","env":{"A":"${TOKEN:-x}"}}`)}}, env("/h", nil))
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Usage {
		t.Fatalf("err %v", err)
	}
	for _, s := range []string{"team:x", "mcp/r.json", "${TOKEN:-x}", "default"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error lacks %q: %v", s, err)
		}
	}
}

func TestAbsoluteGlobOutsideHome(t *testing.T) {
	p, err := New().Render(items(), env("/h", map[string]string{"XDG_CONFIG_HOME": "/abs/elsewhere"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range p.Entries {
		if e.List && e.Name != "/abs/elsewhere/opencode/livery/instructions/*.md" {
			t.Errorf("glob %q", e.Name)
		}
	}
}

func TestNoInstructionNoList(t *testing.T) {
	p, err := New().Render(items()[:1], env("/h", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 0 || len(p.Notes) != 0 {
		t.Errorf("plan %+v", p)
	}
}

func TestJSONCPreferred(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := New().Render(items(), env(home, nil))
	if err != nil {
		t.Fatal(err)
	}
	if p.Entries[0].Path != filepath.Join(root, "opencode.jsonc") {
		t.Errorf("path %q", p.Entries[0].Path)
	}
}

func TestNativeRejected(t *testing.T) {
	_, err := New().Render([]content.Item{{Kind: content.Native, Harness: "opencode", Name: "x", Layer: "base"}}, env("/h", nil))
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Usage || !strings.Contains(err.Error(), "native files for opencode are not supported yet") {
		t.Errorf("err %v", err)
	}
	if _, err := New().Render([]content.Item{{Kind: content.Native, Harness: "kiro", Name: "x"}}, env("/h", nil)); err != nil {
		t.Error(err)
	}
}

func projectEnv(project string, vars map[string]string) adapter.Env {
	e := env("/h", vars)
	e.Project = project
	return e
}

func TestProjectScope(t *testing.T) {
	base, err := content.LoadLayer(os.DirFS(contenttest.Base(t, t.TempDir())), "base")
	if err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	p, err := New().Render(base, projectEnv(proj, map[string]string{"XDG_CONFIG_HOME": "/x"}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != filepath.Join(proj, ".opencode") {
		t.Errorf("root %q", p.Root)
	}
	var paths []string
	for _, f := range p.Files {
		paths = append(paths, strings.TrimPrefix(f.Path, proj+"/"))
	}
	want := ".opencode/livery/instructions/commits.md .opencode/livery/instructions/go.md " +
		".opencode/skills/review/SKILL.md .opencode/skills/review/scripts/check.sh"
	if got := strings.Join(paths, " "); got != want {
		t.Errorf("files %q, want %q", got, want)
	}
	lists, servers := 0, 0
	for _, e := range p.Entries {
		if e.Path != filepath.Join(proj, "opencode.json") {
			t.Errorf("entry path %q", e.Path)
		}
		switch e.Key {
		case "instructions":
			lists++
			if e.Name != ".opencode/livery/instructions/*.md" || !e.List {
				t.Errorf("list entry %+v", e)
			}
		case "mcp.servers":
			servers++
		}
	}
	if lists != 1 || servers != 2 {
		t.Errorf("lists %d servers %d", lists, servers)
	}
}

func TestProjectConfigFile(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".opencode", "opencode.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := projectConfigFile(proj); got != filepath.Join(proj, ".opencode", "opencode.json") {
		t.Errorf("got %q", got)
	}
	if err := os.WriteFile(filepath.Join(proj, "opencode.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := projectConfigFile(proj); got != filepath.Join(proj, "opencode.jsonc") {
		t.Errorf("got %q", got)
	}
}
