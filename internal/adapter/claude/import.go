package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	shared "github.com/Pnakoryn/livery/internal/adapter/importer"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

const (
	symlinkReason = shared.SymlinkReason
	renameFile    = "name must match " + content.NameRule + "; rename the file"
)

// Import reads the Claude Code folder back into items. It only reads.
func (c claude) Import(env adapter.Env) ([]adapter.Found, []adapter.Skipped, error) {
	root := c.Root(env)
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
	im := &importer{Base: shared.Base{Root: root}}
	if linkedRoot {
		im.Skip(root, symlinkReason)
	}
	if env.Project != "" {
		im.base = env.Project
		if rootOK {
			if err := im.skills(); err != nil {
				return nil, nil, err
			}
			if err := im.rules(); err != nil {
				return nil, nil, err
			}
			if err := im.natives(); err != nil {
				return nil, nil, err
			}
			for _, n := range []string{"settings.json", "settings.local.json", "commands"} {
				im.seen(n, "not supported by livery yet")
			}
		}
		const notes = "project notes; livery does not manage them"
		im.seenAt(filepath.Join(env.Project, "CLAUDE.md"), notes)
		im.seenAt(filepath.Join(root, "CLAUDE.md"), notes)
		if err := im.mcp(filepath.Join(env.Project, ".mcp.json")); err != nil {
			return nil, nil, err
		}
	} else {
		state := filepath.Join(env.Home, ".claude.json")
		if d := configDir(env); d != "" {
			state = filepath.Join(d, ".claude.json")
		}
		if err := im.skills(); err != nil {
			return nil, nil, err
		}
		if err := im.rules(); err != nil {
			return nil, nil, err
		}
		im.seen("CLAUDE.md", "personal notes; copy what the team needs into an instruction")
		if err := im.natives(); err != nil {
			return nil, nil, err
		}
		for _, n := range []string{"settings.json", "commands", "hooks"} {
			im.seen(n, "not supported by livery yet")
		}
		if err := im.mcp(state); err != nil {
			return nil, nil, err
		}
	}
	shared.SortFound(im.found)
	sort.SliceStable(im.Skipped, func(i, j int) bool { return im.Skipped[i].Path < im.Skipped[j].Path })
	return im.found, im.Skipped, nil
}

type importer struct {
	shared.Base
	base  string // project root; Found.Origin of servers is relative to it
	found []adapter.Found
}

// seen lists an entry of the root that is present and not imported.
func (im *importer) seen(name, reason string) {
	im.seenAt(filepath.Join(im.Root, name), reason)
}

func (im *importer) seenAt(p, reason string) {
	fi, err := os.Lstat(p)
	if err != nil {
		return
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		reason = symlinkReason
	}
	im.Skip(p, reason)
}

func (im *importer) skills() error {
	found, skipped, err := shared.Skills(im.Root, filepath.Join(im.Root, "skills"))
	if err != nil {
		return err
	}
	im.found = append(im.found, found...)
	im.Skipped = append(im.Skipped, skipped...)
	return nil
}

func (im *importer) rules() error {
	dir := filepath.Join(im.Root, "rules")
	for _, e := range im.ReadDir(dir) {
		p := filepath.Join(dir, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			im.Skip(p, symlinkReason)
			continue
		}
		if e.IsDir() {
			im.Skip(p, "rules in subfolders are not imported; move it up")
			continue
		}
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			continue
		}
		if !content.ValidName(name) {
			im.Skip(p, renameFile)
			continue
		}
		f, err := shared.ReadFile(p)
		if err != nil {
			return exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", p, err)
		}
		ins, reason := parseRule(f.Data)
		if reason != "" {
			im.Skip(p, reason)
			continue
		}
		im.found = append(im.found, adapter.Found{
			Item:  content.Item{Kind: content.InstructionKind, Name: name, Origin: im.Rel(p), Instruction: ins},
			Paths: []string{p},
		})
	}
	return nil
}

// natives imports every file below the nativeDirs folders as a Claude Code-only item.
func (im *importer) natives() error {
	for _, top := range nativeDirs {
		dir := filepath.Join(im.Root, top)
		if len(im.ReadDir(dir)) == 0 {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == dir {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				im.Skip(p, symlinkReason)
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			f, err := shared.ReadFile(p)
			if err != nil {
				return err
			}
			origin := im.Rel(p)
			f.Path = origin
			im.found = append(im.found, adapter.Found{
				Item: content.Item{Kind: content.Native, Name: origin, Harness: "claude", Origin: origin,
					Files: []content.File{f}},
				Paths: []string{p},
			})
			return nil
		})
		if err != nil {
			return exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", dir, err)
		}
	}
	return nil
}

// parseRule turns a rule file into an instruction; a non-empty reason means it is skipped.
func parseRule(data []byte) (content.Instruction, string) {
	fm := content.ScanFrontmatter(data)
	if !fm.Opened {
		return content.Instruction{Body: fm.Body}, ""
	}
	if fm.Unclosed {
		return content.Instruction{}, "frontmatter is not closed with ---; fix the file"
	}
	if len(fm.Items) > 0 {
		return content.Instruction{}, fmt.Sprintf("frontmatter not understood: list item outside paths (line %d)", fm.Items[0].Line)
	}
	var globs []string
	hasPaths := false
	for _, f := range fm.Fields {
		n, key, value := f.Line, f.Key, f.Value
		if key != "paths" {
			return content.Instruction{}, fmt.Sprintf("frontmatter key on line %d is not paths; livery shares only paths", n)
		}
		if hasPaths {
			return content.Instruction{}, fmt.Sprintf("frontmatter key paths appears twice (line %d)", n)
		}
		hasPaths = true
		switch {
		case value == "":
		case strings.HasPrefix(value, "["):
			list, ok := shared.ParseInline(value)
			if !ok {
				return content.Instruction{}, fmt.Sprintf("frontmatter not understood: paths list (line %d)", n)
			}
			globs = append(globs, list...)
		default:
			g, ok := shared.Unquote(value)
			if !ok || g == "" {
				return content.Instruction{}, fmt.Sprintf("frontmatter not understood: paths (line %d)", n)
			}
			globs = append(globs, g)
		}
		for _, item := range f.Items {
			if value != "" { // list items only follow an empty paths:
				return content.Instruction{}, fmt.Sprintf("frontmatter not understood: list item outside paths (line %d)", item.Line)
			}
			g, ok := shared.Unquote(item.Value)
			if !ok || g == "" {
				return content.Instruction{}, fmt.Sprintf("frontmatter not understood: bad glob in paths (line %d)", item.Line)
			}
			globs = append(globs, g)
		}
	}
	if fm.Malformed > 0 {
		return content.Instruction{}, fmt.Sprintf("frontmatter not understood: line %d is not key: value", fm.Malformed)
	}
	if hasPaths && len(globs) == 0 {
		return content.Instruction{}, "frontmatter key paths has no globs"
	}
	return content.Instruction{Globs: globs, Body: fm.Body}, ""
}

func (im *importer) mcp(file string) error {
	found, skipped, err := shared.MCPServers(file, shared.MCPSpec{
		Origin:      im.serverOrigin(file),
		IsServerKey: content.IsMCPKey,
		Key: func(key string, raw json.RawMessage) (bool, bool, string) {
			if key != "type" {
				return false, false, ""
			}
			var t string
			if json.Unmarshal(raw, &t) != nil {
				return true, false, "type " + string(raw) + " is not supported by livery yet"
			}
			if t != "stdio" && t != "http" {
				return true, false, "type " + t + " is not supported by livery yet"
			}
			return true, false, ""
		},
	})
	if err != nil {
		return err
	}
	im.found = append(im.found, found...)
	im.Skipped = append(im.Skipped, skipped...)
	return nil
}
func (im *importer) serverOrigin(file string) string {
	if im.base != "" {
		if r, err := filepath.Rel(im.base, file); err == nil {
			return filepath.ToSlash(r)
		}
	}
	return filepath.Base(file)
}
