package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
)

// Kind is the kind of an Item.
type Kind string

// The kinds of item.
const (
	Skill           Kind = "skill"
	InstructionKind Kind = "instruction"
	MCP             Kind = "mcp"
	Native          Kind = "native"
)

// File is one file of an item. Path is relative to the item and slash-separated.
type File struct {
	Path string
	Data []byte
	Exec bool
}

// Item is one unit of content read from a layer.
type Item struct {
	Kind Kind
	// Name is the skill, instruction or server name; for Native the path below the harness
	// directory, e.g. "hooks/lint.json".
	Name string
	// Harness is the top-level directory name (Native only).
	Harness string
	// Layer is as given to LoadLayer: "base", "team:web", "personal".
	Layer string
	// Origin is the path inside the layer's fs, for messages.
	Origin string
	// Files: Skill: every file of the directory, sorted by Path; Native: one file.
	Files []File
	// Instruction is set for InstructionKind only.
	Instruction Instruction
	// Server is the MCP server's JSON object, compacted (MCP only).
	Server json.RawMessage
}

// Manifest is livery.json at the root of the shared repository.
type Manifest struct {
	Schema int `json:"schema"`
}

// MCPNameRule is the rule for the name of an MCP server, as shown in messages.
const MCPNameRule = "^[A-Za-z0-9][A-Za-z0-9._-]*$"

var mcpNameRe = regexp.MustCompile(MCPNameRule)

// ValidMCPName says whether name is a valid MCP server name.
func ValidMCPName(name string) bool { return mcpNameRe.MatchString(name) }

// LoadLayer reads the root of fsys (one layer directory) into items, sorted by kind, harness
// and name. Every error is an exit.Usage error naming the layer and the path.
func LoadLayer(fsys fs.FS, layer string) ([]Item, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, exit.Usagef("%s: cannot read the layer: %v", layer, err)
	}
	var items []Item
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch {
		case e.Type()&fs.ModeSymlink != 0 || (!e.IsDir() && !e.Type().IsRegular()):
			return nil, exit.Usagef("%s: %s: not a regular file or directory (symlinks are not followed); remove it", layer, name)
		case !e.IsDir():
			if name == "README.md" {
				continue
			}
			return nil, exit.Usagef("%s: %s: unexpected file at the top level; only README.md is allowed, remove or move it into skills/, instructions/, mcp/ or a harness directory", layer, name)
		}
		var got []Item
		var err error
		switch name {
		case "skills":
			got, err = loadSkills(fsys, layer)
		case "instructions":
			got, err = loadInstructions(fsys, layer)
		case "mcp":
			got, err = loadMCP(fsys, layer)
		default:
			got, err = loadNative(fsys, layer, name)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, got...)
	}
	SortItems(items)
	return items, nil
}

func checkName(layer, origin, name string) error {
	if !ValidName(name) {
		return exit.Usagef("%s: %s: name %q must match %s; rename it", layer, origin, name, NameRule)
	}
	return nil
}

func readDir(fsys fs.FS, layer, dir string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, exit.Usagef("%s: %s: cannot read directory: %v", layer, dir, err)
	}
	return entries, nil
}

func loadSkills(fsys fs.FS, layer string) ([]Item, error) {
	entries, err := readDir(fsys, layer, "skills")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		origin := path.Join("skills", e.Name())
		if !e.IsDir() {
			return nil, exit.Usagef("%s: %s: a file directly in skills/; skills are directories skills/<name>/SKILL.md", layer, origin)
		}
		if err := checkName(layer, origin, e.Name()); err != nil {
			return nil, err
		}
		files, err := readTree(fsys, layer, origin)
		if err != nil {
			return nil, err
		}
		hasSkill := false
		for _, f := range files {
			if f.Path == "SKILL.md" {
				hasSkill = true
			}
		}
		if !hasSkill {
			return nil, exit.Usagef("%s: %s: SKILL.md is missing; add %s/SKILL.md", layer, origin, origin)
		}
		items = append(items, Item{Kind: Skill, Name: e.Name(), Layer: layer, Origin: origin, Files: files})
	}
	return items, nil
}

// readTree returns every regular file below dir, paths relative to dir, sorted by path.
func readTree(fsys fs.FS, layer, dir string) ([]File, error) {
	var files []File
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return exit.Usagef("%s: %s: not a regular file (symlinks are not followed); remove it", layer, p)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		files = append(files, File{Path: strings.TrimPrefix(p, dir+"/"), Data: data, Exec: info.Mode()&0o100 != 0})
		return nil
	})
	if err != nil {
		var ee *exit.Error
		if errors.As(err, &ee) {
			return nil, err
		}
		return nil, exit.Usagef("%s: %s: cannot read: %v", layer, dir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func loadInstructions(fsys fs.FS, layer string) ([]Item, error) {
	entries, err := readDir(fsys, layer, "instructions")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		origin := path.Join("instructions", e.Name())
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if e.IsDir() || !e.Type().IsRegular() || !ok {
			return nil, exit.Usagef("%s: %s: instructions must be regular files named <name>.md; rename or remove it", layer, origin)
		}
		if err := checkName(layer, origin, name); err != nil {
			return nil, err
		}
		data, err := fs.ReadFile(fsys, origin)
		if err != nil {
			return nil, exit.Usagef("%s: %s: cannot read: %v", layer, origin, err)
		}
		ins, err := ParseInstruction(layer+": "+origin, data)
		if err != nil {
			return nil, err
		}
		items = append(items, Item{Kind: InstructionKind, Name: name, Layer: layer, Origin: origin, Instruction: ins})
	}
	return items, nil
}

func loadMCP(fsys fs.FS, layer string) ([]Item, error) {
	entries, err := readDir(fsys, layer, "mcp")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		origin := path.Join("mcp", e.Name())
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !e.Type().IsRegular() || !ok {
			return nil, exit.Usagef("%s: %s: MCP servers must be regular files named <name>.json; rename or remove it", layer, origin)
		}
		if !ValidMCPName(name) {
			return nil, exit.Usagef("%s: %s: name %q must match %s; rename it", layer, origin, name, MCPNameRule)
		}
		data, err := fs.ReadFile(fsys, origin)
		if err != nil {
			return nil, exit.Usagef("%s: %s: cannot read: %v", layer, origin, err)
		}
		if err := validateServer(layer, origin, data); err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, data); err != nil {
			return nil, exit.Usagef("%s: %s: not valid JSON: %v", layer, origin, err)
		}
		items = append(items, Item{Kind: MCP, Name: name, Layer: layer, Origin: origin, Server: json.RawMessage(buf.Bytes())})
	}
	return items, nil
}

func validateServer(layer, origin string, data []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return exit.Usagef("%s: %s: must be a JSON object (one MCP server per file); fix the file", layer, origin)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !IsMCPKey(k) {
			return exit.Usagef("%s: %s: unknown key %q; allowed keys are %s", layer, origin, k, strings.Join(MCPKeys(), ", "))
		}
	}
	_, hasCmd := obj["command"]
	_, hasURL := obj["url"]
	if hasCmd == hasURL {
		return exit.Usagef("%s: %s: exactly one of command and url is required; set one and remove the other", layer, origin)
	}
	key := "command"
	if hasURL {
		key = "url"
	}
	var s string
	if err := json.Unmarshal(obj[key], &s); err != nil || s == "" {
		return exit.Usagef("%s: %s: %s must be a non-empty string", layer, origin, key)
	}
	if raw, ok := obj["args"]; ok {
		var a []string
		if err := json.Unmarshal(raw, &a); err != nil || a == nil {
			return exit.Usagef("%s: %s: args must be an array of strings", layer, origin)
		}
	}
	for _, k := range []string{"env", "headers"} {
		if raw, ok := obj[k]; ok {
			var m map[string]string
			if err := json.Unmarshal(raw, &m); err != nil || m == nil {
				return exit.Usagef("%s: %s: %s must be an object of strings", layer, origin, k)
			}
		}
	}
	return nil
}

func loadNative(fsys fs.FS, layer, dir string) ([]Item, error) {
	files, err := readTree(fsys, layer, dir)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(files))
	for _, f := range files {
		items = append(items, Item{
			Kind:    Native,
			Name:    f.Path,
			Harness: dir,
			Layer:   layer,
			Origin:  path.Join(dir, f.Path),
			Files:   []File{f},
		})
	}
	return items, nil
}

// ReadManifest reads livery.json at the root of the repository's fs.
func ReadManifest(fsys fs.FS) (Manifest, error) {
	data, err := fs.ReadFile(fsys, "livery.json")
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, exit.Usagef("this is not a shared repository: livery.json is missing; add {\"schema\": 1} at the repository root")
	}
	if err != nil {
		return Manifest{}, exit.Usagef("livery.json: cannot read: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil || raw == nil {
		if err == nil {
			err = errors.New("not a JSON object")
		}
		return Manifest{}, exit.Usagef("livery.json: %v; the file must be {\"schema\": 1} and nothing else", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Manifest{}, exit.Usagef("livery.json: unexpected data after the JSON object; the file must be {\"schema\": 1}")
	}
	for k := range raw {
		if k != "schema" {
			return Manifest{}, exit.Usagef("livery.json: unknown key %q; the file must be {\"schema\": 1} and nothing else", k)
		}
	}
	var m Manifest
	if err := json.Unmarshal(raw["schema"], &m.Schema); err != nil || m.Schema < 1 {
		return Manifest{}, exit.Usagef("livery.json: schema is missing or not a number >= 1; the file must be {\"schema\": 1}")
	}
	if m.Schema > 1 {
		return Manifest{}, exit.Environmentf("livery.json: schema %d: this repository needs a newer livery; upgrade livery and run again", m.Schema)
	}
	return m, nil
}
