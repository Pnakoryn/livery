// Package claude is the adapter for the Claude Code harness.
package claude

import (
	"bytes"
	"encoding/json"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

// nativeDirs are the folders below claude/ in a layer that are copied as they are.
var nativeDirs = []string{"agents"}

type claude struct{}

// New returns the Claude Code adapter.
func New() adapter.Adapter { return claude{} }

func (claude) Name() string { return "claude" }

func configDir(env adapter.Env) string {
	if env.Project != "" || env.Getenv == nil {
		return ""
	}
	v := env.Getenv("CLAUDE_CONFIG_DIR")
	if v == "" {
		return ""
	}
	if !filepath.IsAbs(v) {
		v = filepath.Join(env.Home, v)
	}
	return filepath.Clean(v)
}

func (claude) Root(env adapter.Env) string {
	if env.Project != "" {
		return filepath.Join(env.Project, ".claude")
	}
	if d := configDir(env); d != "" {
		return d
	}
	return filepath.Join(env.Home, ".claude")
}

// stateFile is the shared file the MCP servers go to.
func stateFile(env adapter.Env) string {
	if env.Project != "" {
		return filepath.Join(env.Project, ".mcp.json")
	}
	if d := configDir(env); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	return filepath.Join(env.Home, ".claude.json")
}

// EntryKeys: every key server can write.
func (claude) EntryKeys(env adapter.Env) []adapter.EntryKeys {
	return []adapter.EntryKeys{{Path: stateFile(env), Key: "mcpServers",
		Keys: append([]string{"type"}, content.MCPKeys()...)}}
}

func (c claude) Render(items []content.Item, env adapter.Env) (adapter.Plan, error) {
	root := c.Root(env)
	state := stateFile(env)
	plan := adapter.Plan{Root: root}
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
				Path:   filepath.Join(root, "rules", it.Name+".md"),
				Data:   rule(it.Instruction),
				Origin: origin,
			})
		case content.MCP:
			val, err := server(it.Server)
			if err != nil {
				return adapter.Plan{}, exit.Usagef("%s: %s: %v; fix the file", it.Layer, it.Origin, err)
			}
			plan.Entries = append(plan.Entries, adapter.Entry{
				Path:   state,
				Key:    "mcpServers",
				Name:   it.Name,
				Value:  val,
				Origin: origin,
			})
		case content.Native:
			if it.Harness != "claude" {
				continue
			}
			if !allowedNative(it.Name) {
				return adapter.Plan{}, exit.Usagef("%s: claude/%s: only files below %s are copied for Claude Code; move it there or remove it",
					it.Layer, it.Name, strings.Join(nativeDirs, "/, ")+"/")
			}
			var data []byte
			var exec bool
			if len(it.Files) > 0 {
				data, exec = it.Files[0].Data, it.Files[0].Exec
			}
			plan.Files = append(plan.Files, adapter.File{
				Path:   filepath.Join(root, filepath.FromSlash(it.Name)),
				Data:   data,
				Exec:   exec,
				Origin: origin,
			})
		}
	}
	sort.SliceStable(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	sort.SliceStable(plan.Entries, func(i, j int) bool { return plan.Entries[i].Name < plan.Entries[j].Name })
	return plan, nil
}

func allowedNative(name string) bool {
	name = path.Clean(name)
	for _, d := range nativeDirs {
		if strings.HasPrefix(name, d+"/") {
			return true
		}
	}
	return false
}

func rule(ins content.Instruction) []byte {
	var b strings.Builder
	if len(ins.Globs) > 0 {
		b.WriteString("---\npaths:\n")
		for _, g := range ins.Globs {
			b.WriteString("  - " + content.JSONString(g) + "\n")
		}
		b.WriteString("---\n\n")
	}
	b.WriteString(strings.TrimRight(ins.Body, "\n"))
	b.WriteString("\n")
	return []byte(b.String())
}

// server adds the "type" Claude Code requires and re-encodes compact with sorted keys.
func server(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, exit.Usagef("must be a JSON object")
	}
	if _, ok := obj["type"]; !ok {
		if _, ok := obj["command"]; ok {
			obj["type"] = json.RawMessage(`"stdio"`)
		} else if _, ok := obj["url"]; ok {
			obj["type"] = json.RawMessage(`"http"`)
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}
