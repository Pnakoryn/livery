// Package opencode is the adapter for the opencode harness.
package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

type opencode struct{}

// New returns the opencode adapter.
func New() adapter.Adapter { return opencode{} }

func (opencode) Name() string { return "opencode" }

func (opencode) Root(env adapter.Env) string {
	if env.Project != "" {
		return filepath.Join(env.Project, ".opencode")
	}
	if env.Getenv != nil {
		if v := env.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(v) {
			return filepath.Join(filepath.Clean(v), "opencode")
		}
	}
	return filepath.Join(env.Home, ".config", "opencode")
}

func configFile(root string) string {
	jsonc := filepath.Join(root, "opencode.jsonc")
	if _, err := os.Stat(jsonc); err == nil {
		return jsonc
	}
	return filepath.Join(root, "opencode.json")
}

// projectConfigFile is the first existing project config, else <project>/opencode.json.
func projectConfigFile(project string) string {
	for _, p := range []string{
		filepath.Join(project, "opencode.jsonc"),
		filepath.Join(project, "opencode.json"),
		filepath.Join(project, ".opencode", "opencode.jsonc"),
		filepath.Join(project, ".opencode", "opencode.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(project, "opencode.json")
}

// glob is the instruction folder's glob, with ~/ when the root is below the home folder.
func glob(root, home string) string {
	g := filepath.Join(root, "livery", "instructions", "*.md")
	if home != "" {
		if rel, err := filepath.Rel(home, g); err == nil && rel != "." && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return g
}

// cfgFile is the config file livery writes its entries to.
func (o opencode) cfgFile(env adapter.Env) string {
	if env.Project != "" {
		return projectConfigFile(env.Project)
	}
	return configFile(o.Root(env))
}

// EntryKeys: every key server can write.
func (o opencode) EntryKeys(env adapter.Env) []adapter.EntryKeys {
	return []adapter.EntryKeys{{Path: o.cfgFile(env), Key: "mcp.servers",
		Keys: []string{"type", "command", "environment", "url", "headers"}}}
}

func (o opencode) Render(items []content.Item, env adapter.Env) (adapter.Plan, error) {
	root := o.Root(env)
	cfg := o.cfgFile(env)
	globText := glob(root, env.Home)
	if env.Project != "" {
		globText = ".opencode/livery/instructions/*.md"
	}
	plan := adapter.Plan{Root: root}
	hasInstruction := false
	var origins string
	for _, it := range items {
		origin := it.Layer + ": " + it.Origin
		switch it.Kind {
		case content.Skill:
			for _, f := range it.Files {
				plan.Files = append(plan.Files, adapter.File{
					Path:   filepath.Join(root, "skills", it.Name, filepath.FromSlash(f.Path)),
					Data:   f.Data,
					Exec:   f.Exec,
					Origin: origin,
				})
			}
		case content.InstructionKind:
			plan.Files = append(plan.Files, adapter.File{
				Path:   filepath.Join(root, "livery", "instructions", it.Name+".md"),
				Data:   []byte(strings.TrimRight(it.Instruction.Body, "\n") + "\n"),
				Origin: origin,
			})
			if !hasInstruction {
				origins = origin
			}
			hasInstruction = true
			if len(it.Instruction.Globs) > 0 {
				plan.Notes = append(plan.Notes, fmt.Sprintf(
					"opencode: instruction %s applies to every file (opencode has no file patterns)", it.Name))
			}
		case content.MCP:
			val, err := server(it.Server)
			if err != nil {
				return adapter.Plan{}, exit.Usagef("%s: %s: %v; fix the file", it.Layer, it.Origin, err)
			}
			plan.Entries = append(plan.Entries, adapter.Entry{
				Path:   cfg,
				Key:    "mcp.servers",
				Name:   it.Name,
				Value:  val,
				Origin: origin,
			})
		case content.Native:
			if it.Harness == "opencode" {
				return adapter.Plan{}, exit.Usagef("%s: opencode/%s: native files for opencode are not supported yet; remove it",
					it.Layer, it.Name)
			}
		}
	}
	if hasInstruction {
		plan.Entries = append(plan.Entries, adapter.Entry{
			Path:   cfg,
			Key:    "instructions",
			Name:   globText,
			List:   true,
			Origin: origins,
		})
	}
	sort.SliceStable(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	sort.SliceStable(plan.Entries, func(i, j int) bool { return plan.Entries[i].Name < plan.Entries[j].Name })
	sort.Strings(plan.Notes)
	return plan, nil
}

var varRe = regexp.MustCompile(`\$\{([^}]*)\}`)
var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// convert rewrites ${NAME} to {env:NAME}; any other ${...} form is an error.
func convert(s string) (string, error) {
	var bad string
	out := varRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[2 : len(m)-1]
		if !nameRe.MatchString(inner) {
			if bad == "" {
				bad = m
			}
			return m
		}
		return "{env:" + inner + "}"
	})
	if bad != "" {
		return "", exit.Usagef("%s is not supported, opencode has no default values; use ${NAME} only", bad)
	}
	return out, nil
}

func convertAll(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return convert(x)
	case []any:
		for i := range x {
			r, err := convertAll(x[i])
			if err != nil {
				return nil, err
			}
			x[i] = r
		}
	case map[string]any:
		for k := range x {
			r, err := convertAll(x[k])
			if err != nil {
				return nil, err
			}
			x[k] = r
		}
	}
	return v, nil
}

func strMap(v any, what string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, exit.Usagef("%s must be a JSON object", what)
	}
	return m, nil
}

// server turns a livery server into opencode's shape, compact with sorted keys.
func server(raw json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var in map[string]any
	if err := dec.Decode(&in); err != nil || in == nil {
		return nil, exit.Usagef("must be a JSON object")
	}
	res := map[string]any{}
	switch {
	case in["command"] != nil:
		cmd, ok := in["command"].(string)
		if !ok {
			return nil, exit.Usagef("command must be a string")
		}
		arr := []any{cmd}
		if a, ok := in["args"]; ok {
			list, ok := a.([]any)
			if !ok {
				return nil, exit.Usagef("args must be an array")
			}
			arr = append(arr, list...)
		}
		res["type"] = "local"
		res["command"] = arr
		if e, ok := in["env"]; ok {
			m, err := strMap(e, "env")
			if err != nil {
				return nil, err
			}
			if len(m) > 0 {
				res["environment"] = m
			}
		}
	case in["url"] != nil:
		res["type"] = "remote"
		res["url"] = in["url"]
		if h, ok := in["headers"]; ok {
			m, err := strMap(h, "headers")
			if err != nil {
				return nil, err
			}
			if len(m) > 0 {
				res["headers"] = m
			}
		}
	default:
		return nil, exit.Usagef("needs command or url")
	}
	if _, err := convertAll(res); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(res); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}
