package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"reflect"
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

// planCommand is the complete command that shows the full diff of cfg without a terminal.
func planCommand(command string, cfg config.Config, env adapter.Env) string {
	parts := []string{"livery", command}
	if env.Project != "" {
		parts = append(parts, "--project")
	}
	parts = append(parts, "--source", argQuote(cfg.Source))
	if cfg.Channel != "" && cfg.Channel != "stable" {
		parts = append(parts, "--channel", argQuote(cfg.Channel))
	}
	for _, h := range cfg.Harnesses {
		parts = append(parts, "--harness", argQuote(h))
	}
	if len(cfg.Teams) == 0 {
		parts = append(parts, "--team", `""`)
	}
	for _, t := range cfg.Teams {
		parts = append(parts, "--team", argQuote(t))
	}
	return strings.Join(append(parts, "--plan"), " ")
}

// argQuote puts s in single quotes unless it only has characters a shell leaves alone.
func argQuote(s string) string {
	if s != "" && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789@%+=:,./_-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// change is a change prepareChange computed and reported; applyChange applies it.
type change struct {
	p      engine.Prepared
	cfg    config.Config
	goBack bool
	edited []apply.Action
	// release gives the run lock back; the caller calls it when done.
	release func()
}

// prepareChange computes the change for cfg and reports it: the header, the list, the notes and,
// with plan, the full diff and the plan line; without plan the diff of the items the user edited.
// update and setup both call it. Without plan it takes the run lock (before the fetch) and the
// caller releases it with change.release once applyChange returned.
func prepareChange(f *engine.Fetcher, command string, cfg config.Config, plan bool, stdout io.Writer, env adapter.Env) (*change, error) {
	c := &change{cfg: cfg, release: func() {}}
	if !plan {
		release, err := takeRunLock(env, command)
		if err != nil {
			return nil, err
		}
		c.release = release
	}
	var err error
	c.p, err = engine.Prepare(f, env, version(), cfg)
	if err != nil {
		c.release()
		return nil, err
	}
	if err := c.report(command, plan, stdout); err != nil {
		c.release()
		return nil, err
	}
	return c, nil
}

// report prints what prepareChange describes.
func (c *change) report(command string, plan bool, stdout io.Writer) error {
	p := c.p
	tag := tagLabel(p.Resolved.Tag)
	c.goBack = p.OldTag != "" && p.Resolved.Tag != "" && p.OldTag != p.Resolved.Tag
	if c.goBack {
		tag = p.OldTag + " -> " + tag
	}
	_, _ = fmt.Fprintf(stdout, "livery %s: %s %s (%s)\n", command, p.Resolved.Source, tag, source.ShortCommit(p.Resolved.Commit))
	if p.MovedTag != "" {
		_, _ = fmt.Fprintln(stdout, p.MovedTag)
	}
	apply.Report(stdout, p.Result, !plan)
	for _, n := range p.Notes {
		_, _ = fmt.Fprintf(stdout, "note    %s\n", n)
	}
	for _, u := range p.Unset {
		if command == "update" && !setsEntry(p.Result, u.Server) {
			continue
		}
		_, _ = fmt.Fprintln(stdout, u.Line())
	}
	if plan {
		if err := apply.Diff(stdout, p.Result); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, planLine)
		return nil
	}
	c.edited = p.Result.Blocked()
	if len(c.edited) > 0 {
		only := p.Result
		only.Actions = c.edited
		return apply.Diff(stdout, only)
	}
	return nil
}

// applyChange applies c and writes the lock, then prints the way back to a newer release, one
// "edited" line per item the user edited, hint (when not empty) and the "done" line. Edited
// items are skipped and make it return a Finding; an error from Execute or the lock write wins.
func applyChange(c *change, stdout io.Writer, env adapter.Env, hint string) error {
	p := c.p
	lockPath := paths.LockFile(env)
	now, err := apply.Execute(p.Result)
	if err != nil {
		return recordPartial(lockPath, p.Result.Old, now, err, p.Result.UpdateCmd())
	}
	// In a project an unchanged lock is not rewritten: a different livery version alone does not
	// change the committed lock.
	if env.Project == "" || !now.SameAsFile(lockPath) {
		if err := now.Write(lockPath); err != nil {
			return err
		}
	}
	if c.goBack {
		if c.cfg.Channel != "stable" && c.cfg.Channel != "next" {
			_, _ = fmt.Fprintf(stdout, "fixed on %s: livery update will not install newer releases; to follow releases again: livery setup%s --channel stable\n",
				c.cfg.Channel, projectArg(env))
		} else {
			_, _ = fmt.Fprintf(stdout, "to go back: livery setup%s --channel %s\n", projectArg(env), p.OldTag)
		}
	}
	for _, a := range c.edited {
		_, _ = fmt.Fprintln(stdout, editedLine(env, a, p.Result.UpdateCmd()))
	}
	if hint != "" {
		_, _ = fmt.Fprintln(stdout, hint)
	}
	if n := appliedCount(p.Result); n > 0 {
		_, _ = fmt.Fprintf(stdout, "done    %d change(s) applied\n", n)
	}
	if len(c.edited) == 0 {
		return nil
	}
	return exit.Findingf("%d edited by you, kept; the rest is applied", len(c.edited))
}

// setsEntry reports whether this run sets the entry of the MCP server name.
func setsEntry(r apply.Result, name string) bool {
	for _, a := range r.Actions {
		if a.Op == apply.SetEntry && a.Key != "" && !a.List && a.Name == name {
			return true
		}
	}
	return false
}

// editedLine is the block install prints after the run for an item the user edited: livery
// kept it, and these are the ways out. The lines after the first are indented by eight spaces.
func editedLine(env adapter.Env, a apply.Action, next string) string {
	name := actionName(env, a)
	scope := lock.ScopeOf(env)
	layer, origin, found := strings.Cut(a.Origin, ": ")
	if !found {
		layer, origin = "", a.Origin
	}
	undo := "delete " + name
	if a.Key != "" && !a.List {
		undo = fmt.Sprintf("remove the entry %q from %s", a.Name, scope.Show(a.Path))
	}
	const more = "\n        "
	personal := paths.PersonalDir(env)
	if personal == "" {
		return "edited  " + name + ": you changed it; livery kept your version" +
			more + "to take the shared version: " + undo + ", then run livery update --project; to change it for everyone, change it in the shared repository"
	}
	inPersonal := func(rel string) string {
		return lock.Tilde(env.Home, filepath.Join(personal, filepath.FromSlash(rel)))
	}
	skillRest, isSkill := belowSkill(origin, a.Path)
	if layer == "personal" {
		file := inPersonal(origin)
		if isSkill {
			file = inPersonal(origin + "/" + skillRest)
		}
		return "edited  " + name + ": you changed it; livery kept your version" +
			more + "to keep your change: put it into " + file + ", " + undo + ", then run " + next
	}
	var keep string
	switch {
	case isSkill:
		folder := strings.TrimSuffix(filepath.ToSlash(a.Path), "/"+skillRest)
		keep = "copy the folder " + scope.Show(filepath.FromSlash(folder)) + " to " + inPersonal(origin)
	case strings.HasPrefix(origin, "instructions/") && strings.HasSuffix(origin, ".md"):
		keep = "put your text into " + inPersonal(origin) + " (Markdown; the only frontmatter keys are description and globs)"
	case strings.HasPrefix(origin, "mcp/") && strings.HasSuffix(origin, ".json"):
		keep = "write the server as " + inPersonal(origin) + " (one JSON object; the only keys are command, args, env, url, headers)"
	default:
		keep = "put your version at " + inPersonal(origin)
	}
	return "edited  " + name + ": you changed it; livery kept your version and did not install the team's" +
		more + "to take the team's version: " + undo + ", then run " + next +
		more + "to keep your change: " + keep + ", " + undo + ", then run " + next
}

// belowSkill returns the path of the file at path below its skill folder when origin is a skill
// folder ("skills/<name>") and path lies inside a folder of that name; ok is false otherwise.
func belowSkill(origin, path string) (string, bool) {
	if strings.Count(origin, "/") != 1 || !strings.HasPrefix(origin, "skills/") {
		return "", false
	}
	_, rest, found := strings.Cut(filepath.ToSlash(path), "/"+origin+"/")
	return rest, found && rest != ""
}

// actionName names a in messages as apply.Target does, with the path as the scope shows it.
func actionName(env adapter.Env, a apply.Action) string {
	return apply.Target(lock.ScopeOf(env).Show(a.Path), a)
}

// recordPartial handles an Execute that failed with runErr after now became the true lock:
// when now differs from old it writes now to lockPath, so the files already written stay
// livery's. It returns runErr, or an Environment error naming lockPath when the lock itself
// cannot be written.
func recordPartial(lockPath string, old, now lock.Lock, runErr error, next string) error {
	if reflect.DeepEqual(old, now) {
		return runErr
	}
	if err := now.Write(lockPath); err != nil {
		return exit.Environmentf("%s: cannot record the files already changed (%v) after: %v; fix the problem, then run %s", lockPath, err, runErr, next)
	}
	return runErr
}

// planLine is the last line of a --plan run.
const planLine = "plan    nothing was written; run the same command without --plan to apply"

// appliedCount is the number of actions of r that an applying run carries out.
func appliedCount(r apply.Result) int {
	n := 0
	for _, a := range r.Actions {
		switch a.Op {
		case apply.Write, apply.Remove, apply.Adopt, apply.SetEntry, apply.UnsetEntry:
			n++
		}
	}
	return n
}

// projectNote prints a note when a machine-scope command runs inside a livery project.
func projectNote(stdout io.Writer, env adapter.Env, command string) {
	if env.Project != "" {
		return
	}
	if root, ok := paths.FindProject(env.Dir); ok {
		_, _ = fmt.Fprintf(stdout, "note    this folder is a livery project (%s); for the project run livery %s --project\n", root, command)
	}
}
