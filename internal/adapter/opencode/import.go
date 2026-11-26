package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	shared "github.com/Pnakoryn/livery/internal/adapter/importer"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

var envRefRe = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

const symlinkReason = shared.SymlinkReason
const nameReason = "name must match " + content.NameRule + "; rename it"

// Import reads the opencode folder back into items. It only reads.
func (o opencode) Import(env adapter.Env) ([]adapter.Found, []adapter.Skipped, error) {
	root := o.Root(env)
	fi, err := os.Stat(root)
	rootOK := err == nil && fi.IsDir()
	linkedRoot := false
	if env.Project != "" {
		if li, lerr := os.Lstat(root); lerr == nil && li.Mode()&fs.ModeSymlink != 0 {
			linkedRoot, rootOK = true, false
		}
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", root, err)
	}
	if !rootOK && env.Project == "" {
		return nil, nil, nil
	}
	im := &importer{Base: shared.Base{Root: root}, root: root, home: env.Home, project: env.Project}
	cfg := configFile(root)
	if env.Project != "" {
		cfg = projectConfigFile(env.Project)
	}
	if linkedRoot {
		im.Skip(root, symlinkReason)
	}
	if rootOK {
		if err := im.skills(); err != nil {
			return nil, nil, err
		}
	}
	if !linkedRoot || !inside(root, cfg) {
		if err := im.config(cfg); err != nil {
			return nil, nil, err
		}
	}
	notes := "personal notes; copy what the team needs into an instruction"
	notesDir := root
	if env.Project != "" {
		notes = "project notes; livery does not manage them"
		notesDir = env.Project
	}
	for _, s := range []struct{ dir, name, reason string }{
		{notesDir, "AGENTS.md", notes},
		{root, "plugins", "not supported by livery yet"},
		{root, "agents", "not supported by livery yet"},
		{root, "commands", "not supported by livery yet"},
	} {
		if linkedRoot && s.dir == root {
			continue
		}
		p := filepath.Join(s.dir, s.name)
		if _, err := os.Lstat(p); err == nil {
			im.Skip(p, s.reason)
		}
	}
	shared.SortFound(im.found)
	sort.SliceStable(im.Skipped, func(i, j int) bool { return im.Skipped[i].Path < im.Skipped[j].Path })
	return im.found, im.Skipped, nil
}

type importer struct {
	shared.Base
	root, home string
	project    string // project root; empty for the machine import
	found      []adapter.Found
}

// origin is p relative to the project root (or the folder root); a path outside it stays absolute.
func (im *importer) origin(p string) string {
	base := im.root
	if im.project != "" {
		base = im.project
	}
	if !inside(base, p) {
		if r, err := filepath.Rel(base, p); err == nil && r == "." {
			return "."
		}
		return filepath.ToSlash(p)
	}
	return (&shared.Base{Root: base}).Rel(p)
}

func cannotRead(p string, err error) error {
	return exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", p, err)
}

func (im *importer) skills() error {
	base := im.root
	if im.project != "" {
		base = im.project
	}
	found, skipped, err := shared.Skills(base, filepath.Join(im.root, "skills"))
	if err != nil {
		return err
	}
	im.found = append(im.found, found...)
	im.Skipped = append(im.Skipped, skipped...)
	return nil
}

func (im *importer) config(cfg string) error {
	fi, err := os.Lstat(cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return cannotRead(cfg, err)
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		im.Skip(cfg, symlinkReason)
		return nil
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		return cannotRead(cfg, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		return exit.Findingf("%s: not strict JSON (%v); comments and trailing commas must be removed for the import, then run again",
			cfg, errOr(err))
	}
	if raw, ok := top["mcp"]; ok {
		if err := im.mcp(cfg, raw); err != nil {
			return err
		}
	}
	if raw, ok := top["instructions"]; ok {
		im.instructions(cfg, raw)
	}
	return nil
}

// linkedParent reports whether a folder between root (excluded) and p
// (excluded) is a symbolic link.
func linkedParent(root, p string) bool {
	rel, err := filepath.Rel(root, filepath.Dir(p))
	if err != nil || rel == "." {
		return false
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if info, err := os.Lstat(cur); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func errOr(err error) error {
	if err == nil {
		return errors.New("top level must be an object")
	}
	return err
}

func (im *importer) instructions(cfg string, raw json.RawMessage) {
	var list []any
	if err := json.Unmarshal(raw, &list); err != nil {
		im.Skip(cfg, "instructions: not an array")
		return
	}
	own := glob(im.root, im.home)
	if im.project != "" {
		own = ".opencode/livery/instructions/*.md"
	}
	ownAbs := filepath.Join(im.root, "livery", "instructions", "*.md")
	seen := map[string]bool{}
	for _, el := range list {
		s, ok := el.(string)
		if !ok {
			im.Skip(cfg, "instructions: element is not a string")
			continue
		}
		if s == own || s == ownAbs || (im.project != "" && filepath.ToSlash(filepath.Clean(s)) == own) {
			continue
		}
		label := "instructions: " + shared.QuoteName(s) + ": "
		var p string
		switch {
		case strings.Contains(s, "://"):
			im.Skip(cfg, label+"a URL; copy the text into an instruction")
			continue
		case strings.ContainsAny(s, "*?["):
			im.Skip(cfg, label+"a glob; list one file, or copy the files into instructions")
			continue
		case im.project != "" && strings.HasPrefix(s, "~/"):
			im.Skip(cfg, label+"outside the project; keep the file inside it")
			continue
		case im.project != "" && !filepath.IsAbs(s):
			p = filepath.Join(im.project, filepath.FromSlash(s))
			if !inside(im.project, p) {
				im.Skip(cfg, label+"outside the project; keep the file inside it")
				continue
			}
		case im.project != "":
			p = filepath.Clean(s)
			if !inside(im.project, p) {
				im.Skip(cfg, label+"outside the project; keep the file inside it")
				continue
			}
		case strings.HasPrefix(s, "~/"):
			if im.home == "" {
				im.Skip(cfg, label+"cannot expand ~/ without a home folder")
				continue
			}
			p = filepath.Join(im.home, s[2:])
		case filepath.IsAbs(s):
			p = filepath.Clean(s)
		default:
			im.Skip(cfg, label+"a relative path; list it as an absolute or ~/ path")
			continue
		}
		if im.project != "" && linkedParent(im.project, p) {
			im.Skip(p, symlinkReason)
			continue
		}
		info, err := os.Lstat(p)
		switch {
		case err != nil:
			im.Skip(cfg, label+"file not found")
			continue
		case info.Mode()&fs.ModeSymlink != 0:
			im.Skip(p, symlinkReason)
			continue
		case !info.Mode().IsRegular():
			im.Skip(cfg, label+"not a regular file")
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		if !content.ValidName(name) {
			im.Skip(p, nameReason)
			continue
		}
		if seen[name] {
			im.Skip(p, "instruction "+name+" is listed twice; the first one is taken")
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil {
			im.Skip(p, "cannot read: "+err.Error())
			continue
		}
		seen[name] = true
		im.found = append(im.found, adapter.Found{
			Item: content.Item{Kind: content.InstructionKind, Name: name, Origin: im.origin(p),
				Instruction: content.Instruction{Body: string(body)}},
			Paths: []string{p},
		})
	}
}

type candidate struct {
	key  string // "mcp.servers" or "mcp"
	name string
	raw  json.RawMessage
}

func (im *importer) mcp(cfg string, raw json.RawMessage) error {
	var mcp map[string]json.RawMessage
	if err := json.Unmarshal(raw, &mcp); err != nil || mcp == nil {
		im.Skip(cfg, "mcp: not a JSON object")
		return nil //nolint:nilerr // a wrong shape is a skipped line, not a failure
	}
	var cands []candidate
	if sraw, ok := mcp["servers"]; ok {
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(sraw, &servers); err != nil || servers == nil {
			im.Skip(cfg, "mcp.servers: not a JSON object")
		}
		for n, v := range servers {
			cands = append(cands, candidate{"mcp.servers", n, v})
		}
	}
	inServers := map[string]bool{}
	for _, c := range cands {
		inServers[c.name] = true
	}
	for n, v := range mcp {
		if n == "servers" || n == "timeout" {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(v, &obj) != nil || obj == nil {
			continue
		}
		if _, ok := obj["type"]; !ok {
			continue
		}
		if inServers[n] {
			im.Skip(cfg, "server "+n+": also defined below mcp.servers, which wins; this one is ignored")
			continue
		}
		cands = append(cands, candidate{"mcp", n, v})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].name < cands[j].name })
	for _, c := range cands {
		if !content.ValidMCPName(c.name) {
			im.Skip(cfg, "server "+shared.QuoteName(c.name)+": name must match "+content.MCPNameRule+"; rename it")
			continue
		}
		val, ok := im.convertServer(cfg, c)
		if !ok {
			continue
		}
		im.found = append(im.found, adapter.Found{
			Item:  content.Item{Kind: content.MCP, Name: c.name, Origin: im.origin(cfg), Server: val},
			Paths: []string{cfg},
			Path:  cfg, Key: c.key, Name: c.name,
		})
	}
	return nil
}

func fromEnv(v any) any {
	switch x := v.(type) {
	case string:
		return envRefRe.ReplaceAllString(x, "${$1}")
	case []any:
		for i := range x {
			x[i] = fromEnv(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = fromEnv(x[k])
		}
	}
	return v
}

func (im *importer) convertServer(cfg string, c candidate) (json.RawMessage, bool) {
	pre := "server " + c.name + ": "
	dec := json.NewDecoder(bytes.NewReader(c.raw))
	dec.UseNumber()
	var in map[string]any
	if err := dec.Decode(&in); err != nil || in == nil {
		im.Skip(cfg, pre+"not a JSON object")
		return nil, false
	}
	typ, _ := in["type"].(string)
	var dropped []string
	for k := range in {
		switch k {
		case "type", "enabled", "disabled":
		case "command", "environment":
			if typ != "local" {
				dropped = append(dropped, k)
			}
		case "url", "headers":
			if typ != "remote" {
				dropped = append(dropped, k)
			}
		default:
			dropped = append(dropped, k)
		}
	}
	sort.Strings(dropped)
	if b, ok := in["disabled"].(bool); ok && b {
		im.Skip(cfg, pre+"disabled")
		return nil, false
	}
	if b, ok := in["enabled"].(bool); ok && !b {
		im.Skip(cfg, pre+"disabled (enabled is false)")
		return nil, false
	}
	res := map[string]any{}
	order := []string{}
	switch typ {
	case "local":
		list, ok := in["command"].([]any)
		if !ok || len(list) == 0 {
			im.Skip(cfg, pre+"local server needs a command array")
			return nil, false
		}
		cmd, ok := list[0].(string)
		if !ok {
			im.Skip(cfg, pre+"command must be an array of strings")
			return nil, false
		}
		res["command"] = cmd
		order = append(order, "command")
		if len(list) > 1 {
			res["args"] = list[1:]
			order = append(order, "args")
		}
		if e, ok := in["environment"].(map[string]any); ok && len(e) > 0 {
			res["env"] = e
			order = append(order, "env")
		}
	case "remote":
		u, ok := in["url"].(string)
		if !ok || u == "" {
			im.Skip(cfg, pre+"remote server needs a url")
			return nil, false
		}
		res["url"] = u
		order = append(order, "url")
		if h, ok := in["headers"].(map[string]any); ok && len(h) > 0 {
			res["headers"] = h
			order = append(order, "headers")
		}
	default:
		im.Skip(cfg, pre+"type must be local or remote")
		return nil, false
	}
	if len(dropped) > 0 {
		im.Note(cfg, shared.DroppedReason(c.name, dropped))
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range order {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		var vb bytes.Buffer
		enc := json.NewEncoder(&vb)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(fromEnv(res[k])); err != nil {
			im.Skip(cfg, pre+"not valid JSON")
			return nil, false
		}
		buf.Write(bytes.TrimSuffix(vb.Bytes(), []byte("\n")))
	}
	buf.WriteByte('}')
	return json.RawMessage(buf.Bytes()), true
}

// inside reports whether p is below root.
func inside(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
