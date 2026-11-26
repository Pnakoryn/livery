// Package cli parses the command line and runs the chosen command.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"syscall"
	"unsafe"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
)

// Version is the released version; the build sets it with -ldflags "-X github.com/Pnakoryn/livery/internal/cli.Version=...".
var Version = "0.0.0-dev"

const defaultVersion = "0.0.0-dev"

// readBuildInfo is replaced in tests.
var readBuildInfo = debug.ReadBuildInfo

// version returns the version everything reads: the -ldflags value when set, else the main
// module's version of a `go install` build, else the default.
func version() string {
	if Version != defaultVersion {
		return Version
	}
	if bi, ok := readBuildInfo(); ok && bi != nil {
		if v := strings.TrimPrefix(bi.Main.Version, "v"); v != "" && bi.Main.Version != "(devel)" {
			return v
		}
	}
	return defaultVersion
}

const usage = `livery installs and updates your team's shared coding-agent setup (skills, instructions, MCP servers).

Start:  livery setup --source <address of your team's shared repository>
Later:  livery update

Usage: livery <command> [flags]

Commands:
  setup     Create or change the config, then install
  update    Install or update the shared setup from the configured source
  uninstall Remove what livery installed and you did not change; keep the config
  import    Write a coding agent's folder into a shared repository folder
  list      Show the teams and items the shared repository offers, or with --installed what is installed
  check     Validate a shared repository folder and scan it for secrets
  doctor    Check the setup: one line per check, exit 1 on a finding
  version   Print the version
  help      Print this message

Most commands take --plan (show the change, write nothing) and --project (work on the project around this folder, not the machine).
Run livery <command> --help for a command's flags and examples.
`

// cmdHelp is the help of a command that takes flags: the usage line, a description, an
// optional note printed after the flags, and examples.
type cmdHelp struct {
	usage, desc, note string
	examples          []string
}

var commandHelp = map[string]cmdHelp{
	"setup": {
		usage: "livery setup [flags]",
		desc:  "Create or change the config from the flags, the existing config and prompts, then install.",
		note:  "Without a terminal setup never asks. On the first setup --source is required, --harness defaults to the coding agents whose folder exists, --team defaults to none; a later setup keeps the saved source, harnesses and teams unless the flag is given.",
		examples: []string{
			"livery setup --source git@example.com:team/agent-setup.git",
			"livery setup --team web --team platform",
			"livery setup --channel v1.2.0   # stay on one release",
		},
	},
	"update": {
		usage:    "livery update [flags]",
		desc:     "Install or update the shared setup from the configured source.",
		examples: []string{"livery update", "livery update --plan"},
	},
	"uninstall": {
		usage:    "livery uninstall [flags]",
		desc:     "Remove what livery installed and you did not change; keep the config.",
		examples: []string{"livery uninstall --plan", "livery uninstall --project"},
	},
	"import": {
		usage: "livery import [flags]",
		desc:  "Write a coding agent's folder into a shared repository folder; it never commits.",
		examples: []string{
			"livery import --from claude --into ~/src/agent-setup",
			"livery import --from kiro --into ~/src/agent-setup --team web --only skill/review",
		},
	},
	"list": {
		usage:    "livery list [flags]",
		desc:     "Show the teams and items the shared repository offers, or with --installed what is installed.",
		examples: []string{"livery list", "livery list --source git@example.com:team/agent-setup.git --channel next"},
	},
	"check": {
		usage:    "livery check [DIR]",
		desc:     "Validate a shared repository folder (default: the working directory) and scan it for secrets.",
		examples: []string{"livery check", "livery check ~/src/agent-setup"},
	},
	"doctor": {
		usage:    "livery doctor [flags]",
		desc:     "Check the setup: one line per check, exit 1 on a finding.",
		examples: []string{"livery doctor", "livery doctor --project"},
	},
}

// helpError carries the help text of a command whose --help was asked for.
type helpError struct{ text string }

func (e *helpError) Error() string { return "help requested" }

// newHelp builds the help text of command name from its flag set: one line per flag with two
// dashes, the value name (the backquoted word that starts the usage text) and the description.
func newHelp(name string, fs *flag.FlagSet) *helpError {
	h := commandHelp[name]
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s\n\n%s\n", h.usage, h.desc)
	var heads, descs []string
	fs.VisitAll(func(f *flag.Flag) {
		head := "--" + f.Name
		desc := f.Usage
		if strings.HasPrefix(desc, "`") {
			value, _ := flag.UnquoteUsage(f)
			head += " <" + value + ">"
			desc = strings.TrimPrefix(desc, "`"+value+"` ")
		}
		heads = append(heads, head)
		descs = append(descs, desc)
	})
	if len(heads) > 0 {
		width := 0
		for _, s := range heads {
			width = max(width, len(s))
		}
		b.WriteString("\nFlags:\n")
		for i := range heads {
			fmt.Fprintf(&b, "  %-*s   %s\n", width, heads[i], descs[i])
		}
	}
	if h.note != "" {
		fmt.Fprintf(&b, "\n%s\n", h.note)
	}
	b.WriteString("\nExamples:\n")
	for _, e := range h.examples {
		fmt.Fprintf(&b, "  %s\n", e)
	}
	return &helpError{b.String()}
}

// Run executes livery with args (without the program name) and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "livery: cannot find the home directory: %v; set HOME and run livery again\n", err)
		return int(exit.Environment)
	}
	dir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "livery: cannot find the working directory: %v; cd into an existing folder and run livery again\n", err)
		return int(exit.Environment)
	}
	env := adapter.Env{Home: home, Getenv: os.Getenv, Dir: dir}
	return RunEnv(args, os.Stdin, stdout, stderr, env, isTerminal(os.Stdin) && isTerminal(stdout))
}

// isTerminal reports whether v is a file whose terminal attributes can be read; /dev/null, pipes
// and regular files are not terminals.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), termGet, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// RunEnv is Run with the process's home, environment and terminal state passed in.
func RunEnv(args []string, stdin io.Reader, stdout, stderr io.Writer, env adapter.Env, interactive bool) int {
	err := run(args, stdin, stdout, env, interactive)
	if err == nil {
		return 0
	}
	var h *helpError
	if errors.As(err, &h) {
		if _, err := io.WriteString(stdout, h.text); err != nil {
			_, _ = fmt.Fprintf(stderr, "livery: %s\n", err)
			return int(exit.Finding)
		}
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "livery: %s\n", err)
	var e *exit.Error
	if errors.As(err, &e) {
		return int(e.Code)
	}
	return int(exit.Finding)
}

// parseFlags parses args for command name into fs; a stray argument or unknown flag is a usage error.
func parseFlags(name string, fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return flagError(name, fs, err)
	}
	if fs.NArg() > 0 {
		return exit.Usagef("%s: unexpected argument %q; run livery %s --help", name, fs.Arg(0), name)
	}
	return nil
}

// flagError turns an error of fs.Parse for command name into the help text or a usage error
// that names the flag and the command's help.
func flagError(name string, fs *flag.FlagSet, err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return newHelp(name, fs)
	}
	msg := err.Error()
	if f, ok := strings.CutPrefix(msg, "flag provided but not defined: "); ok {
		msg = "unknown flag --" + strings.TrimLeft(f, "-")
	} else if f, ok := strings.CutPrefix(msg, "flag needs an argument: "); ok {
		msg = "--" + strings.TrimLeft(f, "-") + " needs a value"
	}
	return exit.Usagef("%s: %s; run livery %s --help", name, msg, name)
}

func run(args []string, stdin io.Reader, stdout io.Writer, env adapter.Env, interactive bool) error {
	if len(args) == 0 {
		_, err := io.WriteString(stdout, usage)
		return err
	}
	switch args[0] {
	case "help":
		if len(args) > 1 {
			if _, ok := commandHelp[args[1]]; ok {
				return run([]string{args[1], "--help"}, stdin, stdout, env, interactive)
			}
			if args[1] != "version" && args[1] != "help" {
				return exit.Usagef("unknown command %q; run `livery help`", args[1])
			}
		}
		_, err := io.WriteString(stdout, usage)
		return err
	case "-h", "--help":
		_, err := io.WriteString(stdout, usage)
		return err
	case "version", "-V", "--version":
		_, err := fmt.Fprintf(stdout, "livery %s\n", version())
		return err
	case "update":
		return update(args[1:], stdout, env)
	case "uninstall":
		return uninstall(args[1:], stdout, env)
	case "list":
		return list(args[1:], stdout, env)
	case "check":
		return check(args[1:], stdout, env)
	case "doctor":
		return doctor(args[1:], stdout, env)
	case "setup":
		return setup(args[1:], stdin, stdout, env, interactive)
	case "import":
		return importCmd(args[1:], stdin, stdout, env, interactive)
	default:
		return exit.Usagef("unknown command %q; run `livery help`", args[0])
	}
}
