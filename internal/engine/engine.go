// Package engine runs the parts in order: resolve the source, load the layers, render for
// each harness and compute the change. It is the only core package that imports an adapter.
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/adapter/claude"
	"github.com/Pnakoryn/livery/internal/adapter/kiro"
	"github.com/Pnakoryn/livery/internal/adapter/opencode"
	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
	"github.com/Pnakoryn/livery/internal/source"
)

// Adapters returns the registered adapters; the single place they are registered.
func Adapters() []adapter.Adapter {
	return []adapter.Adapter{kiro.New(), claude.New(), opencode.New()}
}

// Names returns the registered adapters' names, in order.
func Names() []string {
	var names []string
	for _, a := range Adapters() {
		names = append(names, a.Name())
	}
	return names
}

// Importers returns the registered adapters that can read their harness folder back, in
// registration order.
func Importers() []adapter.Adapter {
	var res []adapter.Adapter
	for _, a := range Adapters() {
		if _, ok := a.(adapter.Importer); ok {
			res = append(res, a)
		}
	}
	return res
}

// Prepared is everything a command needs to report or apply.
type Prepared struct {
	Config   config.Config
	Resolved source.Resolved
	// Teams are the team names found under teams/ in the source, sorted.
	Teams []string
	// MovedTag is the warning line when the resolved tag points to another commit than in the
	// old lock; empty otherwise. TagFrom and TagTo are the shortened commits.
	MovedTag string
	TagFrom  string
	TagTo    string
	// OldTag is the tag of the old lock when it has the same source and a tag; empty otherwise.
	OldTag    string
	Overrides []content.Override
	// Items are the combined items of every layer, as rendered.
	Items  []content.Item
	Result apply.Result
	// Notes are the adapters' notes of every plan, in adapter order.
	Notes []string
	// Unset lists, per MCP server in item order, the ${NAME} references whose variable is empty
	// in the environment. Servers without such names are not listed.
	Unset []UnsetVars
}

// UnsetVars are the variables an MCP server refers to that are not set.
type UnsetVars struct {
	Server string
	// Names are sorted.
	Names []string
}

// Problem says which variables are not set, e.g. "A, B are not set in this shell".
func (u UnsetVars) Problem() string {
	verb := "is"
	if len(u.Names) > 1 {
		verb = "are"
	}
	return strings.Join(u.Names, ", ") + " " + verb + " not set in this shell"
}

// Fix says what to do about it.
func (u UnsetVars) Fix() string {
	it := "it"
	if len(u.Names) > 1 {
		it = "them"
	}
	return "set " + it + " before you start your coding agent"
}

// Line is the note line (without a newline) that tells the user about u.
func (u UnsetVars) Line() string {
	return "note    mcp " + u.Server + ": " + u.Problem() + "; " + u.Fix()
}

var varRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// collectRefs adds the ${NAME} references in every string of v to into.
func collectRefs(v any, into map[string]bool) {
	switch x := v.(type) {
	case string:
		for _, m := range varRefRe.FindAllStringSubmatch(x, -1) {
			into[m[1]] = true
		}
	case []any:
		for _, e := range x {
			collectRefs(e, into)
		}
	case map[string]any:
		for _, e := range x {
			collectRefs(e, into)
		}
	}
}

// unsetVars finds, in the source MCP items, the referenced variables that env does not set.
func unsetVars(items []content.Item, env adapter.Env) []UnsetVars {
	var res []UnsetVars
	for _, it := range items {
		if it.Kind != content.MCP {
			continue
		}
		var v any
		if json.Unmarshal(it.Server, &v) != nil {
			continue
		}
		refs := map[string]bool{}
		collectRefs(v, refs)
		var names []string
		for n := range refs {
			if env.Getenv == nil || env.Getenv(n) == "" {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		res = append(res, UnsetVars{Server: it.Name, Names: names})
	}
	return res
}

// ErrPersonal is wrapped by the errors of a broken file in the personal layer.
var ErrPersonal = errors.New("personal layer")

// personalError makes err (from loading the personal layer in dir) start with the full path of
// the file as the scope shows it, and end with the fix. Errors that are not about a file in the
// layer (the folder itself is unreadable) pass through.
func personalError(env adapter.Env, dir string, err error) error {
	var ee *exit.Error
	if !errors.As(err, &ee) || ee.Code != exit.Usage {
		return err
	}
	rest, ok := strings.CutPrefix(ee.Message, "personal: ")
	if !ok || strings.HasPrefix(rest, "cannot read the layer") {
		return err
	}
	shown := lock.ScopeOf(env).Show(dir)
	return &exit.Error{Code: exit.Usage, Cause: ErrPersonal,
		Message: shown + "/" + rest + "; fix or remove the file, then run livery update"}
}

// loadDir loads the layer in dir; a missing directory is an empty layer. next is the command
// the unreadable-directory message tells the user to run.
func loadDir(dir, layer, next string) ([]content.Item, error) {
	st, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, exit.Environmentf("cannot read %s: %v; check its permissions, then run %s", dir, err, next)
	}
	if !st.IsDir() {
		return nil, exit.Usagef("%s is not a directory; remove it or replace it with a directory", dir)
	}
	return content.LoadLayer(os.DirFS(dir), layer)
}

// Fetcher remembers what fetch returned for a source address and channel, so one livery run
// fetches each source once. Create one per command (the zero value works) and pass it to Teams
// and Prepare. A nil *Fetcher remembers nothing.
type Fetcher struct {
	// Retry is the command a fix hint tells the user to run again; empty means "livery update".
	Retry string

	done map[[2]string]fetched
}

type fetched struct {
	res   source.Resolved
	dir   string
	teams []string
	err   error
}

func (f *Fetcher) fetch(env adapter.Env, src, channel string) (source.Resolved, string, []string, error) {
	if f == nil {
		return fetch(env, src, channel, "")
	}
	key := [2]string{src, channel}
	if r, ok := f.done[key]; ok {
		return r.res, r.dir, r.teams, r.err
	}
	res, dir, teams, err := fetch(env, src, channel, f.Retry)
	if f.done == nil {
		f.done = map[[2]string]fetched{}
	}
	f.done[key] = fetched{res, dir, teams, err}
	return res, dir, teams, err
}

// fetch resolves and exports the source, reads the manifest and lists teams/ (sorted).
func fetch(env adapter.Env, src, channel, retry string) (source.Resolved, string, []string, error) {
	cache := paths.CacheDir(env)
	res, err := source.Resolve(cache, src, channel, retry)
	if err != nil {
		return res, "", nil, err
	}
	dir, err := source.Export(cache, res)
	if err != nil {
		return res, "", nil, err
	}
	if _, err := content.ReadManifest(os.DirFS(dir)); err != nil {
		return res, dir, nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "teams"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, dir, nil, exit.Environmentf("cannot read %s: %v; if teams in the shared repository is a file, replace it with a directory (or remove it) and tag a new release, otherwise remove the cache directory %s; then run livery update", filepath.Join(dir, "teams"), err, cache)
	}
	var teams []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			teams = append(teams, e.Name())
		}
	}
	sort.Strings(teams)
	return res, dir, teams, nil
}

// Teams resolves the source and returns the team names under teams/, sorted.
func Teams(f *Fetcher, env adapter.Env, src, channel string) ([]string, source.Resolved, error) {
	res, _, teams, err := f.fetch(env, src, channel)
	return teams, res, err
}

// Prepare resolves the source, builds the items and computes the change.
// It writes nothing outside the cache directory. The first error stops the run.
func Prepare(f *Fetcher, env adapter.Env, version string, cfg config.Config) (Prepared, error) {
	p := Prepared{Config: cfg}
	if cfg.Source == "" {
		return p, exit.Usagef("no source in %s; run `livery setup`", paths.ConfigFile(env))
	}
	base, teams, err := prepareSource(f, env, &p)
	if err != nil {
		return p, err
	}
	plans, err := render(env, &p, base, teams, cfg.Harnesses)
	if err != nil {
		return p, err
	}

	old, _, err := lock.ReadIn(paths.LockFile(env), lock.ScopeOf(env))
	if err != nil {
		return p, err
	}
	res := p.Resolved
	if old.Source == res.Source {
		p.OldTag = old.Tag
	}
	if old.Source == res.Source && old.Tag != "" && old.Tag == res.Tag && old.Commit != res.Commit {
		p.TagFrom, p.TagTo = source.ShortCommit(old.Commit), source.ShortCommit(res.Commit)
		p.MovedTag = fmt.Sprintf("warning: tag %s moved from %s to %s since the last run; a release tag should never change, ask the owner of the shared repository",
			res.Tag, p.TagFrom, p.TagTo)
	}
	meta := lock.Lock{Livery: version, Source: res.Source, Channel: res.Channel, Tag: res.Tag, Commit: res.Commit,
		Teams: cfg.Teams, Harnesses: cfg.Harnesses}
	p.Result, err = apply.Compute(plans, old, meta, lock.ScopeOf(env), "")
	return p, err
}

// render combines base, teams and the personal layer as it is on disk into p.Items and renders
// them for every adapter in harnesses; the other adapters get an empty plan.
func render(env adapter.Env, p *Prepared, base []content.Item, teams [][]content.Item, harnesses []string) ([]adapter.Plan, error) {
	// A project has no personal layer: PersonalDir is empty there.
	var personal []content.Item
	if dir := paths.PersonalDir(env); dir != "" {
		var err error
		if personal, err = loadDir(dir, "personal", "livery update"); err != nil {
			return nil, personalError(env, dir, err)
		}
	}
	items, overrides, err := content.Combine(base, teams, personal, paths.PersonalDir(env) != "")
	if err != nil {
		return nil, err
	}
	p.Overrides = append(p.Overrides, overrides...)
	p.Items = items
	p.Unset = unsetVars(items, env)

	names := Names()
	for _, it := range items {
		if it.Kind == content.Native && !slices.Contains(names, it.Harness) {
			return nil, exit.Usagef("%s: %s/%s: no adapter for harness %q; the registered harnesses: %s; remove or move the file",
				it.Layer, it.Harness, it.Name, it.Harness, strings.Join(names, ", "))
		}
	}

	var plans []adapter.Plan
	for _, a := range Adapters() {
		if !slices.Contains(harnesses, a.Name()) {
			plans = append(plans, adapter.Plan{Root: a.Root(env), EntryKeys: a.EntryKeys(env)})
			continue
		}
		plan, err := a.Render(items, env)
		if err != nil {
			if dir := paths.PersonalDir(env); dir != "" {
				err = personalError(env, dir, err)
			}
			return nil, err
		}
		plan.EntryKeys = a.EntryKeys(env)
		plans = append(plans, plan)
		p.Notes = append(p.Notes, plan.Notes...)
	}
	return plans, nil
}

// PrepareInstalled builds the items of the release recorded in old (its source, commit, teams
// and harnesses) and computes the change against the disk, without git fetch: the config is not
// read. The personal layer is read as it is on disk. A cache without the commit is an error
// wrapping source.ErrNotCached. It writes nothing outside the cache directory.
func PrepareInstalled(env adapter.Env, version string, old lock.Lock) (Prepared, error) {
	p := Prepared{Config: config.Config{Source: old.Source, Channel: old.Channel, Teams: old.Teams, Harnesses: old.Harnesses}}
	cache := paths.CacheDir(env)
	res, err := source.Cached(cache, old.Source, old.Channel, old.Tag, old.Commit)
	p.Resolved = res
	if err != nil {
		return p, err
	}
	dir, err := source.Export(cache, res)
	if err != nil {
		return p, err
	}
	base, err := loadDir(filepath.Join(dir, "base"), "base", "livery update")
	if err != nil {
		return p, err
	}
	var teams [][]content.Item
	for _, t := range old.Teams {
		its, err := loadDir(filepath.Join(dir, "teams", t), "team:"+t, "livery update")
		if err != nil {
			return p, err
		}
		teams = append(teams, its)
	}
	plans, err := render(env, &p, base, teams, old.Harnesses)
	if err != nil {
		return p, err
	}
	if old.Source == res.Source {
		p.OldTag = old.Tag
	}
	meta := lock.Lock{Livery: version, Source: res.Source, Channel: res.Channel, Tag: res.Tag, Commit: res.Commit,
		Teams: old.Teams, Harnesses: old.Harnesses}
	p.Result, err = apply.Compute(plans, old, meta, lock.ScopeOf(env), "")
	return p, err
}

// MissingTeam returns "" when every team of wanted is in found, the teams of the source src;
// otherwise it names the first missing team and the teams that exist.
func MissingTeam(src string, wanted, found []string) string {
	for _, t := range wanted {
		if !slices.Contains(found, t) {
			exist := "the shared repository has no teams"
			if len(found) > 0 {
				exist = "the teams that exist: " + strings.Join(found, ", ")
			}
			return fmt.Sprintf("team %q is not in the source %s; %s", t, src, exist)
		}
	}
	return ""
}

// prepareSource fetches the source of p.Config into p and loads its base and chosen teams.
func prepareSource(f *Fetcher, env adapter.Env, p *Prepared) ([]content.Item, [][]content.Item, error) {
	cfg := p.Config
	res, dir, found, err := f.fetch(env, cfg.Source, cfg.Channel)
	p.Resolved = res
	if err != nil {
		return nil, nil, err
	}
	p.Teams = found
	if m := MissingTeam(cfg.Source, cfg.Teams, found); m != "" {
		return nil, nil, exit.Usagef("%s; fix teams in %s", m, paths.ConfigFile(env))
	}
	base, err := loadDir(filepath.Join(dir, "base"), "base", "livery update")
	if err != nil {
		return nil, nil, err
	}
	var teams [][]content.Item
	for _, t := range cfg.Teams {
		its, err := loadDir(filepath.Join(dir, "teams", t), "team:"+t, "livery update")
		if err != nil {
			return nil, nil, err
		}
		teams = append(teams, its)
	}
	return base, teams, nil
}

// TeamItems are the items of one team folder of a source.
type TeamItems struct {
	Name  string
	Items []content.Item
}

// SourceCatalog is what a source offers: its base items and the items of every team.
type SourceCatalog struct {
	Resolved source.Resolved
	Base     []content.Item
	// Teams are in name order.
	Teams []TeamItems
}

// Catalog resolves and exports the source and loads base and every team under teams/.
// It writes nothing outside the cache directory.
func Catalog(env adapter.Env, address, channel, retry string) (SourceCatalog, error) {
	res, dir, teams, err := fetch(env, address, channel, retry)
	c := SourceCatalog{Resolved: res}
	if err != nil {
		return c, err
	}
	if c.Base, err = loadDir(filepath.Join(dir, "base"), "base", "livery update"); err != nil {
		return c, err
	}
	for _, t := range teams {
		its, err := loadDir(filepath.Join(dir, "teams", t), "team:"+t, "livery update")
		if err != nil {
			return c, err
		}
		c.Teams = append(c.Teams, TeamItems{Name: t, Items: its})
	}
	return c, nil
}
