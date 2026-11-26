package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/secrets"
)

// CheckLine is one result line of Check. Status is "ok", "info" or "finding".
type CheckLine struct {
	Status, Check, Detail, Fix string
}

// Check validates the working tree of a shared repository in dir: manifest, layers, rendering
// for every adapter and a scan for secrets. A failing check does not stop the later ones, except
// a missing livery.json (the folder is no shared repository): that is the only line returned.
// It reads only; it uses no config, lock, cache or git.
func Check(env adapter.Env, dir string) []CheckLine {
	var out []CheckLine
	add := func(status, check, detail, fix string) {
		out = append(out, CheckLine{status, check, detail, fix})
	}
	fsys := os.DirFS(dir)

	// 1. manifest
	if _, err := fs.Stat(fsys, "livery.json"); errors.Is(err, fs.ErrNotExist) {
		return []CheckLine{{"finding", "manifest", dir + " is not a shared repository: livery.json is missing",
			"pass the folder of a shared repository; to check what is installed on this machine run livery doctor; to start a shared repository run livery import --help"}}
	}
	if _, err := content.ReadManifest(fsys); err != nil {
		add("finding", "manifest", exit.OneLine(err, "; "), "")
	} else {
		add("ok", "manifest", "livery.json", "")
	}

	// 2. layers
	load := func(rel, layer string) ([]content.Item, bool) {
		items, err := loadDir(filepath.Join(dir, filepath.FromSlash(rel)), layer, "livery check")
		if err != nil {
			add("finding", "layer "+layer, exit.OneLine(err, "; "), "")
			return nil, false
		}
		add("ok", "layer "+layer, fmt.Sprintf("%d items", len(items)), "")
		return items, true
	}
	base, baseOK := load("base", "base")
	type team struct {
		name  string
		items []content.Item
	}
	var teams []team
	if entries, err := os.ReadDir(filepath.Join(dir, "teams")); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			add("finding", "layer teams", exit.OneLine(err, "; "), "fix the permissions of teams/ or remove it")
		}
	} else {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if !content.ValidName(e.Name()) {
				add("finding", "layer team:"+e.Name(), "teams/"+e.Name()+" is not a valid team name", "rename it to lowercase letters, digits and dashes, starting with a letter or digit")
				continue
			}
			if items, ok := load(path.Join("teams", e.Name()), "team:"+e.Name()); ok {
				teams = append(teams, team{e.Name(), items})
			}
		}
	}

	// 3. render
	names := Names()
	unknown := map[string]bool{}
	scan := func(items []content.Item) []content.Item {
		var keep []content.Item
		for _, it := range items {
			if it.Kind == content.Native && !slices.Contains(names, it.Harness) {
				unknown[it.Harness] = true
				continue
			}
			keep = append(keep, it)
		}
		return keep
	}
	if baseOK {
		base = scan(base)
	}
	for i := range teams {
		teams[i].items = scan(teams[i].items)
	}
	for _, a := range Adapters() {
		check := "render " + a.Name()
		var err error
		var notes []string
		addNotes := func(ns []string) {
			for _, n := range ns {
				n = strings.TrimPrefix(n, a.Name()+": ")
				if !slices.Contains(notes, n) {
					notes = append(notes, n)
				}
			}
		}
		if baseOK {
			var ns []string
			ns, err = renderCheck(a, base, env)
			addNotes(ns)
		}
		n := 1
		for _, t := range teams {
			if err != nil {
				break
			}
			var items []content.Item
			items, _, err = content.Combine(base, [][]content.Item{t.items}, nil, true)
			if err == nil {
				var ns []string
				ns, err = renderCheck(a, items, env)
				addNotes(ns)
			}
			if err != nil {
				err = fmt.Errorf("with team %s: %w", t.name, err)
			}
			n++
		}
		if err != nil {
			add("finding", check, exit.OneLine(err, "; "), "")
		} else {
			add("ok", check, fmt.Sprintf("base and %d teams render", len(teams)), "")
		}
		for _, n := range notes {
			add("info", check, n, "")
		}
	}
	var un []string
	for h := range unknown {
		un = append(un, h)
	}
	sort.Strings(un)
	for _, h := range un {
		add("finding", "render "+h, "no adapter for harness "+h, "the registered harnesses: "+strings.Join(names, ", ")+"; remove or move the folder")
	}

	// 4. secrets
	return append(out, checkSecrets(dir)...)
}

// renderCheck renders items and rejects destinations wanted twice, as Prepare would; it
// looks at no installed file.
func renderCheck(a adapter.Adapter, items []content.Item, env adapter.Env) ([]string, error) {
	plan, err := a.Render(items, env)
	if err != nil {
		return nil, err
	}
	if _, _, err = apply.WantedFiles([]adapter.Plan{plan}); err != nil {
		return nil, err
	}
	return plan.Notes, nil
}

func checkSecrets(dir string) []CheckLine {
	var out []CheckLine
	add := func(status, check, detail, fix string) {
		out = append(out, CheckLine{status, check, detail, fix})
	}
	allow := secrets.Allow{}
	if data, err := os.ReadFile(filepath.Join(dir, "secrets-allow")); err == nil {
		allow, err = secrets.ParseAllow("secrets-allow", data)
		if err != nil {
			allow = secrets.Allow{}
			add("finding", "secrets", exit.OneLine(err, "; "), "")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		add("finding", "secrets", "secrets-allow: "+exit.OneLine(err, "; "), "fix the file's permissions")
	}
	files, found := 0, 0
	// WalkDir does not follow a linked root; the other checks do, so scan the real folder.
	root := dir
	if r, rerr := filepath.EvalSymlinks(dir); rerr == nil {
		root = r
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if err != nil {
			found++
			add("finding", "secrets", rel+": cannot read: "+exit.OneLine(err, "; "), "fix the permissions")
			return nil
		}
		if d.IsDir() {
			if rel == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		used := rel == "livery.json" || rel == "base" || rel == "teams" || strings.HasPrefix(rel, "base/") || strings.HasPrefix(rel, "teams/")
		if d.Type()&fs.ModeSymlink != 0 {
			if !used {
				return nil // install ignores it; a link is never read
			}
			found++
			add("finding", "links", fmt.Sprintf("%q in the shared repository is a link; links are not allowed, replace it with a regular file", rel), "")
			return nil
		}
		if !d.Type().IsRegular() {
			if !used {
				return nil
			}
			found++
			add("finding", "links", fmt.Sprintf("%q in the shared repository has an unsupported file type; remove it", rel), "")
			return nil
		}
		if rel == "secrets-allow" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			found++
			add("finding", "secrets", rel+": cannot read: "+exit.OneLine(err, "; "), "fix the permissions")
			return nil
		}
		files++
		for _, f := range secrets.Scan(rel, data) {
			if allow.Allows(f) {
				continue
			}
			found++
			if f.Rule == secrets.UnscannedBinary {
				add("finding", "secrets", f.Path+": binary file, not scanned",
					fmt.Sprintf("if it belongs in the shared repository, add the line \"%s %s\" to secrets-allow; otherwise remove it", f.Path, f.Fingerprint))
				continue
			}
			add("finding", "secrets", fmt.Sprintf("%s:%d: possible secret (%s, fingerprint %s)", f.Path, f.Line, f.Rule, f.Fingerprint),
				fmt.Sprintf("remove it or use ${NAME}; if it is not a secret, add the line \"%s %s\" to secrets-allow", f.Path, f.Fingerprint))
		}
		return nil
	})
	if err != nil {
		add("finding", "secrets", exit.OneLine(err, "; "), "")
	}
	if found == 0 && err == nil && len(out) == 0 {
		add("ok", "secrets", fmt.Sprintf("%d files scanned", files), "")
	}
	for _, u := range allow.Unused() {
		add("info", "secrets", "secrets-allow: "+u+" is not needed any more", "")
	}
	return out
}
