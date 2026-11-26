package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/content/contenttest"
)

func writeFiles(t *testing.T, dir string, files []File) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if f.Exec {
			mode = 0o755
		}
		if err := os.WriteFile(p, f.Data, mode); err != nil {
			t.Fatal(err)
		}
	}
}

func stripOrigin(items []Item) []Item {
	out := append([]Item(nil), items...)
	for i := range out {
		out[i].Layer, out[i].Origin = "", ""
	}
	return out
}

func roundTrip(t *testing.T, items []Item) []Item {
	t.Helper()
	files, err := LayerFiles(items)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	got, err := LoadLayer(os.DirFS(dir), "base")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestLayerFilesRoundTripBase(t *testing.T) {
	items, err := LoadLayer(os.DirFS(contenttest.Base(t, t.TempDir())), "base")
	if err != nil {
		t.Fatal(err)
	}
	got := roundTrip(t, items)
	if !reflect.DeepEqual(stripOrigin(got), stripOrigin(items)) {
		t.Fatalf("round trip differs:\n got %+v\nwant %+v", got, items)
	}
}

func TestLayerFilesInstructionGlobs(t *testing.T) {
	items := []Item{{Kind: InstructionKind, Name: "go", Instruction: Instruction{
		Description: "Go style", Globs: []string{"**/*.go", "a,b/{x,y}.ts"}, Body: "Use gofmt.\n",
	}}}
	got := roundTrip(t, items)
	if !reflect.DeepEqual(stripOrigin(got), items) {
		t.Fatalf("got %+v", got)
	}
}

func TestLayerFilesDuplicate(t *testing.T) {
	s := Item{Kind: Skill, Name: "x", Files: []File{{Path: "SKILL.md", Data: []byte("a")}}}
	if _, err := LayerFiles([]Item{s, s}); err == nil {
		t.Fatal("want error")
	}
	if _, err := LayerFiles([]Item{{Kind: MCP, Name: "-bad", Server: json.RawMessage(`{"command":"x"}`)}}); err == nil {
		t.Fatal("want error for bad name")
	}
}

func TestEnvRefs(t *testing.T) {
	out, names, err := EnvRefs(json.RawMessage(`{"command":"x","env":{"TOKEN":"abc","REF":"${OTHER}"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"command":"x","env":{"TOKEN":"${TOKEN}","REF":"${OTHER}"}}` {
		t.Fatalf("out %s", out)
	}
	if !reflect.DeepEqual(names, []string{"TOKEN"}) {
		t.Fatalf("names %v", names)
	}
	if _, _, err := EnvRefs(json.RawMessage(`{"command":"x","env":{"my-key":"v"}}`)); err == nil || !strings.Contains(err.Error(), "my-key") {
		t.Fatalf("err %v", err)
	}
	in := json.RawMessage(`{"command":"x"}`)
	out, names, err = EnvRefs(in)
	if err != nil || string(out) != string(in) || names != nil {
		t.Fatalf("%s %v %v", out, names, err)
	}
}

func TestHeaderRefs(t *testing.T) {
	in := json.RawMessage(`{"url":"https://x","headers":{"Authorization":"Bearer abcdefghij1234567890","Proxy-Authorization":"basic dXNlcjpwYXNz","X-Api-Key":"abc","Cookie":"a=b","x-session-token":"t","Client-Secret":"s","DB-Password":"p","Accept":"application/json","X-Auth":"Bearer ${MINE}"}}`)
	out, names, err := HeaderRefs(in, "my-server")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"url":"https://x","headers":{"Authorization":"Bearer ${MY_SERVER_AUTHORIZATION}","Proxy-Authorization":"basic ${MY_SERVER_PROXY_AUTHORIZATION}","X-Api-Key":"${MY_SERVER_X_API_KEY}","Cookie":"${MY_SERVER_COOKIE}","x-session-token":"${MY_SERVER_X_SESSION_TOKEN}","Client-Secret":"${MY_SERVER_CLIENT_SECRET}","DB-Password":"${MY_SERVER_DB_PASSWORD}","Accept":"application/json","X-Auth":"Bearer ${MINE}"}}`
	if string(out) != want {
		t.Fatalf("out %s", out)
	}
	wantNames := []string{"MY_SERVER_AUTHORIZATION", "MY_SERVER_CLIENT_SECRET", "MY_SERVER_COOKIE", "MY_SERVER_DB_PASSWORD", "MY_SERVER_PROXY_AUTHORIZATION", "MY_SERVER_X_API_KEY", "MY_SERVER_X_SESSION_TOKEN"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("names %v", names)
	}
	if HeaderRefName("github", "Authorization") != "GITHUB_AUTHORIZATION" || HeaderRefName("1x", "k") != "_1X_K" {
		t.Fatal("HeaderRefName")
	}
	for _, s := range []string{`{"command":"x"}`, `{"url":"u","headers":{"Accept":"a","Authorization":"Bearer ${T}"}}`} {
		out, names, err := HeaderRefs(json.RawMessage(s), "x")
		if err != nil || string(out) != s || names != nil {
			t.Errorf("%s: %s %v %v", s, out, names, err)
		}
	}
	if _, _, err := HeaderRefs(json.RawMessage(`{"url":"u","headers":{"Authorization":1}}`), "x"); err == nil {
		t.Error("non-string header: want an error")
	}
}

func TestHeaderRefsSameVariable(t *testing.T) {
	if _, _, err := HeaderRefs(json.RawMessage(`{"url":"u","headers":{"X-Key":"a","X_Key":"b"}}`), "s"); err == nil {
		t.Error("two headers with one variable name: want an error")
	}
}

func TestURLPassword(t *testing.T) {
	for s, want := range map[string]bool{
		`{"url":"https://user:pw@example.com/mcp"}`: true,
		`{"url":"https://user:p%2Fw@example.com"}`:  true,
		`{"url":"https://u:${PW}@example.com"}`:     false,
		`{"url":"https://u:a${PW}@example.com"}`:    true,
		`{"url":"https://user@example.com/mcp"}`:    false,
		`{"url":"https://example.com/a:b@c"}`:       false,
		`{"url":"https://example.com:8080/mcp"}`:    false,
		`{"command":"x","args":["https://u:p@h"]}`:  false,
	} {
		if got := URLPassword(json.RawMessage(s)); got != want {
			t.Errorf("%s: got %v", s, got)
		}
	}
}

func TestLayerFilesRejects(t *testing.T) {
	file := []File{{Path: "SKILL.md", Data: []byte("a")}}
	cmd := json.RawMessage(`{"command":"x"}`)
	cases := map[string]Item{
		"skill path escapes":    {Kind: Skill, Name: "x", Files: []File{{Path: "../../y", Data: []byte("a")}}},
		"skill without files":   {Kind: Skill, Name: "x"},
		"skill without SKILL":   {Kind: Skill, Name: "x", Files: []File{{Path: "a.md", Data: []byte("a")}}},
		"description newline":   {Kind: InstructionKind, Name: "x", Instruction: Instruction{Description: "a\nb", Body: "c\n"}},
		"description quoted":    {Kind: InstructionKind, Name: "x", Instruction: Instruction{Description: `"a"`, Body: "c\n"}},
		"native name escapes":   {Kind: Native, Harness: "kiro", Name: "../x", Files: file},
		"native reserved":       {Kind: Native, Harness: "skills", Name: "x", Files: file},
		"native harness nested": {Kind: Native, Harness: "a/b", Name: "x", Files: file},
		"server unknown key":    {Kind: MCP, Name: "x", Server: json.RawMessage(`{"command":"x","timeout":1}`)},
		"server empty":          {Kind: MCP, Name: "x", Server: json.RawMessage(`{}`)},
	}
	for name, it := range cases {
		if _, err := LayerFiles([]Item{it}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	a := Item{Kind: Skill, Name: "x", Files: file}
	b := Item{Kind: Skill, Name: "x", Files: []File{{Path: "other.md", Data: []byte("b")}}}
	if _, err := LayerFiles([]Item{a, b}); err == nil {
		t.Error("two skills of one name with different files: want an error")
	}
	if _, err := LayerFiles([]Item{{Kind: MCP, Name: "x", Server: cmd}}); err != nil {
		t.Errorf("valid server: %v", err)
	}
}

func TestLayerFilesKeepsExecAndRuleBody(t *testing.T) {
	items := []Item{
		{Kind: InstructionKind, Name: "rule", Instruction: Instruction{Body: "---\nafter a rule\n"}},
		{Kind: Skill, Name: "x", Files: []File{
			{Path: "SKILL.md", Data: []byte("a")},
			{Path: "scripts/run.sh", Data: []byte("#!/bin/sh\n"), Exec: true},
		}},
	}
	got := roundTrip(t, items)
	if !reflect.DeepEqual(stripOrigin(got), items) {
		t.Fatalf("got %+v", got)
	}
}

func TestLayerFilesCRLFBody(t *testing.T) {
	files, err := LayerFiles([]Item{{Kind: InstructionKind, Name: "a", Instruction: Instruction{Description: "d", Body: "one\r\ntwo\r\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files[0].Data), "\none\ntwo\n") || strings.Contains(string(files[0].Data), "\r") {
		t.Errorf("data = %q", files[0].Data)
	}
}

func TestLayerFilesFileIsFolder(t *testing.T) {
	_, err := LayerFiles([]Item{
		{Kind: Native, Harness: "kiro", Name: "a", Files: []File{{Path: "a", Data: []byte("x")}}},
		{Kind: Native, Harness: "kiro", Name: "a/b", Files: []File{{Path: "a/b", Data: []byte("x")}}},
	})
	if err == nil || !strings.Contains(err.Error(), "kiro/a") {
		t.Fatalf("err = %v", err)
	}
}
