// Package kiro is the adapter for the Kiro harness (IDE and CLI share one folder).
package kiro

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

var nativeDirs = []string{"steering", "agents", "hooks", "workflows"}

type kiro struct{}

// New returns the Kiro adapter.
func New() adapter.Adapter { return kiro{} }

func (kiro) Name() string { return "kiro" }

func (kiro) Root(env adapter.Env) string {
	if env.Project != "" {
		return filepath.Join(env.Project, ".kiro")
	}
	if env.Getenv != nil {
		if v := env.Getenv("KIRO_HOME"); v != "" {
			if !filepath.IsAbs(v) {
				v = filepath.Join(env.Home, v)
			}
			return filepath.Clean(v)
		}
	}
	return filepath.Join(env.Home, ".kiro")
}

// mcpFile is the shared file the MCP servers go to.
func (k kiro) mcpFile(env adapter.Env) string {
	return filepath.Join(k.Root(env), "settings", "mcp.json")
}

// EntryKeys: a server is written as it is in the shared repository, with these keys only.
func (k kiro) EntryKeys(env adapter.Env) []adapter.EntryKeys {
	return []adapter.EntryKeys{{Path: k.mcpFile(env), Key: "mcpServers",
		Keys: content.MCPKeys()}}
}

func (k kiro) Render(items []content.Item, env adapter.Env) (adapter.Plan, error) {
	root := k.Root(env)
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
				Path:   filepath.Join(root, "steering", it.Name+".md"),
				Data:   steering(it.Instruction),
				Origin: origin,
			})
		case content.MCP:
			plan.Entries = append(plan.Entries, adapter.Entry{
				Path:   k.mcpFile(env),
				Key:    "mcpServers",
				Name:   it.Name,
				Value:  it.Server,
				Origin: origin,
			})
		case content.Native:
			if it.Harness != "kiro" {
				continue
			}
			if !allowedNative(it.Name) {
				return adapter.Plan{}, exit.Usagef("%s: kiro/%s: only files below %s are copied for Kiro; move it there or remove it",
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

func steering(ins content.Instruction) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	if len(ins.Globs) == 0 {
		b.WriteString("inclusion: always\n")
	} else {
		quoted := make([]string, len(ins.Globs))
		for i, g := range ins.Globs {
			quoted[i] = content.JSONString(g)
		}
		b.WriteString("inclusion: fileMatch\nfileMatchPattern: [" + strings.Join(quoted, ", ") + "]\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(ins.Body, "\n"))
	b.WriteString("\n")
	return []byte(b.String())
}
