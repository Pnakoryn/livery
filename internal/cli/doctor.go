package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
	"github.com/Pnakoryn/livery/internal/source"
)

// doctor implements `livery doctor`: one line per check, exit 1 when one is a finding.
// It writes nothing outside the cache.
func doctor(args []string, stdout io.Writer, env adapter.Env) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	project := projectFlag(fs, "check the project around the working directory")
	if err := parseFlags("doctor", fs, args); err != nil {
		return err
	}
	env, err := scoped(env, *project, false)
	if err != nil {
		return err
	}
	if *project {
		_, _ = fmt.Fprintf(stdout, "project: %s\n", env.Project)
	}
	projectNote(stdout, env, "doctor")
	d := &doctorRun{report: report{w: stdout}, env: env}
	if !d.checkGit() {
		return d.finish()
	}
	cfg, ok := d.checkConfig()
	if !ok {
		return d.finish()
	}
	d.checkLock()
	d.checkHarnesses(cfg)
	p, ok := d.checkSource(cfg)
	if !ok {
		return d.finish()
	}
	d.checkEdited(p)
	d.checkKeptYours(p)
	d.checkMissing(p)
	d.showOverrides(p)
	d.showUnset(p)
	d.checkUpdate(p)
	return d.finish()
}

// doctorRun is one run of doctor: the lines printed so far and the lock it read.
type doctorRun struct {
	report
	env adapter.Env
	// old is the lock; hasLock says whether it exists, lockBad whether it cannot be read.
	old              lock.Lock
	hasLock, lockBad bool
}

// project reports whether doctor checks a project.
func (d *doctorRun) project() bool { return d.env.Project != "" }

// subject names the item of a in a line.
func (d *doctorRun) subject(a apply.Action) string { return actionName(d.env, a) }

// checkGit reports whether git is on the PATH.
func (d *doctorRun) checkGit() bool {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		d.line("finding", "git", "not found on PATH", "install git")
		return false
	}
	d.line("ok", "git", gitPath, "")
	return true
}

// checkConfig reads and validates the config; ok is false when it is missing or wrong.
func (d *doctorRun) checkConfig() (config.Config, bool) {
	fix := "livery setup" + projectArg(d.env)
	cfgPath := paths.ConfigFile(d.env)
	cfg, found, err := config.Load(cfgPath, fix)
	if err == nil && !found {
		err = fmt.Errorf("%s does not exist", cfgPath)
	}
	if err == nil {
		err = cfg.Validate(cfgPath, engine.Names(), fix)
	}
	if err != nil {
		d.line("finding", "config", exit.OneLine(err, " "), fix)
		return cfg, false
	}
	d.line("ok", "config", cfgPath, "")
	return cfg, true
}

// checkLock reads the lock into d.
func (d *doctorRun) checkLock() {
	lockPath := paths.LockFile(d.env)
	var err error
	d.old, d.hasLock, err = lock.ReadIn(lockPath, lock.ScopeOf(d.env))
	switch {
	case err != nil:
		d.lockBad = true
		fix := "remove " + lockPath + ", then livery update" + projectArg(d.env)
		if d.project() {
			fix = "restore " + lockPath + " from git, or remove it, then livery update" + projectArg(d.env)
		}
		if errors.Is(err, lock.ErrNewer) {
			fix = "upgrade livery"
		}
		d.line("finding", "lock", exit.OneLine(err, " "), fix)
	case !d.hasLock:
		d.line("info", "lock", "nothing installed yet", "livery update"+projectArg(d.env))
	default:
		d.line("ok", "lock", lockPath, "")
	}
}

// checkHarnesses checks that the folder of each harness exists. Machine only: whether the
// harness is installed here says nothing about a project.
func (d *doctorRun) checkHarnesses(cfg config.Config) {
	if d.project() {
		return
	}
	for _, name := range cfg.Harnesses {
		var root string
		for _, a := range engine.Adapters() {
			if a.Name() == name {
				root = a.Root(d.env)
			}
		}
		if st, err := os.Stat(root); err == nil && st.IsDir() {
			d.line("ok", "harness "+name, lock.ScopeOf(d.env).Show(root), "")
			continue
		}
		var others []string
		for _, h := range cfg.Harnesses {
			if h != name {
				others = append(others, "--harness "+h)
			}
		}
		rest := strings.Join(others, " ")
		if rest == "" {
			rest = "--harness <another harness>"
		}
		d.line("finding", "harness "+name, lock.ScopeOf(d.env).Show(root)+" does not exist",
			fmt.Sprintf("start %s once, or livery setup %s", name, rest))
	}
}

// checkSource fetches the source, shows what is installed and computes the change; ok is
// false when that fails.
func (d *doctorRun) checkSource(cfg config.Config) (engine.Prepared, bool) {
	fetcher := &engine.Fetcher{Retry: "livery doctor" + projectArg(d.env)}
	if _, _, err := engine.Teams(fetcher, d.env, cfg.Source, cfg.Channel); err != nil {
		d.line("finding", "source", exit.OneLine(err, " "), fmt.Sprintf("check \"source\" in %s", paths.ConfigFile(d.env)))
		return engine.Prepared{}, false
	}
	d.line("ok", "source", fmt.Sprintf("%s (channel %s)", cfg.Source, cfg.Channel), "")
	if !d.lockBad && d.hasLock {
		d.line("ok", "installed", fmt.Sprintf("%s (%s)", tagLabel(d.old.Tag), source.ShortCommit(d.old.Commit)), "")
	}
	p, err := engine.Prepare(fetcher, d.env, version(), cfg)
	if err != nil {
		// Not a source error: the message names its own fix. A bad lock is already a finding.
		if errors.Is(err, engine.ErrPersonal) {
			d.line("finding", "personal", exit.OneLine(err, " "), "")
		} else if !d.lockBad {
			d.line("finding", "install", exit.OneLine(err, " "), "")
		}
		return p, false
	}
	if p.MovedTag != "" {
		d.line("info", "tag", fmt.Sprintf("%s moved from %s to %s", p.Resolved.Tag, p.TagFrom, p.TagTo), "")
	}
	if c := cfg.Channel; c != "stable" && c != "next" && p.Resolved.Newest != "" && source.Newer(p.Resolved.Newest, c) {
		d.line("info", "release", fmt.Sprintf("%s is the newest release, the channel is pinned to %s", p.Resolved.Newest, c), "livery setup"+projectArg(d.env)+" --channel stable")
	}
	return p, true
}

// checkEdited shows the installed items the user changed by hand.
func (d *doctorRun) checkEdited(p engine.Prepared) {
	n := 0
	for _, a := range p.Result.Actions {
		if a.Op != apply.Edited {
			continue
		}
		del := "delete it"
		if a.Key != "" && !a.List {
			del = "remove the entry"
		}
		fix := del + " to take the team's version, or keep yours in the personal layer; livery update prints the steps"
		if d.project() {
			fix = del + ", then livery update --project"
		}
		d.line("finding", "edited", d.subject(a), fix)
		n++
	}
	if n == 0 {
		d.line("ok", "edited", "no installed file was changed by hand", "")
	}
}

// checkKeptYours shows the files of the user that are in the way of the shared setup.
func (d *doctorRun) checkKeptYours(p engine.Prepared) {
	n := 0
	for _, a := range p.Result.Actions {
		if a.Op == apply.KeptYours {
			d.line("finding", "kept, yours", d.subject(a), "rename or remove yours, then livery update"+projectArg(d.env))
			n++
		}
	}
	if n == 0 {
		d.line("ok", "kept, yours", "nothing of yours is in the way", "")
	}
}

// checkMissing shows installed files that are gone. In a project the installed files are
// committed: a missing one is drift.
func (d *doctorRun) checkMissing(p engine.Prepared) {
	if !d.project() {
		return
	}
	for _, a := range p.Result.Actions {
		if a.Missing {
			d.line("finding", "missing", d.subject(a), "livery update --project")
		}
	}
}

// showOverrides shows the items one layer replaces in another.
func (d *doctorRun) showOverrides(p engine.Prepared) {
	for _, o := range p.Overrides {
		d.line("info", "override", fmt.Sprintf("%s: %s replaces %s: %s",
			o.Winner.Layer, o.Winner.Origin, o.Loser.Layer, o.Loser.Origin), "")
	}
}

// showUnset shows the unset variables of MCP servers: info only, never a finding.
func (d *doctorRun) showUnset(p engine.Prepared) {
	for _, u := range p.Unset {
		d.line("info", "mcp "+u.Server, u.Problem(), u.Fix())
	}
}

// checkUpdate says whether livery update would change something.
func (d *doctorRun) checkUpdate(p engine.Prepared) {
	installed := !d.lockBad && d.hasLock
	old, tag, fix := d.old, p.Resolved.Tag, "livery update"+projectArg(d.env)
	switch {
	case installed && old.Tag != "" && tag != "" && source.Newer(old.Tag, tag):
		// A channel pinned to an older tag: livery update goes back to it.
		d.line("info", "update", fmt.Sprintf("the channel is pinned to %s, older than the installed %s", tag, old.Tag), fix+" moves back to "+tag)
	case installed && old.Tag != "" && tag != "" && old.Tag != tag:
		d.line("info", "update", fmt.Sprintf("%s is available (installed: %s)", tag, old.Tag), fix)
	case installed && old.Tag == "" && tag == "" && old.Commit != "" && old.Commit != p.Resolved.Commit:
		d.line("info", "update", "a newer commit is available", fix)
	case p.Result.Changes():
		d.line("info", "update", "changes pending", fix)
	default:
		d.line("ok", "update", "up to date", "")
	}
}
