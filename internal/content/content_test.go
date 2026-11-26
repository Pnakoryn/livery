package content

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Pnakoryn/livery/internal/exit"
)

func codeOf(t *testing.T, err error) exit.Code {
	t.Helper()
	var ee *exit.Error
	if !errors.As(err, &ee) {
		t.Fatalf("want *exit.Error, got %v", err)
	}
	return ee.Code
}

func TestLoadLayerItems(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/review/SKILL.md":       {Data: []byte("# review")},
		"skills/review/scripts/run.sh": {Data: []byte("#!/bin/sh"), Mode: 0o755},
		"instructions/go.md":           {Data: []byte("use go")},
		"mcp/github.json":              {Data: []byte(`{ "command": "gh-mcp", "args": ["serve"] }`)},
		"kiro/hooks/lint.json":         {Data: []byte("{}")},
		"README.md":                    {Data: []byte("hi")},
	}
	items, err := LoadLayer(fsys, "team:web")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("want 4 items, got %d: %+v", len(items), items)
	}
	by := map[Kind]Item{}
	for _, it := range items {
		by[it.Kind] = it
		if it.Layer != "team:web" {
			t.Errorf("layer %q", it.Layer)
		}
	}
	sk := by[Skill]
	if sk.Name != "review" || len(sk.Files) != 2 || sk.Files[0].Path != "SKILL.md" || sk.Files[1].Path != "scripts/run.sh" || !sk.Files[1].Exec || sk.Files[0].Exec {
		t.Errorf("skill: %+v", sk)
	}
	if by[InstructionKind].Name != "go" || by[InstructionKind].Instruction.Body != "use go" {
		t.Errorf("instruction: %+v", by[InstructionKind])
	}
	if string(by[MCP].Server) != `{"command":"gh-mcp","args":["serve"]}` {
		t.Errorf("server: %s", by[MCP].Server)
	}
	n := by[Native]
	if n.Harness != "kiro" || n.Name != "hooks/lint.json" || len(n.Files) != 1 {
		t.Errorf("native: %+v", n)
	}
}

func TestLoadLayerEmpty(t *testing.T) {
	items, err := LoadLayer(fstest.MapFS{}, "base")
	if err != nil || len(items) != 0 {
		t.Fatalf("got %v, %v", items, err)
	}
}

func TestLoadLayerErrors(t *testing.T) {
	tests := []struct {
		name string
		fs   fstest.MapFS
		want string
	}{
		{"no SKILL.md", fstest.MapFS{"skills/review/a.md": {}}, "skills/review"},
		{"bad skill name", fstest.MapFS{"skills/Bad_Name/SKILL.md": {}}, "skills/Bad_Name"},
		{"file in skills", fstest.MapFS{"skills/x.md": {}}, "skills/x.md"},
		{"instruction ext", fstest.MapFS{"instructions/x.txt": {}}, "instructions/x.txt"},
		{"instruction dir", fstest.MapFS{"instructions/x/y.md": {}}, "instructions/x"},
		{"both", fstest.MapFS{"mcp/x.json": {Data: []byte(`{"command":"a","url":"b"}`)}}, "mcp/x.json"},
		{"neither", fstest.MapFS{"mcp/x.json": {Data: []byte(`{"args":[]}`)}}, "mcp/x.json"},
		{"empty command", fstest.MapFS{"mcp/x.json": {Data: []byte(`{"command":""}`)}}, "mcp/x.json"},
		{"unknown key", fstest.MapFS{"mcp/x.json": {Data: []byte(`{"command":"a","autoApprove":[]}`)}}, `unknown key "autoApprove"`},
		{"args", fstest.MapFS{"mcp/x.json": {Data: []byte(`{"command":"a","args":[1]}`)}}, "mcp/x.json"},
		{"env", fstest.MapFS{"mcp/x.json": {Data: []byte(`{"url":"a","env":{"A":1}}`)}}, "mcp/x.json"},
		{"not json", fstest.MapFS{"mcp/x.json": {Data: []byte(`nope`)}}, "mcp/x.json"},
		{"top-level file", fstest.MapFS{"notes.txt": {}}, "notes.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadLayer(tc.fs, "team:web")
			if err == nil {
				t.Fatal("want error")
			}
			if codeOf(t, err) != exit.Usage {
				t.Errorf("code %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "team:web") {
				t.Errorf("message %q lacks %q or layer", err, tc.want)
			}
		})
	}
}

func TestReadManifest(t *testing.T) {
	tests := []struct {
		name string
		fs   fstest.MapFS
		code exit.Code
		want string
	}{
		{"ok", fstest.MapFS{"livery.json": {Data: []byte(`{"schema":1}`)}}, 0, ""},
		{"newer", fstest.MapFS{"livery.json": {Data: []byte(`{"schema":2}`)}}, exit.Environment, "newer livery"},
		{"unknown key", fstest.MapFS{"livery.json": {Data: []byte(`{"schema":1,"min_livery":"0.1.0"}`)}}, exit.Usage, "min_livery"},
		{"not json", fstest.MapFS{"livery.json": {Data: []byte(`x`)}}, exit.Usage, "livery.json"},
		{"empty object", fstest.MapFS{"livery.json": {Data: []byte(`{}`)}}, exit.Usage, "schema"},
		{"schema zero", fstest.MapFS{"livery.json": {Data: []byte(`{"schema":0}`)}}, exit.Usage, "schema"},
		{"schema negative", fstest.MapFS{"livery.json": {Data: []byte(`{"schema":-1}`)}}, exit.Usage, "schema"},
		{"null", fstest.MapFS{"livery.json": {Data: []byte(`null`)}}, exit.Usage, "livery.json"},
		{"wrong case key", fstest.MapFS{"livery.json": {Data: []byte(`{"Schema":1}`)}}, exit.Usage, "Schema"},
		{"trailing brace", fstest.MapFS{"livery.json": {Data: []byte(`{"schema":1}}`)}}, exit.Usage, "after the JSON"},
		{"trailing object", fstest.MapFS{"livery.json": {Data: []byte(`{"schema":1}{"schema":1}`)}}, exit.Usage, "after the JSON"},
		{"missing", fstest.MapFS{}, exit.Usage, "livery.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := ReadManifest(tc.fs)
			if tc.code == 0 {
				if err != nil || m.Schema != 1 {
					t.Fatalf("got %v, %v", m, err)
				}
				return
			}
			if err == nil || codeOf(t, err) != tc.code || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestLoadLayerIgnoresDotEntries(t *testing.T) {
	base := fstest.MapFS{
		"skills/review/SKILL.md": {Data: []byte("# review")},
		"instructions/go.md":     {Data: []byte("use go")},
		"mcp/github.json":        {Data: []byte(`{"command":"gh"}`)},
	}
	want, err := LoadLayer(base, "base")
	if err != nil {
		t.Fatal(err)
	}
	withDots := fstest.MapFS{
		".DS_Store":              {Data: []byte("x")},
		".git/config":            {Data: []byte("x")},
		"skills/.gitkeep":        {Data: []byte("")},
		"instructions/.DS_Store": {Data: []byte("x")},
		"mcp/.DS_Store":          {Data: []byte("x")},
	}
	for k, v := range base {
		withDots[k] = v
	}
	got, err := LoadLayer(withDots, "base")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("want %d items, got %d", len(want), len(got))
	}
	for i := range got {
		if got[i].Kind != want[i].Kind || got[i].Name != want[i].Name {
			t.Errorf("item %d: %+v vs %+v", i, got[i], want[i])
		}
	}
}

func TestLoadLayerKeepsDotFileInSkill(t *testing.T) {
	items, err := LoadLayer(fstest.MapFS{
		"skills/review/SKILL.md": {Data: []byte("# review")},
		"skills/review/.hidden":  {Data: []byte("h")},
	}, "base")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].Files) != 2 || items[0].Files[0].Path != ".hidden" {
		t.Fatalf("items: %+v", items)
	}
}

func TestMCPNames(t *testing.T) {
	ok := fstest.MapFS{
		"mcp/awslabs.core-mcp-server.json": {Data: []byte(`{"command":"a"}`)},
		"mcp/My_Server.json":               {Data: []byte(`{"command":"b"}`)},
		"mcp/.x.json":                      {Data: []byte(`not json`)},
	}
	items, err := LoadLayer(ok, "base")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if len(names) != 2 || names[0] != "My_Server" || names[1] != "awslabs.core-mcp-server" {
		t.Errorf("names = %v", names)
	}
	for _, f := range []string{"mcp/-x.json", "mcp/a b.json"} {
		_, err := LoadLayer(fstest.MapFS{f: {Data: []byte(`{"command":"a"}`)}}, "base")
		if err == nil || !strings.Contains(err.Error(), MCPNameRule) {
			t.Errorf("%s: err = %v", f, err)
		}
	}
	if _, err := LoadLayer(fstest.MapFS{"skills/My_Skill/SKILL.md": {Data: []byte("x")}}, "base"); err == nil {
		t.Error("skill My_Skill accepted")
	}
}

func TestHeaderRefNameWideServer(t *testing.T) {
	if got := HeaderRefName("awslabs.core-mcp-server", "X-Api-Key"); got != "AWSLABS_CORE_MCP_SERVER_X_API_KEY" {
		t.Errorf("got %s", got)
	}
}
