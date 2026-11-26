// Package importer holds the harness-neutral parts of reading a harness folder back into items:
// the skip list, directory and file reading with the link rule, the skills folder walk and the
// small parsers shared by the adapters' Import.
package importer

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

// SymlinkReason is the skip reason of a symbolic link.
const SymlinkReason = "symbolic link"

// Base is the skip list of one import run and the root that origins are relative to.
type Base struct {
	Root    string
	Skipped []adapter.Skipped
}

// Skip records a path that is not imported and why.
func (b *Base) Skip(p, reason string) {
	b.Skipped = append(b.Skipped, adapter.Skipped{Path: p, Reason: reason})
}

// Note records a path that is imported with something left out, and what.
func (b *Base) Note(p, reason string) {
	b.Skipped = append(b.Skipped, adapter.Skipped{Path: p, Reason: reason, Partial: true})
}

// DroppedReason is the reason of a server that is imported without some keys.
func DroppedReason(name string, keys []string) string {
	return "server " + name + ": imported without " + strings.Join(keys, ", ") + " (not shared: every developer sets it in the harness)"
}

// Rel is p relative to the root, with slashes.
func (b *Base) Rel(p string) string {
	r, err := filepath.Rel(b.Root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// ReadDir lists dir; a missing directory gives nothing, a symbolic link is skipped, an
// unreadable directory is skipped with "cannot read: ".
func (b *Base) ReadDir(dir string) []os.DirEntry {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		b.Skip(dir, "cannot read: "+err.Error())
		return nil
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		b.Skip(dir, SymlinkReason)
		return nil
	}
	if !fi.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.Skip(dir, "cannot read: "+err.Error())
		return nil
	}
	return entries
}

// ReadFile reads one file with its executable bit; the Path of the result is not set.
func ReadFile(p string) (content.File, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return content.File{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return content.File{}, err
	}
	return content.File{Data: data, Exec: fi.Mode()&0o100 != 0}, nil
}

// Skills walks the skills folder dir. Origins are relative to root. A linked folder or skill, a
// bad name and a skill without SKILL.md are skipped; an unreadable file is an error.
func Skills(root, dir string) ([]adapter.Found, []adapter.Skipped, error) {
	b := &Base{Root: root}
	var found []adapter.Found
	for _, e := range b.ReadDir(dir) {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.Type()&fs.ModeSymlink != 0:
			b.Skip(p, SymlinkReason)
			continue
		case !e.IsDir():
			continue
		case !content.ValidName(e.Name()):
			b.Skip(p, "name must match "+content.NameRule+"; rename the folder")
			continue
		}
		var files []content.File
		var paths []string
		err := filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if fp == p {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				b.Skip(fp, SymlinkReason)
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			f, err := ReadFile(fp)
			if err != nil {
				return err
			}
			r, _ := filepath.Rel(p, fp)
			f.Path = filepath.ToSlash(r)
			files = append(files, f)
			paths = append(paths, fp)
			return nil
		})
		if err != nil {
			return nil, nil, exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", p, err)
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		sort.Strings(paths)
		hasSkill := false
		for _, f := range files {
			if f.Path == "SKILL.md" {
				hasSkill = true
			}
		}
		if !hasSkill {
			b.Skip(p, "SKILL.md is missing")
			continue
		}
		found = append(found, adapter.Found{
			Item:  content.Item{Kind: content.Skill, Name: e.Name(), Origin: b.Rel(p), Files: files},
			Paths: paths,
		})
	}
	return found, b.Skipped, nil
}

// Unquote strips one pair of double or single quotes.
func Unquote(s string) (string, bool) {
	if s == "" {
		return "", true
	}
	switch s[0] {
	case '"':
		var v string
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return "", false
		}
		return v, true
	case '\'':
		if len(s) < 2 || s[len(s)-1] != '\'' {
			return "", false
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), true
	}
	return s, true
}

// ParseInline parses an inline list such as ["a", 'b', c]; empty items and an empty list fail.
func ParseInline(s string) ([]string, bool) {
	var list []string
	if err := json.Unmarshal([]byte(s), &list); err == nil {
		for _, g := range list {
			if g == "" {
				return nil, false
			}
		}
		return list, len(list) > 0
	}
	if !strings.HasSuffix(s, "]") {
		return nil, false
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, false
	}
	parts, ok := splitInline(inner)
	if !ok {
		return nil, false
	}
	for _, part := range parts {
		g, ok := Unquote(strings.TrimSpace(part))
		if !ok || g == "" {
			return nil, false
		}
		list = append(list, g)
	}
	return list, true
}

// splitInline splits at commas outside quotes and outside {...}, the rule of the source parser.
func splitInline(inner string) ([]string, bool) {
	var items []string
	var quote byte
	depth, start := 0, 0
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			if depth > 0 {
				depth--
			}
		case c == ',' && depth == 0:
			items = append(items, inner[start:i])
			start = i + 1
		}
	}
	return append(items, inner[start:]), quote == 0 && depth == 0
}

// SortFound sorts found in the order of content.SortItems; equal items keep their order.
func SortFound(found []adapter.Found) {
	sort.SliceStable(found, func(i, j int) bool { return content.ItemLess(found[i].Item, found[j].Item) })
}

// QuoteName is s as a JSON string.
func QuoteName(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
