package cli

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
)

// manifestData is the livery.json import writes into a new shared repository folder.
const manifestData = "{\"schema\": 1}\n"

// importFile is one file import wants below DIR.
type importFile struct {
	rel      string // slash-separated, relative to DIR
	data     []byte
	exec     bool
	original string // the file it was read from, for messages
}

// importCmd implements `livery import`: it reads a harness folder and writes its content as a
// layer of a shared repository folder. It only reads the harness folder, the config, lock and cache, and
// writes nothing outside DIR. It never runs git.
func importCmd(args []string, stdin io.Reader, stdout io.Writer, env adapter.Env, interactive bool) error {
	o, err := parseImportFlags(args)
	if err != nil {
		return err
	}
	in := bufio.NewReader(stdin)
	env, err = scoped(env, o.project, true)
	if err != nil {
		return err
	}
	ads, err := chooseHarnesses(o, env, in, stdout, interactive)
	if err != nil {
		return err
	}
	dir, err := chooseTarget(o, env, in, stdout, interactive)
	if err != nil {
		return err
	}
	layerDir := "base"
	if o.team != "" {
		layerDir = "teams/" + o.team
	}

	rd, err := readHarnesses(env, ads, o.project)
	if err != nil {
		return err
	}
	if len(rd.found) == 0 && len(rd.skipped) == 0 {
		_, _ = fmt.Fprintf(stdout, "nothing to import from %s\n", rd.root)
		return nil
	}
	if err := pruneRead(env, rd, stdout); err != nil {
		return err
	}
	if err := mergeSame(rd, stdout); err != nil {
		return err
	}
	selected, err := selectItems(o, rd, in, stdout, interactive)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		printSkipped(stdout, rd.skipped)
		_, _ = fmt.Fprintf(stdout, "nothing to import from %s\n", rd.root)
		return nil
	}
	conv, err := convertItems(selected, dir, layerDir)
	if err != nil {
		return err
	}
	write, same, err := checkTarget(stdout, dir, conv)
	if err != nil {
		return err
	}
	if err := writeImport(dir, write, o.plan); err != nil {
		return err
	}
	reportImport(stdout, o, dir, rd, conv, write, same)
	return nil
}

// itemLabel is how --only and the prompt name an item: kind/name, or harness/path for a native file.
func itemLabel(it content.Item) string {
	switch it.Kind {
	case content.Native:
		return it.Harness + "/" + it.Name
	case content.InstructionKind:
		return "instruction/" + it.Name
	}
	return string(it.Kind) + "/" + it.Name
}

// dropInstalled removes the items livery installed (by the lock) and returns how many it removed.
func dropInstalled(env adapter.Env, found []adapter.Found) ([]adapter.Found, int, error) {
	installed, err := installedBy(env)
	if err != nil {
		return found, 0, err
	}
	var kept []adapter.Found
	n := 0
	for _, f := range found {
		if installed(f) {
			n++
		} else {
			kept = append(kept, f)
		}
	}
	return kept, n, nil
}

// installedBy returns whether livery installed a found item, by the lock (none without a lock).
func installedBy(env adapter.Env) (func(adapter.Found) bool, error) {
	scope := lock.ScopeOf(env)
	l, ok, err := lock.ReadIn(paths.LockFile(env), scope)
	if err != nil || !ok {
		return func(adapter.Found) bool { return false }, err
	}
	return func(f adapter.Found) bool {
		if f.Path != "" {
			p, _ := scope.Store(f.Path)
			for _, m := range l.Merged {
				if m.Path == p && m.Key == f.Key && !m.List {
					if _, ok := m.Entries[f.Name]; ok {
						return true
					}
				}
			}
			return false
		}
		if len(f.Paths) == 0 {
			return false
		}
		for _, p := range f.Paths {
			k, _ := scope.Store(p)
			if _, ok := l.Files[k]; !ok {
				return false
			}
		}
		return true
	}, nil
}

// originalOf is the harness file the layer file rel (relative to the layer) was read from.
func originalOf(f adapter.Found, rel string) string {
	if f.Item.Kind == content.Skill {
		inSkill := strings.TrimPrefix(rel, path.Join("skills", f.Item.Name)+"/")
		suffix := string(filepath.Separator) + filepath.FromSlash(inSkill)
		for _, p := range f.Paths {
			if strings.HasSuffix(p, suffix) {
				return p
			}
		}
		// A file this harness does not have: where it would be, next to SKILL.md.
		for _, p := range f.Paths {
			if filepath.Base(p) == "SKILL.md" {
				return filepath.Join(filepath.Dir(p), filepath.FromSlash(inSkill))
			}
		}
	}
	if len(f.Paths) > 0 {
		return f.Paths[0]
	}
	return f.Item.Origin
}

// linkBelow returns the first path element of rel below dir that is a symbolic link, or "".
func linkBelow(dir, rel string) string {
	p := dir
	for _, el := range strings.Split(rel, "/") {
		p = filepath.Join(p, el)
		st, err := os.Lstat(p)
		if err != nil {
			return ""
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return p
		}
	}
	return ""
}

// writeNew creates the file f below dir, with its folders; it never replaces a file.
func writeNew(dir string, f importFile) error {
	target := filepath.Join(dir, filepath.FromSlash(f.rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return exit.Environmentf("import: cannot create %s: %v; fix the permissions, then run livery import again", filepath.Dir(target), err)
	}
	mode := os.FileMode(0o644)
	if f.exec {
		mode = 0o755
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return exit.Environmentf("import: cannot create %s: %v; fix the problem, then run livery import again", target, err)
	}
	_, werr := out.Write(f.data)
	if werr == nil {
		werr = out.Chmod(mode)
	}
	if cerr := out.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return exit.Environmentf("import: cannot write %s: %v; fix the problem, then run livery import again", target, werr)
	}
	return nil
}

// shellQuote quotes s for a shell command line when it needs it.
func shellQuote(s string) string {
	plain := func(r rune) bool {
		return strings.ContainsRune("/.-_~+:,@", r) || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !plain(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
