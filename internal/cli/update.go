package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

// update implements `livery update [--plan]`. It never prompts.
func update(args []string, stdout io.Writer, env adapter.Env) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	plan := fs.Bool("plan", false, "show the change and write nothing")
	project := projectFlag(fs, "work on the project around the working directory")
	if err := parseFlags("update", fs, args); err != nil {
		return err
	}
	env, err := scoped(env, *project, false)
	if err != nil {
		return err
	}
	if *project {
		_, _ = fmt.Fprintf(stdout, "project: %s\n", env.Project)
	}
	projectNote(stdout, env, "update")
	path := paths.ConfigFile(env)
	cfg, found, err := config.Load(path, "livery setup"+projectArg(env))
	if err != nil {
		return err
	}
	if !found {
		return exit.Usagef("%s does not exist; run `livery setup` first", path)
	}
	if err := cfg.Validate(path, engine.Names(), "livery setup"+projectArg(env)); err != nil {
		return err
	}
	c, err := prepareChange(&engine.Fetcher{Retry: "livery update" + projectArg(env)}, "update", cfg, *plan, stdout, env)
	if err != nil || *plan {
		return err
	}
	defer c.release()
	return applyChange(c, stdout, env, "")
}
