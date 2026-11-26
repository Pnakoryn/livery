package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/engine"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
	"github.com/Pnakoryn/livery/internal/source"
)

// list implements `livery list [--source ADDRESS] [--channel C]`: what the source offers, or
// with --installed (listInstalled) what is installed.
// It is read-only and takes no run lock.
func list(args []string, stdout io.Writer, env adapter.Env) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	src := fs.String("source", "", "`address` git URL or folder of a shared repository to show instead of the configured one")
	channel := fs.String("channel", "", "`channel` stable (newest release, the default), next (newest commit), or a release tag such as v1.2.0 (use with --source)")
	project := projectFlag(fs, "show the source of the project around the working directory")
	installed := fs.Bool("installed", false, "show what is installed and what is yours")
	if err := parseFlags("list", fs, args); err != nil {
		return err
	}
	env, err := scoped(env, *project, false)
	if err != nil {
		return err
	}
	if *installed {
		if *src != "" || *channel != "" {
			return exit.Usagef("list: --installed shows the installed setup and takes no --source or --channel")
		}
		return listInstalled(stdout, env)
	}
	var cfg config.Config
	if *src != "" {
		cfg.Source = source.Canonical(*src)
		cfg.Channel = *channel
		if cfg.Channel == "" {
			cfg.Channel = "stable"
		}
	} else {
		if *channel != "" {
			return exit.Usagef("list: --channel needs --source; pass --source ADDRESS or run `livery setup%s --channel C`", projectArg(env))
		}
		path := paths.ConfigFile(env)
		var found bool
		cfg, found, err = config.Load(path, "livery setup"+projectArg(env))
		if err != nil {
			return err
		}
		if !found || cfg.Source == "" {
			return exit.Usagef("list: no source in %s; run `livery setup%s` or pass --source ADDRESS", path, projectArg(env))
		}
	}
	cat, err := engine.Catalog(env, cfg.Source, cfg.Channel, "livery list"+projectArg(env))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s %s (%s)\n", cat.Resolved.Source, tagLabel(cat.Resolved.Tag), source.ShortCommit(cat.Resolved.Commit))
	printLayer(stdout, "base", cat.Base)
	for _, t := range cat.Teams {
		head := "team " + t.Name
		if slices.Contains(cfg.Teams, t.Name) {
			head += " *"
		}
		printLayer(stdout, head, t.Items)
	}
	return nil
}

// tagLabel shows a release tag; an empty tag (the next channel) is "next".
func tagLabel(tag string) string {
	if tag == "" {
		return "next"
	}
	return tag
}

func printLayer(w io.Writer, heading string, items []content.Item) {
	_, _ = fmt.Fprintf(w, "  %s\n", heading)
	if len(items) == 0 {
		_, _ = fmt.Fprintln(w, "    (empty)")
		return
	}
	for _, it := range items {
		kind := string(it.Kind)
		if it.Kind == content.Native {
			kind = it.Harness
		}
		_, _ = fmt.Fprintf(w, "    %-12s %s\n", kind, it.Name)
	}
}

// listInstalled implements `livery list --installed`: the items of the release the lock records
// (read from the cache, no fetch), with their layer and state, and what in the harness folders is the user's.
func listInstalled(stdout io.Writer, env adapter.Env) error {
	old, ok, err := lock.ReadIn(paths.LockFile(env), lock.ScopeOf(env))
	if err != nil {
		return err
	}
	if !ok {
		return exit.Usagef("list: nothing installed; run livery setup%s", projectArg(env))
	}
	p, err := engine.PrepareInstalled(env, version(), old)
	if errors.Is(err, source.ErrNotCached) {
		return exit.Environmentf("list: the installed release is not in the cache; run livery update%s, then livery list --installed%s", projectArg(env), projectArg(env))
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s %s (%s)\n", old.Source, tagLabel(old.Tag), source.ShortCommit(old.Commit))
	// The cache holds the release tags of the last fetch by any livery command.
	if old.Channel == "stable" && source.Newer(p.Resolved.Newest, old.Tag) {
		_, _ = fmt.Fprintf(stdout, "note    %s is available: livery update%s\n", p.Resolved.Newest, projectArg(env))
	}

	states := map[string]string{}
	rank := map[string]int{}
	set := func(origin, state string, r int) {
		if r > rank[origin] {
			rank[origin], states[origin] = r, state
		}
	}
	for _, a := range p.Result.Actions {
		if a.List {
			// A list element (opencode's "instructions" glob) is shared by every item of its
			// kind and has the origin of only one of them: it shows no state. Each item shows
			// the state of its own files.
			continue
		}
		switch a.Op {
		case apply.Edited:
			set(a.Origin, "edited", 4)
		case apply.KeptYours:
			set(a.Origin, "kept, yours", 3)
		case apply.Write, apply.SetEntry:
			if a.Missing {
				// Recorded in the lock but deleted from disk.
				set(a.Origin, "missing", 2)
			} else {
				set(a.Origin, "pending", 1)
			}
		case apply.Adopt:
			set(a.Origin, "pending", 1)
		}
	}
	// The name column is as wide as the longest name, so the columns after it line up.
	width := 20
	for _, it := range p.Items {
		width = max(width, len(it.Name))
	}
	for _, it := range p.Items {
		kind := string(it.Kind)
		if it.Kind == content.Native {
			kind = it.Harness
		}
		layer := "base"
		switch {
		case it.Layer == "personal":
			layer = "personal"
		case strings.HasPrefix(it.Layer, "team:"):
			layer = "team " + strings.TrimPrefix(it.Layer, "team:")
		}
		line := fmt.Sprintf("  %-12s %-*s %s", kind, width, it.Name, layer)
		if st := states[it.Layer+": "+it.Origin]; st != "" {
			line = fmt.Sprintf("  %-12s %-*s %-12s %s", kind, width, it.Name, layer, st)
		}
		_, _ = fmt.Fprintln(stdout, line)
	}

	type mine struct{ harness, kind, name string }
	var yours []mine
	for _, a := range engine.Importers() {
		if !slices.Contains(old.Harnesses, a.Name()) {
			continue
		}
		fnd, _, err := a.(adapter.Importer).Import(env)
		if err != nil {
			return err
		}
		fnd, _, err = dropInstalled(env, fnd)
		if err != nil {
			return err
		}
		for _, f := range fnd {
			kind := string(f.Item.Kind)
			if f.Item.Kind == content.Native {
				kind = f.Item.Harness
			}
			yours = append(yours, mine{a.Name(), kind, f.Item.Name})
		}
	}
	sort.Slice(yours, func(i, j int) bool {
		a, b := yours[i], yours[j]
		if a.harness != b.harness {
			return a.harness < b.harness
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.name < b.name
	})
	_, _ = fmt.Fprintln(stdout, "yours")
	if len(yours) == 0 {
		_, _ = fmt.Fprintln(stdout, "    (nothing)")
	}
	for _, y := range yours {
		_, _ = fmt.Fprintf(stdout, "  %-7s %-12s %s\n", y.harness, y.kind, y.name)
	}
	return nil
}
