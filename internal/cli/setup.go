package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/config"
	"github.com/Pnakoryn/livery/internal/engine"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/paths"
	"github.com/Pnakoryn/livery/internal/source"
)

// listFlag is a repeatable string flag that remembers whether it was given.
type listFlag struct {
	set    bool
	empty  bool
	values []string
}

func (l *listFlag) String() string { return strings.Join(l.values, ",") }

func (l *listFlag) Set(v string) error {
	l.set = true
	if v == "" {
		l.empty = true
	} else {
		l.values = append(l.values, v)
	}
	return nil
}

// setup implements `livery setup`: it fills the config from the flags, the existing config and
// (in a terminal) prompts, saves it and installs like `update`.
func setup(args []string, stdin io.Reader, stdout io.Writer, env adapter.Env, interactive bool) error {
	s, err := parseSetup(args, stdin, stdout, env, interactive)
	if err != nil {
		return err
	}
	if err := s.chooseScope(); err != nil {
		return err
	}
	cfg, err := s.resolveConfig()
	if err != nil {
		return err
	}
	if s.plan {
		return s.showPlan(cfg)
	}
	return s.apply(cfg)
}

// setupRun is one run of setup: its flags, where it works and what it learned on the way.
type setupRun struct {
	src, channel     string
	teams, harnesses listFlag
	plan, project    bool

	in          *bufio.Reader
	stdout      io.Writer
	env         adapter.Env
	interactive bool
	// asked is set once setup asked a question: it then asks "Apply?" before it writes.
	asked bool
	// path is the config file; found says whether it existed.
	path    string
	found   bool
	fetcher *engine.Fetcher
}

// parseSetup reads and checks the flags of setup.
func parseSetup(args []string, stdin io.Reader, stdout io.Writer, env adapter.Env, interactive bool) (*setupRun, error) {
	s := &setupRun{in: bufio.NewReader(stdin), stdout: stdout, env: env, interactive: interactive}
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.StringVar(&s.src, "source", "", "`address` git URL or folder of your team's shared repository")
	fs.StringVar(&s.channel, "channel", "", "`channel` stable (newest release, the default), next (newest commit), or a release tag such as v1.2.0")
	fs.Var(&s.teams, "team", "`name` team to install; repeat for several; replaces the saved list; --team \"\" for none")
	fs.Var(&s.harnesses, "harness", "`name` coding agent to set up: "+orList(engine.Names())+"; repeat for several; replaces the saved list")
	fs.BoolVar(&s.plan, "plan", false, "show the change and write nothing")
	project := projectFlag(fs, "set up the project around the working directory")
	if err := parseFlags("setup", fs, args); err != nil {
		return nil, err
	}
	s.project = *project
	if err := checkSetupFlags(s.channel, &s.teams, &s.harnesses); err != nil {
		return nil, err
	}
	return s, nil
}

// chooseScope asks for the scope in a terminal inside a git repository when there is no
// config and a choice is left open, then moves s.env into the scope.
func (s *setupRun) chooseScope() error {
	if s.interactive && !s.project && (s.src == "" || !s.harnesses.set || !s.teams.set) {
		if _, err := os.Stat(paths.ConfigFile(s.env)); os.IsNotExist(err) && inGitRepo(s.env.Dir) {
			s.asked = true
			s.project, err = askScope(s.in, s.stdout, s.env)
			if err != nil {
				return err
			}
		}
	}
	env, err := scoped(s.env, s.project, true)
	if err != nil {
		return err
	}
	s.env = env
	projectNote(s.stdout, env, "setup")
	if s.project {
		_, _ = fmt.Fprintf(s.stdout, "project: %s\n", env.Project)
	}
	if s.harnesses.empty {
		return exit.Usagef("setup: --harness is empty; pass --harness with one of: %s", strings.Join(engine.Names(), ", "))
	}
	return nil
}

// resolveConfig builds the config from the saved one, the flags and (in a terminal) questions,
// fetches the source for its teams and checks the result.
func (s *setupRun) resolveConfig() (config.Config, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return cfg, err
	}
	if err := s.chooseSource(&cfg); err != nil {
		return cfg, err
	}
	s.fetcher = &engine.Fetcher{Retry: "livery setup" + projectArg(s.env)}
	teams, _, err := engine.Teams(s.fetcher, s.env, cfg.Source, cfg.Channel)
	if err != nil {
		return cfg, err
	}
	if err := s.chooseHarnesses(&cfg); err != nil {
		return cfg, err
	}
	if err := s.chooseTeams(&cfg, teams); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(s.path, engine.Names(), "livery setup"+projectArg(s.env)); err != nil {
		return cfg, err
	}
	if m := engine.MissingTeam(cfg.Source, cfg.Teams, teams); m != "" {
		return cfg, exit.Usagef("setup: %s", m)
	}
	return cfg, nil
}

// loadConfig reads the saved config of the scope and puts the flags given on top.
func (s *setupRun) loadConfig() (config.Config, error) {
	s.path = paths.ConfigFile(s.env)
	cfg, found, err := config.Load(s.path, "livery setup"+projectArg(s.env))
	if err != nil {
		return cfg, err
	}
	s.found = found
	if s.src != "" {
		cfg.Source = expandHome(s.env, s.src)
	}
	if s.channel != "" {
		cfg.Channel = s.channel
	}
	if cfg.Channel == "" {
		cfg.Channel = "stable"
	}
	if s.harnesses.set {
		cfg.Harnesses = s.harnesses.values
	}
	if s.teams.set {
		cfg.Teams = s.teams.values
	}
	return cfg, nil
}

// chooseSource asks for the source when the config has none and makes it canonical.
func (s *setupRun) chooseSource(cfg *config.Config) error {
	if cfg.Source == "" {
		if !s.interactive {
			return exit.Usagef("setup: no source; pass --source <git URL or path>")
		}
		s.asked = true
		v, err := ask("setup", s.in, s.stdout, "Address of your team's shared repository (git URL or folder): ", "pass --source", func(a string) (string, bool) {
			return a, a != ""
		})
		if err != nil {
			return err
		}
		cfg.Source = expandHome(s.env, v)
	}
	cfg.Source = source.Canonical(cfg.Source)
	return nil
}

// chooseHarnesses fills the harnesses when the config has none: asked in a terminal, else
// the harnesses whose folder exists.
func (s *setupRun) chooseHarnesses(cfg *config.Config) error {
	if len(cfg.Harnesses) > 0 {
		return nil
	}
	names := engine.Names()
	var detected []string
	for _, a := range engine.Adapters() {
		if st, err := os.Stat(a.Root(s.env)); err == nil && st.IsDir() {
			detected = append(detected, a.Name())
		}
	}
	if !s.interactive {
		if len(detected) == 0 {
			return exit.Usagef("setup: no harness folder found; pass --harness with one of: %s", strings.Join(names, ", "))
		}
		cfg.Harnesses = detected
		return nil
	}
	s.asked = true
	var err error
	cfg.Harnesses, err = askHarnesses(s.in, s.stdout, names, detected)
	return err
}

// chooseTeams asks for the teams in a terminal on the first setup when --team is not given
// and the source has teams.
func (s *setupRun) chooseTeams(cfg *config.Config, teams []string) error {
	if s.teams.set || s.found || !s.interactive || len(teams) == 0 {
		return nil
	}
	s.asked = true
	for i, t := range teams {
		_, _ = fmt.Fprintf(s.stdout, "  %d. %s\n", i+1, t)
	}
	idx, err := askNumbers("setup", s.in, s.stdout, "Teams to add to the base setup (numbers, comma-separated, empty for none): ", "pass --team", len(teams), true)
	if err != nil {
		return err
	}
	cfg.Teams = nil
	for _, i := range idx {
		cfg.Teams = append(cfg.Teams, teams[i])
	}
	return nil
}

// showPlan prints the config setup would save and the change it would make.
func (s *setupRun) showPlan(cfg config.Config) error {
	data, err := cfg.JSON()
	if err != nil {
		return err
	}
	var line bytes.Buffer
	if err := json.Compact(&line, data); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(s.stdout, "config  would save %s: %s\n", s.path, line.Bytes())
	_, err = prepareChange(s.fetcher, "setup", cfg, true, s.stdout, s.env)
	return err
}

// apply prepares and reports the change, asks "Apply?" when setup asked a question before,
// then saves the config and applies the change. A no writes nothing.
func (s *setupRun) apply(cfg config.Config) error {
	c, err := prepareChange(s.fetcher, "setup", cfg, false, s.stdout, s.env)
	if err != nil {
		return err
	}
	defer c.release()
	if s.asked {
		ok, err := s.confirm(cfg)
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(s.stdout, "nothing written")
			return nil
		}
	}
	if err := s.saveConfig(cfg); err != nil {
		return err
	}
	hint := ""
	if s.project && !s.found {
		hint = fmt.Sprintf("commit %s/ and the files above so your team gets the same setup", paths.ProjectDir)
	}
	return applyChange(c, s.stdout, s.env, hint)
}

// confirm points to the full diff and asks "Apply?"; it returns true for yes.
func (s *setupRun) confirm(cfg config.Config) (bool, error) {
	_, _ = fmt.Fprintf(s.stdout, "full diff: %s\n", planCommand("setup", cfg, s.env))
	v, err := ask("setup", s.in, s.stdout, "Apply? [Y/n] ", "pass --plan", func(a string) (string, bool) {
		switch a {
		case "", "y", "Y":
			return "y", true
		case "n", "N":
			return "n", true
		}
		return a, false
	})
	return v == "y", err
}

// saveConfig writes cfg to the config file and prints "saved" when its content changed.
func (s *setupRun) saveConfig(cfg config.Config) error {
	data, err := cfg.JSON()
	if err != nil {
		return err
	}
	old, readErr := os.ReadFile(s.path)
	changed := readErr != nil || string(old) != string(data)+"\n"
	if err := cfg.Save(s.path); err != nil {
		return err
	}
	if changed {
		_, _ = fmt.Fprintf(s.stdout, "saved   %s\n", lock.ScopeOf(s.env).Show(s.path))
	}
	return nil
}

// askScope asks whether to set up this machine or the project around the working directory and
// returns true for the project.
func askScope(in *bufio.Reader, stdout io.Writer, env adapter.Env) (bool, error) {
	dir, err := filepath.Abs(env.Dir)
	if err != nil {
		dir = env.Dir
	}
	_, _ = fmt.Fprintf(stdout, "Set up: 1. this machine (your user account)  2. this project (%s; the files are committed for everyone)\n", dir)
	v, err := ask("setup", in, stdout, "Scope (1 or 2) [1]: ", "answer 1 for this machine or 2 for the project", func(s string) (string, bool) {
		return s, s == "" || s == "1" || s == "2"
	})
	return v == "2", err
}

// inGitRepo reports whether dir or a folder above it has a .git file or folder.
func inGitRepo(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Lstat(filepath.Join(abs, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}

// orList joins names as "a, b or c".
func orList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// checkSetupFlags checks the values given as flags. Its messages name the flag, the value and
// what is allowed; config keys are config.Validate's business.
func checkSetupFlags(channel string, teams, harnesses *listFlag) error {
	if channel != "" && !source.ValidChannel(channel) {
		return exit.Usagef("setup: --channel %s: use stable, next, or a release tag such as v1.2.0", channel)
	}
	names := engine.Names()
	for i, h := range harnesses.values {
		if !slices.Contains(names, h) {
			return exit.Usagef("setup: --harness %s: unknown; valid: %s", h, strings.Join(names, ", "))
		}
		if slices.Contains(harnesses.values[:i], h) {
			return exit.Usagef("setup: --harness %s is given twice", h)
		}
	}
	for i, t := range teams.values {
		if !config.ValidTeam(t) {
			return exit.Usagef("setup: --team %s: invalid name; use lowercase letters, digits and '-'", t)
		}
		if slices.Contains(teams.values[:i], t) {
			return exit.Usagef("setup: --team %s is given twice", t)
		}
	}
	return nil
}

// ask prints prompt (for command cmd) and reads a line until parse accepts it; the question is
// repeated once. advice ends the error message, e.g. "pass --source".
func ask(cmd string, in *bufio.Reader, stdout io.Writer, prompt, advice string, parse func(string) (string, bool)) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		_, _ = io.WriteString(stdout, prompt)
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return "", exit.Usagef("%s: no valid answer to %q; %s", cmd, strings.TrimSpace(prompt), advice)
		}
		if v, ok := parse(strings.TrimSpace(line)); ok {
			return v, nil
		}
		_, _ = fmt.Fprintln(stdout, "Invalid answer.")
	}
	return "", exit.Usagef("%s: no valid answer to %q; %s", cmd, strings.TrimSpace(prompt), advice)
}

// askNumbers reads comma-separated 1-based numbers up to limit and returns them 0-based, in
// order without duplicates. Empty input gives none when emptyOK.
func askNumbers(cmd string, in *bufio.Reader, stdout io.Writer, prompt, advice string, limit int, emptyOK bool) ([]int, error) {
	var res []int
	_, err := ask(cmd, in, stdout, prompt, advice, func(s string) (string, bool) {
		res = nil
		if s == "" {
			return s, emptyOK
		}
		for _, f := range strings.Split(s, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(f))
			if err != nil || n < 1 || n > limit {
				return s, false
			}
			if !slices.Contains(res, n-1) {
				res = append(res, n-1)
			}
		}
		return s, true
	})
	return res, err
}

func askHarnesses(in *bufio.Reader, stdout io.Writer, names, detected []string) ([]string, error) {
	var def []int
	for i, n := range names {
		mark := ""
		if slices.Contains(detected, n) {
			mark = " (detected)"
			def = append(def, i)
		}
		_, _ = fmt.Fprintf(stdout, "  %d. %s%s\n", i+1, n, mark)
	}
	var nums []string
	for _, i := range def {
		nums = append(nums, strconv.Itoa(i+1))
	}
	prompt := fmt.Sprintf("Coding agents to set up (numbers, comma-separated) [%s]: ", strings.Join(nums, ","))
	idx, err := askNumbers("setup", in, stdout, prompt, "pass --harness", len(names), len(def) > 0)
	if err != nil {
		return nil, err
	}
	if len(idx) == 0 {
		idx = def
	}
	var res []string
	for _, i := range idx {
		res = append(res, names[i])
	}
	return res, nil
}
