package kiro

import (
	"encoding/json"
	"errors"
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

const symlinkReason = shared.SymlinkReason

// Import reads the Kiro folder back into items. It only reads.
func (k kiro) Import(env adapter.Env) ([]adapter.Found, []adapter.Skipped, error) {
	root := k.Root(env)
	if env.Project != "" {
		if li, err := os.Lstat(root); err == nil && li.Mode()&fs.ModeSymlink != 0 {
			return nil, []adapter.Skipped{{Path: root, Reason: symlinkReason}}, nil
		}
	}
	fi, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.IsDir()) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", root, err)
	}
	im := &importer{Base: shared.Base{Root: root}}
	if err := im.skills(); err != nil {
		return nil, nil, err
	}
	if err := im.steering(); err != nil {
		return nil, nil, err
	}
	for _, d := range []string{"agents", "hooks", "workflows"} {
		if err := im.native(d); err != nil {
			return nil, nil, err
		}
	}
	if err := im.mcp(); err != nil {
		return nil, nil, err
	}
	shared.SortFound(im.found)
	sort.SliceStable(im.Skipped, func(i, j int) bool { return im.Skipped[i].Path < im.Skipped[j].Path })
	return im.found, im.Skipped, nil
}

type importer struct {
	shared.Base
	found []adapter.Found
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

func (im *importer) steering() error {
	dir := filepath.Join(im.Root, "steering")
	for _, e := range im.ReadDir(dir) {
		p := filepath.Join(dir, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			im.Skip(p, symlinkReason)
			continue
		}
		if e.IsDir() {
			im.Skip(p, "a folder in steering/; Kiro reads only steering/<name>.md")
			continue
		}
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			im.Skip(p, "not a .md file")
			continue
		}
		if !content.ValidName(name) {
			im.Skip(p, "name must match "+content.NameRule+"; rename the file")
			continue
		}
		f, err := shared.ReadFile(p)
		if err != nil {
			return exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", p, err)
		}
		origin := im.Rel(p)
		ins, why := parseSteering(f.Data)
		if why == "" {
			im.found = append(im.found, adapter.Found{
				Item:  content.Item{Kind: content.InstructionKind, Name: name, Origin: origin, Instruction: ins},
				Paths: []string{p},
			})
			continue
		}
		im.Note(p, "imported as a Kiro-only file: "+why+"; shared instructions can only be always-on or have file patterns")
		f.Path = origin
		im.found = append(im.found, adapter.Found{
			Item: content.Item{Kind: content.Native, Name: origin, Harness: "kiro", Origin: origin,
				Files: []content.File{f}},
			Paths: []string{p},
		})
	}
	return nil
}

// parseSteering turns a steering file into an instruction; a non-empty string says why it has no shared form.
func parseSteering(data []byte) (content.Instruction, string) {
	fm := content.ScanFrontmatter(data)
	if !fm.Opened {
		return content.Instruction{Body: fm.Body}, ""
	}
	if fm.Unclosed {
		return content.Instruction{}, "the frontmatter is not closed with ---"
	}
	if len(fm.Items) > 0 {
		return content.Instruction{}, "a list item outside fileMatchPattern"
	}
	var inclusion string
	var globs []string
	hasPattern := false
	for _, f := range fm.Fields {
		key, value := f.Key, f.Value
		switch key {
		case "inclusion":
			v, ok := shared.Unquote(value)
			if !ok || inclusion != "" {
				return content.Instruction{}, "inclusion is not a single plain value"
			}
			inclusion = v
		case "fileMatchPattern":
			if hasPattern {
				return content.Instruction{}, "fileMatchPattern appears twice"
			}
			hasPattern = true
			switch {
			case value == "":
			case strings.HasPrefix(value, "["):
				list, ok := shared.ParseInline(value)
				if !ok {
					return content.Instruction{}, "fileMatchPattern is not a plain list"
				}
				globs = append(globs, list...)
			default:
				g, ok := shared.Unquote(value)
				if !ok || g == "" {
					return content.Instruction{}, "fileMatchPattern is not a plain pattern"
				}
				globs = append(globs, g)
			}
		default:
			return content.Instruction{}, "the frontmatter key " + key + " is not inclusion or fileMatchPattern"
		}
		for _, item := range f.Items {
			if key != "fileMatchPattern" {
				return content.Instruction{}, "a list item outside fileMatchPattern"
			}
			g, ok := shared.Unquote(item.Value)
			if !ok || g == "" {
				return content.Instruction{}, "a fileMatchPattern entry is not a plain pattern"
			}
			globs = append(globs, g)
		}
	}
	if fm.Malformed > 0 {
		return content.Instruction{}, "a frontmatter line is not key: value"
	}
	body := fm.Body
	switch inclusion {
	case "", "always":
		if hasPattern {
			return content.Instruction{}, "it has fileMatchPattern but is not inclusion: fileMatch"
		}
		return content.Instruction{Body: body}, ""
	case "fileMatch":
		if len(globs) == 0 {
			return content.Instruction{}, "inclusion: fileMatch has no fileMatchPattern"
		}
		return content.Instruction{Globs: globs, Body: body}, ""
	}
	return content.Instruction{}, "inclusion: " + inclusion + " is not always or fileMatch"
}

func (im *importer) native(top string) error {
	dir := filepath.Join(im.Root, top)
	if len(im.ReadDir(dir)) == 0 {
		return nil
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
			Item: content.Item{Kind: content.Native, Name: origin, Harness: "kiro", Origin: origin,
				Files: []content.File{f}},
			Paths: []string{p},
		})
		return nil
	})
	if err != nil {
		return exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", dir, err)
	}
	return nil
}

func (im *importer) mcp() error {
	dir := filepath.Join(im.Root, "settings")
	di, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", dir, err)
	}
	if di.Mode()&fs.ModeSymlink != 0 {
		im.Skip(dir, symlinkReason)
		return nil
	}
	if !di.IsDir() {
		return nil
	}
	file := filepath.Join(dir, "mcp.json")
	found, skipped, err := shared.MCPServers(file, shared.MCPSpec{
		Origin:      im.Rel(file),
		IsServerKey: content.IsMCPKey,
		Key: func(key string, raw json.RawMessage) (bool, bool, string) {
			if key != "disabled" {
				return false, false, ""
			}
			var b bool
			if json.Unmarshal(raw, &b) == nil && b {
				return true, false, "disabled"
			}
			return true, true, ""
		},
	})
	if err != nil {
		return err
	}
	im.found = append(im.found, found...)
	im.Skipped = append(im.Skipped, skipped...)
	return nil
}
