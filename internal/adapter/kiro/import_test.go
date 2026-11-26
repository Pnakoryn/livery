package kiro

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter/adaptertest"
	"github.com/Pnakoryn/livery/internal/content/contenttest"

	"github.com/Pnakoryn/livery/internal/adapter"
	shared "github.com/Pnakoryn/livery/internal/adapter/importer"
	"github.com/Pnakoryn/livery/internal/content"
)

func importEnv(t *testing.T) (adapter.Env, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "kiro")
	return adapter.Env{Home: t.TempDir(), Getenv: func(k string) string {
		if k == "KIRO_HOME" {
			return root
		}
		return ""
	}}, root
}

func put(t *testing.T, p string, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func doImport(t *testing.T, env adapter.Env) ([]adapter.Found, []adapter.Skipped) {
	t.Helper()
	f, s, err := New().(adapter.Importer).Import(env)
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

// TestImportRoundTrip renders the base content, writes it to disk and imports it again, once for
// the machine scope and once for the project scope.
func TestImportRoundTrip(t *testing.T) {
	items, err := content.LoadLayer(os.DirFS(contenttest.Base(t, t.TempDir())), "base")
	if err != nil {
		t.Fatal(err)
	}
	machineEnv, machineRoot := importEnv(t)
	proj := t.TempDir()
	scopes := []struct {
		name  string
		env   adapter.Env
		root  string // the folder that holds settings/mcp.json
		inDir string // when set, every found path starts with it
	}{
		{"machine", machineEnv, machineRoot, ""},
		{"project", adapter.Env{Home: t.TempDir(), Project: proj, Getenv: func(string) string { return "" }},
			filepath.Join(proj, ".kiro"), proj + string(filepath.Separator)},
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
			for _, e := range plan.Entries {
				servers[e.Name] = e.Value
			}
			b, _ := json.Marshal(map[string]any{"mcpServers": servers})
			put(t, filepath.Join(sc.root, "settings", "mcp.json"), string(b), 0o644)

			found, skipped := doImport(t, sc.env)
			if len(skipped) != 0 {
				t.Errorf("skipped = %+v", skipped)
			}
			if len(found) != len(items) {
				t.Fatalf("found %d items, want %d", len(found), len(items))
			}
			for _, want := range items {
				f := find(found, want.Kind, want.Name)
				if f == nil {
					t.Errorf("%s %s not found", want.Kind, want.Name)
					continue
				}
				got := f.Item
				if got.Layer != "" || got.Harness != want.Harness {
					t.Errorf("%s: layer %q harness %q", want.Name, got.Layer, got.Harness)
				}
				if !reflect.DeepEqual(got.Files, want.Files) {
					t.Errorf("%s: files differ:\n%+v\n%+v", want.Name, got.Files, want.Files)
				}
				if !reflect.DeepEqual(got.Instruction.Globs, want.Instruction.Globs) || got.Instruction.Body != want.Instruction.Body {
					t.Errorf("%s: instruction %+v, want %+v", want.Name, got.Instruction, want.Instruction)
				}
				if !adaptertest.SameJSON(got.Server, string(want.Server)) {
					t.Errorf("%s: server %s, want %s", want.Name, got.Server, want.Server)
				}
				if want.Kind == content.MCP && (f.Key != "mcpServers" || f.Name != want.Name || f.Path == "") {
					t.Errorf("%s: entry reference %+v", want.Name, f)
				}
				if len(f.Paths) == 0 {
					t.Errorf("%s: no paths", want.Name)
				}
				for _, p := range f.Paths {
					if sc.inDir != "" && !strings.HasPrefix(p, sc.inDir) {
						t.Errorf("%s: path %s not inside project", want.Name, p)
					}
				}
			}
		})
	}
}

func TestImportManualSteeringIsNative(t *testing.T) {
	env, root := importEnv(t)
	data := "---\ninclusion: manual\n---\n\nHello\n"
	put(t, filepath.Join(root, "steering", "notes.md"), data, 0o644)
	found, _ := doImport(t, env)
	f := find(found, content.Native, "steering/notes.md")
	if f == nil || f.Item.Harness != "kiro" || !bytes.Equal(f.Item.Files[0].Data, []byte(data)) {
		t.Fatalf("found = %+v", found)
	}
}

func TestImportGlobs(t *testing.T) {
	env, root := importEnv(t)
	put(t, filepath.Join(root, "steering", "one.md"), "---\ninclusion: fileMatch\nfileMatchPattern: \"**/*.ts\"\n---\n\nbody\n", 0o644)
	put(t, filepath.Join(root, "steering", "block.md"), "---\ninclusion: fileMatch\nfileMatchPattern:\n  - \"**/*.go\"\n  - '*.md'\n---\nb\n", 0o644)
	put(t, filepath.Join(root, "steering", "inline.md"), "---\ninclusion: fileMatch\nfileMatchPattern: [\"a\", \"b\"]\n---\nc\n", 0o644)
	put(t, filepath.Join(root, "steering", "brace.md"), "---\ninclusion: fileMatch\nfileMatchPattern: ['**/*.{ts,tsx}']\n---\nc\n", 0o644)
	put(t, filepath.Join(root, "steering", "plain.md"), "just text\n", 0o644)
	put(t, filepath.Join(root, "steering", "odd.md"), "---\ninclusion: always\ntitle: x\n---\nz\n", 0o644)
	found, _ := doImport(t, env)
	for name, want := range map[string][]string{"one": {"**/*.ts"}, "block": {"**/*.go", "*.md"}, "inline": {"a", "b"}, "brace": {"**/*.{ts,tsx}"}, "plain": nil} {
		f := find(found, content.InstructionKind, name)
		if f == nil || !reflect.DeepEqual(f.Item.Instruction.Globs, want) {
			t.Errorf("%s: %+v", name, f)
		}
	}
	if f := find(found, content.InstructionKind, "plain"); f == nil || f.Item.Instruction.Body != "just text\n" {
		t.Errorf("plain: %+v", f)
	}
	if find(found, content.Native, "steering/odd.md") == nil {
		t.Error("odd.md should be native")
	}
}

func TestImportMCP(t *testing.T) {
	env, root := importEnv(t)
	put(t, filepath.Join(root, "settings", "mcp.json"), `{"mcpServers":{
		"off":{"command":"x","disabled":true},
		"tools":{"command":"y","args":["a"],"autoApprove":["t"],"timeout":5}}}`, 0o644)
	found, skipped := doImport(t, env)
	if len(found) != 1 || string(found[0].Item.Server) != `{"command":"y","args":["a"]}` {
		t.Fatalf("found = %+v", found)
	}
	var off, dropped bool
	for _, s := range skipped {
		if strings.Contains(s.Reason, "off") && strings.Contains(s.Reason, "disabled") {
			off = true
		}
		if strings.Contains(s.Reason, "tools") && strings.Contains(s.Reason, "autoApprove") {
			dropped = true
		}
	}
	if !off || !dropped {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestImportInvalidJSON(t *testing.T) {
	env, root := importEnv(t)
	put(t, filepath.Join(root, "settings", "mcp.json"), `{nope`, 0o644)
	_, _, err := New().(adapter.Importer).Import(env)
	if err == nil || !strings.Contains(err.Error(), "mcp.json") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportSkippedNamesAndLinks(t *testing.T) {
	env, root := importEnv(t)
	put(t, filepath.Join(root, "skills", "My Skill", "SKILL.md"), "x", 0o644)
	outside := filepath.Join(t.TempDir(), "secret.md")
	put(t, outside, "secret", 0o644)
	if err := os.MkdirAll(filepath.Join(root, "steering"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "steering", "link.md")); err != nil {
		t.Fatal(err)
	}
	found, skipped := doImport(t, env)
	if len(found) != 0 {
		t.Errorf("found = %+v", found)
	}
	var rename, link bool
	for _, s := range skipped {
		if strings.HasSuffix(s.Path, "My Skill") && strings.Contains(s.Reason, "rename") {
			rename = true
		}
		if strings.HasSuffix(s.Path, "link.md") && s.Reason == "symbolic link" {
			link = true
		}
	}
	if !rename || !link {
		t.Errorf("skipped = %+v", skipped)
	}
}

func TestImportMissingRoot(t *testing.T) {
	env, _ := importEnv(t)
	found, skipped := doImport(t, env)
	if len(found) != 0 || len(skipped) != 0 {
		t.Errorf("found %v skipped %v", found, skipped)
	}
}

func TestImportSettingsIsFile(t *testing.T) {
	env, root := importEnv(t)
	put(t, filepath.Join(root, "settings"), "x", 0o644)
	if _, _, err := New().(adapter.Importer).Import(env); err != nil {
		t.Fatal(err)
	}
}

// A parent folder that cannot be searched makes Lstat of the skills and steering folders fail;
// each is a "cannot read: " skip, not an error and not silence. The folders are read directly,
// because the rest of Import also needs the root.
func TestImportUnsearchableRootIsSkip(t *testing.T) {
	_, root := importEnv(t)
	put(t, filepath.Join(root, "skills", "review", "SKILL.md"), "x\n", 0o644)
	put(t, filepath.Join(root, "steering", "a.md"), "x\n", 0o644)
	if err := os.Chmod(root, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if _, err := os.Lstat(filepath.Join(root, "skills")); err == nil {
		t.Skip("the folder is searchable anyway (running as root?)")
	}
	im := &importer{Base: shared.Base{Root: root}}
	if err := im.skills(); err != nil {
		t.Fatalf("skills: %v", err)
	}
	if err := im.steering(); err != nil {
		t.Fatalf("steering: %v", err)
	}
	for _, dir := range []string{"skills", "steering"} {
		if !skippedHasPath(im.Skipped, "/"+dir) {
			t.Errorf("no cannot-read skip for %s: %v", dir, im.Skipped)
		}
	}
}

func skippedHasPath(s []adapter.Skipped, suffix string) bool {
	for _, x := range s {
		if strings.HasSuffix(x.Path, suffix) && strings.HasPrefix(x.Reason, "cannot read: ") {
			return true
		}
	}
	return false
}

func TestImportMCPDuplicateKey(t *testing.T) {
	env, root := importEnv(t)
	put(t, filepath.Join(root, "settings", "mcp.json"), `{"mcpServers":{"dup":{"command":"a","command":"b"}}}`, 0o644)
	found, _ := doImport(t, env)
	if len(found) != 1 || string(found[0].Item.Server) != `{"command":"b"}` {
		t.Fatalf("found = %+v", found)
	}
}
