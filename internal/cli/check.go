package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/engine"
	"github.com/Pnakoryn/livery/internal/exit"
)

// check implements `livery check [DIR]`: validate a shared repository's working tree and scan
// it for secrets. It reads only and exits 1 when a line is a finding.
func check(args []string, stdout io.Writer, env adapter.Env) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return flagError("check", fs, err)
	}
	if fs.NArg() > 1 {
		return exit.Usagef("check: unexpected argument %q; run livery check --help", fs.Arg(1))
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = expandHome(env, fs.Arg(0))
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return exit.Usagef("check: %s is not a folder; pass the repository folder: livery check DIR", dir)
	}
	r := &report{w: stdout}
	for _, l := range engine.Check(env, dir) {
		r.line(l.Status, l.Check, l.Detail, l.Fix)
	}
	return r.finish()
}

// report prints the status lines of check and doctor and counts the findings among them.
type report struct {
	w        io.Writer
	findings int
}

// line prints "<status> <check>: <detail>", with " -> <fix>" when fix is set.
func (r *report) line(status, check, detail, fix string) {
	s := fmt.Sprintf("%-8s %s: %s", status, check, detail)
	if fix != "" {
		s += " -> " + fix
	}
	_, _ = fmt.Fprintln(r.w, s)
	if status == "finding" {
		r.findings++
	}
}

// finish returns a Finding when a line was one.
func (r *report) finish() error {
	if r.findings > 0 {
		return exit.Findingf("%d finding(s); each line above says how to fix it", r.findings)
	}
	return nil
}
