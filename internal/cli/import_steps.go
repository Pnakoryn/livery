package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/engine"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/secrets"
)

// importOpts is what the flags of `livery import` say.
type importOpts struct {
	from    listFlag
	project bool
	into    string
	team    string
	only    listFlag
	plan    bool
}

// importRead is what the harness folders hold: the items found, the files skipped, and the
// folder or folders they came from, for messages.
type importRead struct {
	root    string
	found   []adapter.Found
	harness []string // the harness each found item was read from, parallel to found
	skipped []adapter.Skipped
}

// importConv is the content converted to the source layout.
type importConv struct {
	files        []importFile
	vars         []string
	varInfo      map[string]varInfo
	urlPasswords []string
	collisions   []string
}

// parseImportFlags parses and validates the flags of import.
func parseImportFlags(args []string) (*importOpts, error) {
	fset := flag.NewFlagSet("import", flag.ContinueOnError)
	o := &importOpts{}
	var importers []string
	for _, a := range engine.Importers() {
		importers = append(importers, a.Name())
	}
	fset.Var(&o.from, "from", "`name` coding agent to read from: "+strings.Join(importers, ", ")+"; repeat for several (only with --project)")
	project := projectFlag(fset, "read the project's harness files instead of the machine's")
	into := fset.String("into", "", "`folder` shared repository folder to write into")
	team := fset.String("team", "", "`name` write into teams/<name> instead of base")
	fset.Var(&o.only, "only", "`kind/name` import only this item, such as skill/review; repeat for several (default: every item found)")
	plan := fset.Bool("plan", false, "show the change and write nothing")
	if err := parseFlags("import", fset, args); err != nil {
		return nil, err
	}
	o.project, o.into, o.team, o.plan = *project, *into, *team, *plan

	if o.team != "" && !config.ValidTeam(o.team) {
		return nil, exit.Usagef("import: --team %q is not a valid team name; use lowercase letters, digits and '-', starting with a letter or digit", o.team)
	}
	if o.only.empty {
		return nil, exit.Usagef("import: --only is empty; pass --only KIND/NAME, e.g. --only skill/review")
	}
	if !o.project && len(o.from.values) > 1 {
		return nil, exit.Usagef("import: --from is given %d times; without --project import reads one harness, add --project or pass one --from", len(o.from.values))
	}
	return o, nil
}

// chooseHarnesses returns the adapters to read from: the --from values or, in a terminal, the
// harness the user picks.
func chooseHarnesses(o *importOpts, env adapter.Env, in *bufio.Reader, stdout io.Writer, interactive bool) ([]adapter.Adapter, error) {
	importers := engine.Importers()
	var names []string
	for _, a := range importers {
		names = append(names, a.Name())
	}
	from := o.from.values
	if len(from) == 0 {
		if !interactive {
			return nil, exit.Usagef("import: no harness given; pass --from with one of: %s", strings.Join(names, ", "))
		}
		var detected []string
		for _, a := range importers {
			if st, err := os.Stat(a.Root(env)); err == nil && st.IsDir() {
				detected = append(detected, a.Name())
			}
		}
		if len(detected) == 0 {
			return nil, exit.Usagef("import: no harness folder found; pass --from with one of: %s", strings.Join(names, ", "))
		}
		for i, n := range detected {
			_, _ = fmt.Fprintf(stdout, "  %d. %s\n", i+1, n)
		}
		idx, err := askNumbers("import", in, stdout, "Import from which harness (number): ", "pass --from", len(detected), false)
		if err != nil {
			return nil, err
		}
		if len(idx) != 1 {
			return nil, exit.Usagef("import: choose one harness; pass --from with one of: %s", strings.Join(detected, ", "))
		}
		from = []string{detected[idx[0]]}
	}
	var ads []adapter.Adapter
	for _, f := range from {
		var ad adapter.Adapter
		for _, a := range importers {
			if a.Name() == f {
				ad = a
			}
		}
		if ad == nil {
			return nil, exit.Usagef("import: --from %q is not a harness livery can import from; use one of: %s", f, strings.Join(names, ", "))
		}
		if !slices.Contains(ads, ad) {
			ads = append(ads, ad)
		}
	}
	return ads, nil
}

// chooseTarget returns the shared repository folder to write into, asking for it in a terminal.
func chooseTarget(o *importOpts, env adapter.Env, in *bufio.Reader, stdout io.Writer, interactive bool) (string, error) {
	into := o.into
	if into == "" {
		if !interactive {
			return "", exit.Usagef("import: no target folder; pass --into <shared repository folder>")
		}
		v, err := ask("import", in, stdout, "Shared repository folder to write into: ", "pass --into", func(s string) (string, bool) {
			return s, s != ""
		})
		if err != nil {
			return "", err
		}
		into = v
	}
	dir := filepath.Clean(expandHome(env, into))
	if st, err := os.Stat(dir); err == nil && !st.IsDir() {
		return "", exit.Usagef("import: --into %s is not a directory; pass a folder", dir)
	}
	return dir, nil
}

// readHarnesses asks each adapter for what it holds.
func readHarnesses(env adapter.Env, ads []adapter.Adapter, project bool) (*importRead, error) {
	rd := &importRead{}
	var roots []string
	for _, ad := range ads {
		roots = append(roots, ad.Root(env))
		f, s, err := ad.(adapter.Importer).Import(env)
		if err != nil {
			return nil, err
		}
		rd.found = append(rd.found, f...)
		for range f {
			rd.harness = append(rd.harness, ad.Name())
		}
		rd.skipped = append(rd.skipped, s...)
	}
	rd.root = strings.Join(roots, ", ")
	if project {
		rd.root = env.Project
	}
	return rd, nil
}

// pruneRead drops the items livery installed and says how many it left out.
func pruneRead(env adapter.Env, rd *importRead, stdout io.Writer) error {
	installed, err := installedBy(env)
	if err != nil {
		return err
	}
	var found []adapter.Found
	var harness []string
	left := 0
	for i, f := range rd.found {
		if installed(f) {
			left++
			continue
		}
		found = append(found, f)
		harness = append(harness, rd.harness[i])
	}
	rd.found, rd.harness = found, harness
	if left > 0 {
		_, _ = fmt.Fprintf(stdout, "left out: %d items that livery installed\n", left)
	}
	return nil
}

// mergeSame makes one item of the items with one kind and name found in several harnesses: when
// they are identical it keeps the first (in --from order) with the paths of all and prints a
// `same` line; when they differ it stops with exit 2 naming both files. Native items belong to
// one harness and are never merged.
func mergeSame(rd *importRead, stdout io.Writer) error {
	var found []adapter.Found
	var harness []string
	at := map[string]int{}
	var order []string
	from := map[string][]string{}
	for i, f := range rd.found {
		if f.Item.Kind == content.Native {
			found = append(found, f)
			harness = append(harness, rd.harness[i])
			continue
		}
		label := itemLabel(f.Item)
		j, ok := at[label]
		if !ok {
			at[label] = len(found)
			found = append(found, f)
			harness = append(harness, rd.harness[i])
			order = append(order, label)
			from[label] = []string{rd.harness[i]}
			continue
		}
		first := found[j]
		if p1, p2, same := sameItem(first, f); !same {
			return exit.Usagef("import: %s %q differs between %s and %s; make them the same, or import one harness at a time (one --from per run)", f.Item.Kind, f.Item.Name, p1, p2)
		}
		first.Paths = append(slices.Clone(first.Paths), f.Paths...)
		found[j] = first
		if !slices.Contains(from[label], rd.harness[i]) {
			from[label] = append(from[label], rd.harness[i])
		}
	}
	for _, label := range order {
		if hs := from[label]; len(hs) > 1 {
			_, _ = fmt.Fprintf(stdout, "same    %s: identical in %s; imported once\n", label, strings.Join(hs, ", "))
		}
	}
	rd.found, rd.harness = found, harness
	return nil
}

// sameItem reports whether two found items of one kind and name are identical and, when they
// are not, the file of each that differs.
func sameItem(a, b adapter.Found) (pa, pb string, same bool) {
	pa, pb = foundPath(a, ""), foundPath(b, "")
	switch a.Item.Kind {
	case content.InstructionKind:
		x, y := a.Item.Instruction, b.Item.Instruction
		same = x.Description == y.Description && slices.Equal(x.Globs, y.Globs) &&
			strings.TrimRight(x.Body, " \t\r\n") == strings.TrimRight(y.Body, " \t\r\n")
	case content.Skill:
		fa, fb := skillFiles(a.Item), skillFiles(b.Item)
		same = true
		paths := make([]string, 0, len(fa)+len(fb))
		for p := range fa {
			paths = append(paths, p)
		}
		for p := range fb {
			if _, ok := fa[p]; !ok {
				paths = append(paths, p)
			}
		}
		slices.Sort(paths)
		for _, p := range paths {
			x, okA := fa[p]
			y, okB := fb[p]
			if !okA || !okB || x.Exec != y.Exec || !bytes.Equal(x.Data, y.Data) {
				same = false
				rel := "skills/" + a.Item.Name + "/" + p
				pa, pb = foundPath(a, rel), foundPath(b, rel)
				break
			}
		}
	case content.MCP:
		var x, y any
		ea, eb := json.Unmarshal(a.Item.Server, &x), json.Unmarshal(b.Item.Server, &y)
		same = ea == nil && eb == nil && reflect.DeepEqual(x, y)
	}
	return pa, pb, same
}

// skillFiles maps the relative paths of a skill to its files.
func skillFiles(it content.Item) map[string]content.File {
	m := make(map[string]content.File, len(it.Files))
	for _, f := range it.Files {
		m[f.Path] = f
	}
	return m
}

// foundPath is the harness file a found item (or, with rel, its layer file rel) was read from.
func foundPath(f adapter.Found, rel string) string {
	if f.Path != "" {
		return f.Path
	}
	if rel != "" {
		return originalOf(f, rel)
	}
	if len(f.Paths) > 0 {
		return f.Paths[0]
	}
	return f.Item.Origin
}

// selectItems picks the items to import: those named by --only, those the user picks in a
// terminal, or all of them.
func selectItems(o *importOpts, rd *importRead, in *bufio.Reader, stdout io.Writer, interactive bool) ([]adapter.Found, error) {
	found := rd.found
	if o.only.set {
		var picked []adapter.Found
		for _, f := range found {
			if slices.Contains(o.only.values, itemLabel(f.Item)) {
				picked = append(picked, f)
			}
		}
		for _, name := range o.only.values {
			if !slices.ContainsFunc(picked, func(f adapter.Found) bool { return itemLabel(f.Item) == name }) {
				var labels []string
				for _, f := range found {
					labels = append(labels, itemLabel(f.Item))
				}
				avail := "there is nothing to import"
				if len(labels) > 0 {
					avail = "the items: " + strings.Join(labels, ", ")
				}
				return nil, exit.Usagef("import: --only %s matches nothing in %s; %s", name, rd.root, avail)
			}
		}
		found = picked
	} else if interactive && len(found) > 0 {
		for i, f := range found {
			_, _ = fmt.Fprintf(stdout, "  %d. %s\n", i+1, itemLabel(f.Item))
		}
		idx, err := askNumbers("import", in, stdout, "Import which (numbers, comma-separated, empty for all): ", "pass --only", len(found), true)
		if err != nil {
			return nil, err
		}
		if len(idx) > 0 {
			var picked []adapter.Found
			for _, i := range idx {
				picked = append(picked, found[i])
			}
			found = picked
		}
	}
	return found, nil
}

// varInfo says where a variable is used: the server, the place in it, and whether import
// replaced a literal value by the reference.
type varInfo struct {
	server, place string
	replaced      bool
}

var varRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// refPlaces returns, for each ${NAME} in the strings of the server, the first place it is used:
// env.<key>, headers.<key> or the top-level key.
func refPlaces(server json.RawMessage) map[string]string {
	out := map[string]string{}
	var top map[string]json.RawMessage
	if json.Unmarshal(server, &top) != nil {
		return out
	}
	var walk func(place string, v any)
	walk = func(place string, v any) {
		switch x := v.(type) {
		case string:
			for _, m := range varRefRe.FindAllStringSubmatch(x, -1) {
				if _, ok := out[m[1]]; !ok {
					out[m[1]] = place
				}
			}
		case []any:
			for _, e := range x {
				walk(place, e)
			}
		}
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	// env and headers first: a value import replaced is there, and its line names that place.
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i] == "env" || keys[i] == "headers", keys[j] == "env" || keys[j] == "headers"
		if a != b {
			return a
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		var v any
		if json.Unmarshal(top[k], &v) != nil {
			continue
		}
		if m, ok := v.(map[string]any); ok && (k == "env" || k == "headers") {
			sub := make([]string, 0, len(m))
			for sk := range m {
				sub = append(sub, sk)
			}
			sort.Strings(sub)
			for _, sk := range sub {
				walk(k+"."+sk, m[sk])
			}
			continue
		}
		walk(k, v)
	}
	return out
}

// mcpVars collects the variables of the MCP servers and the clashes between them.
type mcpVars struct {
	owner      map[string]string
	header     map[string]bool
	vars       []string
	info       map[string]varInfo
	collisions []string
}

// add records the variables of the MCP server item it and returns it with its references
// replaced by ${NAME}.
func (m *mcpVars) add(it content.Item) (content.Item, error) {
	label := itemLabel(it)
	srv, ns, err := content.EnvRefs(it.Server)
	if err != nil {
		return it, exit.Usagef("import: %s (%s): %s", label, it.Origin, strings.TrimPrefix(err.Error(), "MCP server: "))
	}
	srv, hs, err := content.HeaderRefs(srv, it.Name)
	if err != nil {
		return it, exit.Usagef("import: %s (%s): %s", label, it.Origin, strings.TrimPrefix(err.Error(), "MCP server: "))
	}
	it.Server = srv
	for n, place := range refPlaces(srv) {
		vi := m.info[n]
		if vi.server == "" {
			vi.server, vi.place = it.Name, place
		}
		// The line names the place where import replaced a value, not an earlier reference.
		if !vi.replaced && (slices.Contains(ns, n) || slices.Contains(hs, n)) {
			vi.server, vi.place, vi.replaced = it.Name, place, true
			if slices.Contains(ns, n) {
				vi.place = "env." + n
			}
		}
		m.info[n] = vi
	}
	for _, n := range hs {
		if prev, ok := m.owner[n]; ok && prev != label && m.header[n] {
			return it, exit.Usagef("import: %s and %s would both use the header variable %s; rename one of the servers (%s) so the names differ, then run livery import again", prev, label, n, it.Origin)
		}
		if prev, ok := m.owner[n]; ok && prev != label {
			m.collisions = append(m.collisions, fmt.Sprintf("%s: the variable %s is already used by %s", label, n, prev))
		}
		if slices.Contains(ns, n) {
			m.collisions = append(m.collisions, fmt.Sprintf("%s: the header variable %s is also an env key", label, n))
		}
		m.owner[n] = label
	}
	for _, n := range ns {
		if prev, ok := m.owner[n]; ok && prev != label && m.header[n] {
			m.collisions = append(m.collisions, fmt.Sprintf("%s: the env key %s is already used by a header variable of %s", label, n, prev))
		}
		if _, ok := m.owner[n]; !ok {
			m.owner[n] = label
		}
	}
	for _, n := range hs {
		m.header[n] = true
	}
	for n := range m.info {
		if !slices.Contains(m.vars, n) {
			m.vars = append(m.vars, n)
		}
	}
	return it, nil
}

// convertItems turns the selected items into the files of the layer layerDir of the source
// folder dir, and notes the variables, the passwords in urls and the name clashes on the way.
func convertItems(found []adapter.Found, dir, layerDir string) (*importConv, error) {
	m := &mcpVars{owner: map[string]string{}, header: map[string]bool{}, info: map[string]varInfo{}}
	conv := &importConv{}
	items := make([]content.Item, 0, len(found))
	converted := make([]adapter.Found, len(found))
	for i, f := range found {
		it := f.Item
		if it.Kind == content.MCP {
			if content.URLPassword(it.Server) {
				conv.urlPasswords = append(conv.urlPasswords, fmt.Sprintf("%s (%s)", itemLabel(it), it.Origin))
			}
			var err error
			if it, err = m.add(it); err != nil {
				return nil, err
			}
		}
		f.Item = it
		converted[i] = f
		items = append(items, it)
	}
	sort.Strings(m.vars)
	conv.vars, conv.varInfo, conv.collisions = m.vars, m.info, m.collisions
	if _, err := content.LayerFiles(items); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(filepath.Join(dir, "livery.json")); errors.Is(err, fs.ErrNotExist) {
		conv.files = append(conv.files, importFile{rel: "livery.json", data: []byte(manifestData)})
	}
	for _, f := range converted {
		lf, err := content.LayerFiles([]content.Item{f.Item})
		if err != nil {
			return nil, err
		}
		for _, x := range lf {
			conv.files = append(conv.files, importFile{rel: layerDir + "/" + x.Path, data: x.Data, exec: x.Exec, original: originalOf(f, x.Path)})
		}
	}
	sort.SliceStable(conv.files, func(i, j int) bool { return conv.files[i].rel < conv.files[j].rel })
	return conv, nil
}

// importFindings prints findings and counts them.
type importFindings struct {
	out io.Writer
	n   int
}

func (f *importFindings) add(format string, a ...any) {
	_, _ = fmt.Fprintf(f.out, "finding  "+format+"\n", a...)
	f.n++
}

// checkTarget checks the converted files before anything is written: name clashes, passwords in
// urls, links and other content in the target folder dir, and secrets. It prints a line for each
// finding and returns an exit 1 error when there is one. write are the files to create, same
// those that are already there with the same content.
func checkTarget(stdout io.Writer, dir string, conv *importConv) (write, same []importFile, err error) {
	fd := &importFindings{out: stdout}
	for _, s := range conv.urlPasswords {
		fd.add("%s: the url holds a password -> move the password into a header or a ${NAME} variable, then run livery import again", s)
	}
	for _, c := range conv.collisions {
		fd.add("%s -> rename one of the servers or headers, then run livery import again", c)
	}
	for _, f := range conv.files {
		target := filepath.Join(dir, filepath.FromSlash(f.rel))
		if link := linkBelow(dir, f.rel); link != "" {
			fd.add("symbolic link: %s -> livery does not follow links; replace it with a folder, or choose another --into", link)
			continue
		}
		st, err := os.Lstat(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			write = append(write, f)
		case err != nil:
			return nil, nil, exit.Environmentf("import: cannot read %s: %v; fix its permissions, then run livery import again", target, err)
		case !st.Mode().IsRegular():
			fd.add("exists: %s -> it is not a regular file; remove or move it, then run livery import again", target)
		default:
			data, err := os.ReadFile(target)
			if err != nil {
				return nil, nil, exit.Environmentf("import: cannot read %s: %v; fix its permissions, then run livery import again", target, err)
			}
			if bytes.Equal(data, f.data) {
				same = append(same, f)
			} else {
				fd.add("exists: %s -> it has other content; remove or rename it, or import into another --into, then run livery import again", target)
			}
		}
	}
	if err := scanSecrets(fd, dir, write); err != nil {
		return nil, nil, err
	}
	if fd.n > 0 {
		return nil, nil, exit.Findingf("import: %d finding(s), nothing written; each line above says how to fix it", fd.n)
	}
	return write, same, nil
}

// scanSecrets adds a finding for each possible secret in the files, except those the
// secrets-allow file of dir allows.
func scanSecrets(fd *importFindings, dir string, write []importFile) error {
	allow := secrets.Allow{}
	allowPath := filepath.Join(dir, "secrets-allow")
	if data, err := os.ReadFile(allowPath); err == nil {
		allow, err = secrets.ParseAllow(allowPath, data)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return exit.Environmentf("import: cannot read %s: %v; fix its permissions, then run livery import again", allowPath, err)
	}
	for _, f := range write {
		for _, s := range secrets.Scan(f.rel, f.data) {
			if allow.Allows(s) {
				continue
			}
			orig := f.original
			if orig == "" {
				orig = s.Path
			}
			if s.Line == 0 && s.Rule == secrets.UnscannedBinary {
				fd.add("%s: binary file, not scanned -> if it belongs in the shared repository, add the line `%s %s` to %s, otherwise remove it from %s; then run livery import again",
					s.Path, s.Path, s.Fingerprint, allowPath, orig)
				continue
			}
			if s.Line == 0 {
				// A file-level finding (binary, key container): there is no line to edit.
				fd.add("%s:%d: possible secret (%s, fingerprint %s) -> if it is not a secret, add the line `%s %s` to %s, otherwise remove it from %s; then run livery import again",
					s.Path, s.Line, s.Rule, s.Fingerprint, s.Path, s.Fingerprint, allowPath, orig)
				continue
			}
			fd.add("%s:%d: possible secret (%s, fingerprint %s) -> remove it from %s or replace it with ${NAME}; if it is not a secret, add the line `%s %s` to %s; then run livery import again",
				s.Path, s.Line, s.Rule, s.Fingerprint, orig, s.Path, s.Fingerprint, allowPath)
		}
	}
	return nil
}

// writeImport creates the files below dir; with plan it writes nothing.
func writeImport(dir string, write []importFile, plan bool) error {
	if plan {
		return nil
	}
	for _, f := range write {
		if err := writeNew(dir, f); err != nil {
			return err
		}
	}
	return nil
}

// printSkipped prints a line for each skipped file.
func printSkipped(stdout io.Writer, skipped []adapter.Skipped) {
	for _, s := range skipped {
		word := "skip"
		if s.Partial {
			word = "note"
		}
		_, _ = fmt.Fprintf(stdout, "%-8s%s: %s\n", word, s.Path, s.Reason)
	}
}

// reportImport prints what was (or, with --plan, would be) written and the next steps.
func reportImport(stdout io.Writer, o *importOpts, dir string, rd *importRead, conv *importConv, write, same []importFile) {
	if o.plan {
		_, _ = fmt.Fprintf(stdout, "livery import: would write into %s (from %s)\n", dir, rd.root)
	} else {
		_, _ = fmt.Fprintf(stdout, "livery import: wrote into %s (from %s)\n", dir, rd.root)
	}
	for _, f := range write {
		_, _ = fmt.Fprintf(stdout, "write   %s\n", filepath.Join(dir, filepath.FromSlash(f.rel)))
	}
	for _, f := range same {
		_, _ = fmt.Fprintf(stdout, "same    %s\n", filepath.Join(dir, filepath.FromSlash(f.rel)))
	}
	printSkipped(stdout, rd.skipped)
	if len(conv.vars) > 0 {
		for _, n := range conv.vars {
			vi := conv.varInfo[n]
			how := "already a reference"
			if vi.replaced {
				how = "the value was replaced by ${" + n + "}"
			}
			_, _ = fmt.Fprintf(stdout, "var     %s: server %s, %s (%s)\n", n, vi.server, vi.place, how)
		}
		_, _ = fmt.Fprintf(stdout, "everyone who installs this needs these variables set in the shell that starts the coding agent\n")
	}
	if len(write) == 0 && len(same) == 0 {
		_, _ = fmt.Fprintf(stdout, "nothing to import from %s\n", rd.root)
		return
	}
	q := shellQuote(dir)
	_, _ = fmt.Fprintf(stdout, "next steps:\n")
	if o.plan {
		_, _ = fmt.Fprintf(stdout, "  run livery import again without --plan\n")
	}
	_, _ = fmt.Fprintf(stdout, "  livery check %s\n", q)
	_, _ = fmt.Fprintf(stdout, "  review and commit the folder (git -C %s init first if it is not a repository)\n", q)
	_, _ = fmt.Fprintf(stdout, "  tag a release: git -C %s tag vX.Y.Z (and push it)\n", q)
	if o.project {
		_, _ = fmt.Fprintf(stdout, "  livery setup --project --source <the address your team clones the shared repository from>\n")
	} else {
		_, _ = fmt.Fprintf(stdout, "  livery setup --source %s\n", q)
		_, _ = fmt.Fprintf(stdout, "  on this machine your own files stay as they are (kept, yours); docs/onboarding.md says how to switch\n")
	}
}
