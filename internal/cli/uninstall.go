package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/apply"
	"github.com/Pnakoryn/livery/internal/engine"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
)

// uninstall implements `livery uninstall [--plan]`: it removes what the lock names and the
// user has not changed. It works from the lock alone: no config, no network.
func uninstall(args []string, stdout io.Writer, env adapter.Env) error {
	fs2 := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	plan := fs2.Bool("plan", false, "show the change and write nothing")
	project := projectFlag(fs2, "work on the project around the working directory")
	if err := parseFlags("uninstall", fs2, args); err != nil {
		return err
	}
	env, err := scoped(env, *project, false)
	if err != nil {
		return err
	}
	if *project {
		_, _ = fmt.Fprintf(stdout, "project: %s\n", env.Project)
	}
	projectNote(stdout, env, "uninstall")
	if !*plan {
		release, err := takeRunLock(env, "uninstall")
		if err != nil {
			return err
		}
		defer release()
	}
	lockPath := paths.LockFile(env)
	old, found, err := lock.ReadIn(lockPath, lock.ScopeOf(env))
	if err != nil {
		return err
	}
	if !found {
		_, _ = fmt.Fprintln(stdout, "nothing installed")
		return nil
	}
	var plans []adapter.Plan
	for _, a := range engine.Adapters() {
		plans = append(plans, adapter.Plan{Root: a.Root(env), EntryKeys: a.EntryKeys(env)})
	}
	res, err := apply.Compute(plans, old, lock.Lock{}, lock.ScopeOf(env), "uninstall")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, "livery uninstall")
	apply.Report(stdout, res, !*plan)
	if *plan {
		// The list says what goes; the old content stays on disk until the run.
		_, _ = fmt.Fprintln(stdout, planLine)
		return nil
	}
	if now, err := apply.Execute(res); err != nil {
		return recordPartial(lockPath, old, now, err, res.UpdateCmd())
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return exit.Environmentf("%s: cannot remove: %v; check its permissions, then run %s", lockPath, err, res.UpdateCmd())
	}
	scope := lock.ScopeOf(env)
	if _, err := os.Stat(paths.ConfigFile(env)); err == nil {
		_, _ = fmt.Fprintf(stdout, "kept    %s\n", scope.Show(paths.ConfigFile(env)))
	}
	if personal := paths.PersonalDir(env); personal != "" {
		if _, err := os.Stat(personal); err == nil {
			_, _ = fmt.Fprintf(stdout, "kept    %s\n", scope.Show(personal))
		}
	}
	_, _ = fmt.Fprintf(stdout, "done    %d removed; to install again: livery update%s; to remove livery's own files too: delete %s\n",
		appliedCount(res), projectArg(env), scope.Show(filepath.Dir(paths.ConfigFile(env))))
	return nil
}
