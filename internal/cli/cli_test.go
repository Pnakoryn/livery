package cli

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
)

func TestRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		args      []string
		code      int
		stdoutHas string
		stderrHas string
	}{
		{"version", []string{"version"}, 0, "livery " + Version, ""},
		{"help", []string{"help"}, 0, "Usage: livery", ""},
		{"no command prints the help", nil, 0, "Start:", ""},
		{"unknown command is wrong usage", []string{"frobnicate"}, 2, "", `"frobnicate"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if got := Run(c.args, &out, &errOut); got != c.code {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", got, c.code, errOut.String())
			}
			if !strings.Contains(out.String(), c.stdoutHas) {
				t.Errorf("stdout = %q, want it to contain %q", out.String(), c.stdoutHas)
			}
			if !strings.Contains(errOut.String(), c.stderrHas) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), c.stderrHas)
			}
		})
	}
}

// failReader fails the test when read.
type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Error("stdin was read")
	return 0, os.ErrClosed
}

// runCLI runs the command line. Without a terminal (interactive false) stdin is a
// failReader: any read fails the test, so every such run proves that nothing is read.
func runCLI(t *testing.T, env adapter.Env, stdin string, interactive bool, args ...string) (int, string, string) {
	t.Helper()
	var in io.Reader = failReader{t}
	if interactive {
		in = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	code := RunEnv(args, in, &out, &errb, env, interactive)
	return code, out.String(), errb.String()
}

func helpRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	home := t.TempDir()
	env := adapter.Env{Home: home, Getenv: func(string) string { return "" }, Dir: home}
	return runCLI(t, env, "", false, args...)
}

func TestCommandHelp(t *testing.T) {
	t.Parallel()
	flags := map[string][]string{
		"setup":     {"-source", "-channel", "-team", "-harness", "-plan", "-project"},
		"update":    {"-plan", "-project"},
		"uninstall": {"-plan", "-project"},
		"import":    {"-from", "-into", "-team", "-only", "-plan", "-project"},
		"list":      {"-source", "-channel", "-project"},
		"check":     nil,
		"doctor":    {"-project"},
	}
	for cmd, names := range flags {
		for _, spell := range []string{"--help", "-h", "-help", "help"} {
			args := []string{cmd, spell}
			if spell == "help" {
				args = []string{"help", cmd}
			}
			code, out, errs := helpRun(t, args...)
			if code != 0 || errs != "" || !strings.HasPrefix(out, "Usage: livery "+cmd) {
				t.Errorf("%v: code %d stderr %q stdout %q", args, code, errs, out)
			}
			for _, n := range names {
				if !strings.Contains(out, n) {
					t.Errorf("%v: no %s in\n%s", args, n, out)
				}
			}
		}
	}
}

func TestHelpDoesNoWork(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := adapter.Env{Home: home, Getenv: func(string) string { return "" }, Dir: home}
	code, out, _ := runCLI(t, env, "", false, "setup", "--source", "x", "--help")
	if code != 0 || strings.Contains(out, "cannot fetch") {
		t.Errorf("code %d out %q", code, out)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Errorf("home after setup --help: %v, %v; want empty", entries, err)
	}
	if code, _, _ := helpRun(t, "help", "nosuch"); code != 2 {
		t.Errorf("help nosuch: %d", code)
	}
	if code, _, errs := helpRun(t, "setup", "--bogus"); code != 2 || !strings.Contains(errs, "livery setup --help") {
		t.Errorf("bogus: %d %q", code, errs)
	}
}

// Bare livery prints the root help on stdout and exits 0; the help says what livery is,
// where to start, and which flags most commands take.
func TestRootHelp(t *testing.T) {
	t.Parallel()
	code, out, errs := helpRun(t)
	if code != 0 || errs != "" || !strings.HasPrefix(out, "livery installs and updates") {
		t.Fatalf("code %d stderr %q stdout %q", code, errs, out)
	}
	for _, want := range []string{"Start:  livery setup --source", "Later:  livery update", "Most commands take --plan", "Run livery <command> --help"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Options:") {
		t.Errorf("Options block still there:\n%s", out)
	}
}

// Command help prints every flag with two dashes and a value name, and ends with examples.
func TestCommandHelpFormat(t *testing.T) {
	t.Parallel()
	singleDash := regexp.MustCompile(`(?m)^\s+-[a-z]`)
	for _, cmd := range []string{"setup", "update", "uninstall", "import", "list", "check", "doctor"} {
		code, out, _ := helpRun(t, cmd, "--help")
		if code != 0 || !strings.Contains(out, "\nExamples:\n  livery "+cmd) {
			t.Errorf("%s --help: code %d\n%s", cmd, code, out)
		}
		if singleDash.MatchString(out) {
			t.Errorf("%s --help has a single-dash flag:\n%s", cmd, out)
		}
		if strings.Contains(out, " string") || strings.Contains(out, " value") {
			t.Errorf("%s --help has a placeholder type:\n%s", cmd, out)
		}
	}
	_, out, _ := helpRun(t, "setup", "--help")
	for _, want := range []string{
		"  --channel <channel>   stable (newest release, the default)",
		"  --harness <name>      coding agent to set up: kiro, claude or opencode; repeat for several; replaces the saved list",
		"  --source <address>    git URL or folder of your team's shared repository",
		`  --team <name>         team to install; repeat for several; replaces the saved list; --team "" for none`,
		"  --plan                show the change and write nothing",
		"Without a terminal setup never asks",
		"a later setup keeps the saved source, harnesses and teams unless the flag is given",
		"livery setup --channel v1.2.0   # stay on one release",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("setup --help: no %q in\n%s", want, out)
		}
	}
	_, out, _ = helpRun(t, "import", "--help")
	for _, want := range []string{"--into <folder>", "--only <kind/name>", "--from <name>", "--team <name>"} {
		if !strings.Contains(out, want) {
			t.Errorf("import --help: no %q in\n%s", want, out)
		}
	}
	_, out, _ = helpRun(t, "list", "--help")
	if !strings.Contains(out, "(use with --source)") {
		t.Errorf("list --help:\n%s", out)
	}
}

// Flag errors name the flag, the value and what is allowed, never a config key, and point to
// the command's help.
func TestFlagErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"setup", "--frob"}, []string{"setup: unknown flag --frob; run livery setup --help"}},
		{[]string{"setup", "-frob"}, []string{"unknown flag --frob"}},
		{[]string{"setup", "--source"}, []string{"setup: --source needs a value; run livery setup --help"}},
		{[]string{"update", "stray"}, []string{`update: unexpected argument "stray"; run livery update --help`}},
		{[]string{"update", "--plan=maybe"}, []string{"run livery update --help"}},
		{[]string{"setup", "--source", "S", "--harness", "vim"}, []string{"setup: --harness vim: unknown; valid: kiro, claude, opencode"}},
		{[]string{"setup", "--source", "S", "--harness", "kiro", "--channel", "latest"}, []string{"setup: --channel latest: use stable, next, or a release tag such as v1.2.0"}},
		{[]string{"setup", "--source", "S", "--harness", "kiro", "--team", "My_Team"}, []string{"setup: --team My_Team: invalid name; use lowercase letters, digits and '-'"}},
		{[]string{"setup", "--source", "S", "--harness", "kiro", "--harness", "kiro"}, []string{"setup: --harness kiro is given twice"}},
		{[]string{"setup", "--source", "S", "--harness", "kiro", "--team", "web", "--team", "web"}, []string{"setup: --team web is given twice"}},
	}
	for _, c := range cases {
		code, out, errs := helpRun(t, c.args...)
		if code != 2 || strings.Contains(errs, `key "`) {
			t.Errorf("%v: code %d stdout %q stderr %q", c.args, code, out, errs)
		}
		for _, w := range c.want {
			if !strings.Contains(errs, w) {
				t.Errorf("%v: stderr %q, want %q", c.args, errs, w)
			}
		}
	}
	// Single-dash spelling still parses: the error is about the value, not the flag.
	if _, _, errs := helpRun(t, "setup", "-source", "S", "-harness", "vim"); !strings.Contains(errs, "--harness vim") {
		t.Errorf("single dash: %q", errs)
	}
}
