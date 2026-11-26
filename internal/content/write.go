package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
)

var (
	envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	envRefRe  = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)
)

// LayerFiles returns the files of a layer folder for items, in the layout LoadLayer reads:
// paths are slash-separated, relative to the layer, sorted by path.
func LayerFiles(items []Item) ([]File, error) {
	var files []File
	from := map[string]string{}
	add := func(it Item, f File) error {
		if prev, ok := from[f.Path]; ok {
			return exit.Usagef("%s is written by both %s and %s; rename or remove one of them", f.Path, prev, describe(it))
		}
		from[f.Path] = describe(it)
		files = append(files, f)
		return nil
	}
	seen := map[string]bool{}
	for _, it := range items {
		id := string(it.Kind) + "\x00" + it.Harness + "\x00" + it.Name
		if seen[id] {
			return nil, exit.Usagef("%s is given twice; rename or remove one of them", describe(it))
		}
		seen[id] = true
		switch it.Kind {
		case Skill:
			if err := checkName("layer", describe(it), it.Name); err != nil {
				return nil, err
			}
			if !slices.ContainsFunc(it.Files, func(f File) bool { return f.Path == "SKILL.md" }) {
				return nil, exit.Usagef("%s: SKILL.md is missing; add it or remove the skill", describe(it))
			}
			for _, f := range it.Files {
				if !localPath(f.Path) {
					return nil, exit.Usagef("%s: file path %q must be a relative path without . or .. elements; rename the file", describe(it), f.Path)
				}
				if err := add(it, File{Path: path.Join("skills", it.Name, f.Path), Data: f.Data, Exec: f.Exec}); err != nil {
					return nil, err
				}
			}
		case InstructionKind:
			if err := checkName("layer", describe(it), it.Name); err != nil {
				return nil, err
			}
			data, err := instructionFile(it)
			if err != nil {
				return nil, err
			}
			if err := add(it, File{Path: "instructions/" + it.Name + ".md", Data: data}); err != nil {
				return nil, err
			}
		case MCP:
			if !ValidMCPName(it.Name) {
				return nil, exit.Usagef("layer: %s: name %q must match %s; rename it", describe(it), it.Name, MCPNameRule)
			}
			if err := validateServer("layer", describe(it), it.Server); err != nil {
				return nil, err
			}
			data, err := serverFile(it)
			if err != nil {
				return nil, err
			}
			if err := add(it, File{Path: "mcp/" + it.Name + ".json", Data: data}); err != nil {
				return nil, err
			}
		case Native:
			if it.Harness == "" || it.Name == "" {
				return nil, exit.Usagef("%s: a native item needs a harness and a name", describe(it))
			}
			switch {
			case it.Harness == "skills" || it.Harness == "instructions" || it.Harness == "mcp" || strings.Contains(it.Harness, "/") || !localPath(it.Harness):
				return nil, exit.Usagef("%s: %q is not a harness directory name (one path element, not skills, instructions or mcp)", describe(it), it.Harness)
			case !localPath(it.Name):
				return nil, exit.Usagef("%s: the name must be a relative path without . or .. elements; rename the file", describe(it))
			case len(it.Files) != 1:
				return nil, exit.Usagef("%s: a native item is exactly one file", describe(it))
			}
			for _, f := range it.Files {
				if err := add(it, File{Path: path.Join(it.Harness, it.Name), Data: f.Data, Exec: f.Exec}); err != nil {
					return nil, err
				}
			}
		default:
			return nil, exit.Usagef("%s: unknown item kind %q", describe(it), it.Kind)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		for dir := path.Dir(f.Path); dir != "."; dir = path.Dir(dir) {
			if prev, ok := from[dir]; ok {
				return nil, exit.Usagef("%s is a file (%s) and also a folder of %s; rename one of them", dir, prev, f.Path)
			}
		}
	}
	return files, nil
}

// localPath reports whether p is a slash-separated relative path that stays below its folder.
func localPath(p string) bool {
	return p != "." && fs.ValidPath(p)
}

func describe(it Item) string {
	return fmt.Sprintf("%s %q", it.Kind, it.Name)
}

// instructionFile writes an instruction in the format ParseInstruction reads. That format has
// no escape sequences, so the result is parsed again: a description or glob it cannot hold is
// an error instead of a file that loads as something else.
func instructionFile(it Item) ([]byte, error) {
	ins := it.Instruction
	var b strings.Builder
	body := strings.TrimRight(strings.ReplaceAll(ins.Body, "\r\n", "\n"), "\n")
	if ins.Description != "" || len(ins.Globs) > 0 || strings.HasPrefix(body, "---") {
		b.WriteString("---\n")
		if ins.Description != "" {
			b.WriteString("description: " + ins.Description + "\n")
		}
		if len(ins.Globs) > 0 {
			b.WriteString("globs:\n")
			for _, g := range ins.Globs {
				b.WriteString("  - \"" + g + "\"\n")
			}
		}
		b.WriteString("---\n\n")
	}
	b.WriteString(body)
	b.WriteString("\n")
	back, err := ParseInstruction(describe(it), []byte(b.String()))
	if err != nil || back.Description != ins.Description || !slices.Equal(back.Globs, ins.Globs) ||
		strings.TrimRight(back.Body, "\n") != body {
		return nil, exit.Usagef("%s: the description or a glob cannot be written as an instruction file (quotes, a newline or leading spaces in it); simplify it", describe(it))
	}
	return []byte(b.String()), nil
}

func serverFile(it Item) ([]byte, error) {
	keys, vals, err := OrderedObject(it.Server)
	if err != nil {
		return nil, exit.Usagef("%s: server is not a JSON object: %v", describe(it), err)
	}
	var parts []string
	for _, k := range MCPKeys() {
		for i, kk := range keys {
			if kk == k {
				parts = append(parts, JSONString(k)+":"+string(vals[i]))
			}
		}
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte("{"+strings.Join(parts, ",")+"}"), "", "  "); err != nil {
		return nil, exit.Usagef("%s: server is not valid JSON: %v", describe(it), err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// EnvRefs replaces every value of the server's env object that is not already exactly ${NAME}
// by ${KEY}, and returns the sorted keys it replaced. The rest of the server is unchanged.
func EnvRefs(server json.RawMessage) (out json.RawMessage, names []string, err error) {
	keys, vals, err := OrderedObject(server)
	if err != nil {
		return nil, nil, exit.Usagef("MCP server is not a JSON object: %v; fix the server", err)
	}
	envAt := -1
	for i, k := range keys {
		if k == "env" {
			envAt = i
		}
	}
	if envAt < 0 {
		return server, nil, nil
	}
	ekeys, evals, err := OrderedObject(vals[envAt])
	if err != nil {
		return nil, nil, exit.Usagef("MCP server: env is not a JSON object: %v; fix the server", err)
	}
	for i, k := range ekeys {
		if !envNameRe.MatchString(k) {
			return nil, nil, exit.Usagef("MCP server: env key %q is not a valid variable name (letters, digits, _; not starting with a digit); rename it", k)
		}
		var s string
		if json.Unmarshal(evals[i], &s) == nil && envRefRe.MatchString(s) {
			continue
		}
		evals[i] = json.RawMessage(JSONString("${" + k + "}"))
		names = append(names, k)
	}
	sort.Strings(names)
	var env []string
	for i, k := range ekeys {
		env = append(env, JSONString(k)+":"+string(evals[i]))
	}
	vals[envAt] = json.RawMessage("{" + strings.Join(env, ",") + "}")
	var parts []string
	for i, k := range keys {
		parts = append(parts, JSONString(k)+":"+string(vals[i]))
	}
	return json.RawMessage("{" + strings.Join(parts, ",") + "}"), names, nil
}

var (
	refInRe      = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)
	exactRefRe   = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)
	refNameBadRe = regexp.MustCompile(`[^A-Z0-9]`)
)

// secretHeader reports whether a header name, compared without case, usually carries a secret.
func secretHeader(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case "authorization", "proxy-authorization", "cookie":
		return true
	}
	for _, w := range []string{"key", "token", "secret", "password"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// HeaderRefName is the variable name HeaderRefs uses for header of the server named server:
// <SERVER>_<HEADER> in upper case, every character outside A-Z0-9 turned into _.
func HeaderRefName(server, header string) string {
	n := refNameBadRe.ReplaceAllString(strings.ToUpper(server+"_"+header), "_")
	if n[0] >= '0' && n[0] <= '9' {
		n = "_" + n
	}
	return n
}

// HeaderRefs replaces the value of every header of the server that usually carries a secret
// (Authorization, Proxy-Authorization, Cookie, or a name containing key, token, secret or
// password, without case) by ${<SERVER>_<HEADER>}, unless the value already contains a ${NAME}
// reference. A value starting with "Bearer " or "Basic " keeps that word. Other headers are
// unchanged. It returns the sorted names it used; the rest of the server is unchanged.
func HeaderRefs(server json.RawMessage, name string) (out json.RawMessage, names []string, err error) {
	keys, vals, err := OrderedObject(server)
	if err != nil {
		return nil, nil, exit.Usagef("MCP server is not a JSON object: %v; fix the server", err)
	}
	at := -1
	for i, k := range keys {
		if k == "headers" {
			at = i
		}
	}
	if at < 0 {
		return server, nil, nil
	}
	hkeys, hvals, err := OrderedObject(vals[at])
	if err != nil {
		return nil, nil, exit.Usagef("MCP server: headers is not a JSON object: %v; fix the server", err)
	}
	changed := false
	owner := map[string]string{}
	for i, k := range hkeys {
		if !secretHeader(k) {
			continue
		}
		var s string
		if err := json.Unmarshal(hvals[i], &s); err != nil {
			return nil, nil, exit.Usagef("MCP server: header %q is not a string; fix the server", k)
		}
		if refInRe.MatchString(s) {
			continue
		}
		prefix := ""
		for _, w := range []string{"Bearer ", "Basic "} {
			if len(s) >= len(w) && strings.EqualFold(s[:len(w)], w) {
				prefix = s[:len(w)]
			}
		}
		ref := HeaderRefName(name, k)
		if prev, ok := owner[ref]; ok && prev != k {
			return nil, nil, exit.Usagef("MCP server: headers %q and %q would both use the variable %s; rename one of them", prev, k, ref)
		}
		owner[ref] = k
		hvals[i] = json.RawMessage(JSONString(prefix + "${" + ref + "}"))
		if !slices.Contains(names, ref) {
			names = append(names, ref)
		}
		changed = true
	}
	if !changed {
		return server, nil, nil
	}
	sort.Strings(names)
	var hs []string
	for i, k := range hkeys {
		hs = append(hs, JSONString(k)+":"+string(hvals[i]))
	}
	vals[at] = json.RawMessage("{" + strings.Join(hs, ",") + "}")
	var parts []string
	for i, k := range keys {
		parts = append(parts, JSONString(k)+":"+string(vals[i]))
	}
	return json.RawMessage("{" + strings.Join(parts, ",") + "}"), names, nil
}

// URLPassword reports whether the server's url holds a password (scheme://user:pass@host).
// A password that is exactly a ${NAME} reference is not one.
func URLPassword(server json.RawMessage) bool {
	var obj struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(server, &obj) != nil || obj.URL == "" {
		return false
	}
	u, err := url.Parse(obj.URL)
	if err == nil {
		pw, ok := u.User.Password()
		return ok && !exactRefRe.MatchString(pw)
	}
	// Not parseable: look for user:pass@ in the authority by hand.
	rest := obj.URL
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return false
	}
	_, pw, ok := strings.Cut(rest[:at], ":")
	return ok && !exactRefRe.MatchString(pw)
}
