package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/content/contenttest"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
)

// sameJSON compares two JSON documents by decoded value, not by bytes.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(x, y)
}

func importEnv(t *testing.T) (adapter.Env, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "claude")
	return adapter.Env{Home: t.TempDir(), Getenv: func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return root
		}
		return ""
	}}, root
}

func put(t *testing.T, p, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func doImport(t *testing.T, e adapter.Env) ([]adapter.Found, []adapter.Skipped) {
	t.Helper()
	f, s, err := New().(adapter.Importer).Import(e)
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

func find(found []adapter.Found, kind content.Kind, name string) *adapter.Found {
	for i := range found {
		if found[i].Item.Kind == kind && found[i].Item.Name == name {
			return &found[i]
		}
	}
	return nil
}

func skippedHas(s []adapter.Skipped, path, reason string) bool {
	for _, x := range s {
		if strings.HasSuffix(x.Path, path) && strings.Contains(x.Reason, reason) {
			return true
		}
	}
	return false
}

// TestImportRoundTrip renders the base content, writes it to disk and imports it again, once for
// the machine scope and once for the project scope.
func TestImportRoundTrip(t *testing.T) {
	items, err := content.LoadLayer(os.DirFS(contenttest.Base(t, t.TempDir())), "base")
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, native("claude", "agents/reviewer.md"), native("claude", "agents/sub/b.md"))
	machineEnv, machineRoot := importEnv(t)
	projEnv, proj := projectEnv(t)
	scopes := []struct {
		name  string
		env   adapter.Env
		mcp   string // the file that holds the servers
		inDir string // when set, every found path and entry path is below it
	}{
		{"machine", machineEnv, filepath.Join(machineRoot, ".claude.json"), ""},
		{"project", projEnv, filepath.Join(proj, ".mcp.json"), proj},
	}
	for _, sc := range scopes {
		t.Run(sc.name, func(t *testing.T) {
			plan, err := New().Render(items, sc.env)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range plan.Files {
				mode := os.FileMode(0o644)
				if f.Exec {
					mode = 0o755
				}
				put(t, f.Path, string(f.Data), mode)
			}
			servers := map[string]json.RawMessage{}
			for _, en := range plan.Entries {
				if sc.inDir != "" && en.Path != sc.mcp {
					t.Fatalf("entry path %s", en.Path)
				}
				servers[en.Name] = en.Value
			}
			b, _ := json.Marshal(map[string]any{"mcpServers": servers})
			put(t, sc.mcp, string(b), 0o644)

			found, skipped := doImport(t, sc.env)
			if len(skipped) != 0 {
				t.Errorf("skipped = %+v", skipped)
			}
			n := 0
			for _, want := range items {
				if want.Kind == content.Native && want.Harness != "claude" {
					continue
				}
				n++
				f := find(found, want.Kind, want.Name)
				if f == nil {
					t.Errorf("%s %s not found", want.Kind, want.Name)
					continue
				}
				got := f.Item
				if got.Harness != want.Harness || (want.Kind == content.Native && got.Origin != want.Name) {
					t.Errorf("%s: harness %q origin %q", want.Name, got.Harness, got.Origin)
				}
				if !reflect.DeepEqual(got.Files, want.Files) {
					t.Errorf("%s: files differ:\n%+v\n%+v", want.Name, got.Files, want.Files)
				}
				if !reflect.DeepEqual(got.Instruction.Globs, want.Instruction.Globs) || got.Instruction.Body != want.Instruction.Body {
					t.Errorf("%s: instruction %+v, want %+v", want.Name, got.Instruction, want.Instruction)
				}
				if !sameJSON(got.Server, want.Server) {
					t.Errorf("%s: server %s, want %s", want.Name, got.Server, want.Server)
				}
				if want.Kind == content.MCP && (f.Key != "mcpServers" || f.Name != want.Name || f.Path == "") {
					t.Errorf("%s: entry reference %+v", want.Name, f)
				}
				if len(f.Paths) == 0 {
					t.Errorf("%s: no paths", want.Name)
				}
				for _, p := range f.Paths {
					if sc.inDir != "" && !strings.HasPrefix(p, sc.inDir+string(filepath.Separator)) {
						t.Errorf("%s: path %s not inside project", want.Name, p)
					}
				}
			}
			if len(found) != n {
				t.Errorf("found %d items, want %d", len(found), n)
			}
		})
	}
}

func TestImportRules(t *testing.T) {
	e, root := importEnv(t)
	r := filepath.Join(root, "rules")
	put(t, filepath.Join(r, "plain.md"), "just text\n", 0o644)
	put(t, filepath.Join(r, "block.md"), "---\npaths:\n  - \"**/*.go\"\n  - '*.md'\n---\n\nb\n", 0o644)
	put(t, filepath.Join(r, "inline.md"), "---\npaths: [\"a\", b]\n---\nc\n", 0o644)
	put(t, filepath.Join(r, "brace.md"), "---\npaths: ['**/*.{ts,tsx}']\n---\nc\n", 0o644)
	put(t, filepath.Join(r, "desc.md"), "---\ndescription: x\n---\nz\n", 0o644)
	put(t, filepath.Join(r, "web", "css.md"), "x\n", 0o644)
	put(t, filepath.Join(r, "Bad Name.md"), "x\n", 0o644)
	found, skipped := doImport(t, e)
	for name, want := range map[string][]string{"plain": nil, "block": {"**/*.go", "*.md"}, "inline": {"a", "b"}, "brace": {"**/*.{ts,tsx}"}} {
		f := find(found, content.InstructionKind, name)
		if f == nil || !reflect.DeepEqual(f.Item.Instruction.Globs, want) {
			t.Errorf("%s: %+v", name, f)
		}
	}
	if f := find(found, content.InstructionKind, "plain"); f == nil || f.Item.Instruction.Body != "just text\n" {
		t.Errorf("plain: %+v", f)
	}
	if len(found) != 4 {
		t.Errorf("found = %+v", found)
	}
	if !skippedHas(skipped, "desc.md", "line 2") {
		t.Errorf("skipped = %+v", skipped)
	}
	if !skippedHas(skipped, "web", "rules in subfolders are not imported; move it up") {
		t.Errorf("skipped = %+v", skipped)
	}
	if !skippedHas(skipped, "Bad Name.md", "rename") {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestImportMCP(t *testing.T) {
	e, root := importEnv(t)
	put(t, filepath.Join(root, ".claude.json"), `{"mcpServers":{
		"old":{"type":"sse","url":"https://x"},
		"web":{"type":"http","url":"https://y","headers":{"A":"b"}},
		"local":{"type":"stdio","command":"y","args":["a"],"timeout":5}},
		"projects":{"/p":{"mcpServers":{"proj":{"command":"z"}}}}}`, 0o644)
	found, skipped := doImport(t, e)
	if len(found) != 2 {
		t.Fatalf("found = %+v", found)
	}
	if f := find(found, content.MCP, "web"); f == nil || string(f.Item.Server) != `{"url":"https://y","headers":{"A":"b"}}` {
		t.Errorf("web: %+v", f)
	}
	if f := find(found, content.MCP, "local"); f == nil || string(f.Item.Server) != `{"command":"y","args":["a"]}` {
		t.Errorf("local: %+v", f)
	}
	if !skippedHas(skipped, ".claude.json", "server old: type sse") {
		t.Errorf("skipped = %+v", skipped)
	}
	if !skippedHas(skipped, ".claude.json", "server local: imported without timeout") {
		t.Errorf("skipped = %+v", skipped)
	}
	if len(skipped) != 2 {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestImportSeenNotImported(t *testing.T) {
	e, root := importEnv(t)
	put(t, filepath.Join(root, "CLAUDE.md"), "SECRET-NOTES\n", 0o644)
	put(t, filepath.Join(root, "settings.json"), `{"SECRET":1}`, 0o644)
	put(t, filepath.Join(root, "commands", "a.md"), "x", 0o644)
	put(t, filepath.Join(root, "other.txt"), "x", 0o644)
	found, skipped := doImport(t, e)
	if len(found) != 0 {
		t.Errorf("found = %+v", found)
	}
	if len(skipped) != 3 || !skippedHas(skipped, "CLAUDE.md", "personal notes") ||
		!skippedHas(skipped, "settings.json", "not supported by livery yet") ||
		!skippedHas(skipped, "commands", "not supported by livery yet") {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestImportSkillsAndLinks(t *testing.T) {
	e, root := importEnv(t)
	put(t, filepath.Join(root, "skills", "My Skill", "SKILL.md"), "x", 0o644)
	put(t, filepath.Join(root, "skills", "ok", "SKILL.md"), "x", 0o644)
	put(t, filepath.Join(root, "skills", "ok", "run.sh"), "#!/bin/sh\n", 0o755)
	outside := filepath.Join(t.TempDir(), "secret.md")
	put(t, outside, "secret", 0o644)
	if err := os.Symlink(outside, filepath.Join(root, "rules", "link.md")); err != nil {
		if err := os.MkdirAll(filepath.Join(root, "rules"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "rules", "link.md")); err != nil {
			t.Fatal(err)
		}
	}
	found, skipped := doImport(t, e)
	if len(found) != 1 {
		t.Fatalf("found = %+v", found)
	}
	files := found[0].Item.Files
	if len(files) != 2 || files[1].Path != "run.sh" || !files[1].Exec || files[0].Exec {
		t.Errorf("files = %+v", files)
	}
	if !skippedHas(skipped, "My Skill", "rename") || !skippedHas(skipped, "link.md", "symbolic link") {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestImportMissingRoot(t *testing.T) {
	e, _ := importEnv(t)
	found, skipped := doImport(t, e)
	if len(found) != 0 || len(skipped) != 0 {
		t.Errorf("found %v skipped %v", found, skipped)
	}
}

func TestImportMCPOriginDefaultLayout(t *testing.T) {
	home := t.TempDir()
	put(t, filepath.Join(home, ".claude", "x"), "", 0o644)
	put(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"web":{"url":"https://y"}}}`, 0o644)
	e := adapter.Env{Home: home, Getenv: func(string) string { return "" }}
	found, _ := doImport(t, e)
	f := find(found, content.MCP, "web")
	if f == nil || f.Item.Origin != ".claude.json" {
		t.Errorf("web: %+v", f)
	}
}

// A parent folder that cannot be searched makes Lstat of the skills and rules folders fail; each is
// a "cannot read: " skip, not an error and not silence.
func TestImportUnsearchableRootIsSkip(t *testing.T) {
	e, _ := importEnv(t)
	e.Project = t.TempDir()
	root := filepath.Join(e.Project, ".claude")
	put(t, filepath.Join(root, "skills", "review", "SKILL.md"), "x\n", 0o644)
	put(t, filepath.Join(root, "rules", "a.md"), "x\n", 0o644)
	if err := os.Chmod(root, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if _, err := os.Lstat(filepath.Join(root, "skills")); err == nil {
		t.Skip("the folder is searchable anyway (running as root?)")
	}
	_, skipped, err := New().(adapter.Importer).Import(e)
	if err != nil {
		t.Fatalf("err %v", err)
	}
	for _, dir := range []string{"skills", "rules"} {
		if !skippedHas(skipped, "/"+dir, "cannot read: ") {
			t.Errorf("no cannot-read skip for %s: %v", dir, skipped)
		}
	}
}

func TestImportMCPDuplicateKey(t *testing.T) {
	e, root := importEnv(t)
	put(t, filepath.Join(root, ".claude.json"), `{"mcpServers":{"dup":{"command":"a","command":"b"}}}`, 0o644)
	found, _ := doImport(t, e)
	if f := find(found, content.MCP, "dup"); f == nil || string(f.Item.Server) != `{"command":"b"}` {
		t.Fatalf("found = %+v", found)
	}
}
