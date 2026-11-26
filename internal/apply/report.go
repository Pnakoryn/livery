package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/lock"
	"github.com/Pnakoryn/livery/internal/source"
)

// Report writes one line per action that changes or blocks something, or "up to date" when
// there is none. Paths are shown as "~/..." under r.Home, or relative to r.Project in project
// scope. In an applying run (applying set) an edited item has no line here, the caller names it
// after the run, and "up to date" is not written when there is one.
func Report(w io.Writer, r Result, applying bool) {
	scope := lock.Scope{Home: r.Home, Project: r.Project}
	n := 0
	for _, a := range r.Actions {
		name := Target(scope.Show(a.Path), a)
		var line string
		switch a.Op {
		case Write:
			line = "write   " + name
		case Remove:
			line = "remove  " + name
		case Adopt:
			line = "adopt   " + name
		case SetEntry:
			line = "set     " + name
		case UnsetEntry:
			line = "unset   " + name
		case KeptYours:
			line = string(a.Op) + ": " + name + " (the team's version is not installed; to install it, rename or delete yours, then run " + r.UpdateCmd() + ")"
		case Edited:
			n++
			if applying {
				continue
			}
			line = string(a.Op) + ": " + name
		case LeftEdited:
			what := "file"
			if a.Key != "" && !a.List {
				what = "entry"
			}
			if r.Command == "uninstall" {
				line = string(a.Op) + ": " + name + " (you changed it, so it stays; delete it to remove it)"
			} else {
				line = string(a.Op) + ": " + name + " (no longer in the release; the " + what + " is yours now, delete it to remove it)"
			}
		default:
			continue
		}
		_, _ = fmt.Fprintln(w, line)
		n++
	}
	if n == 0 {
		_, _ = fmt.Fprintln(w, "up to date")
	}
}

// Target names a in messages: the path, followed by "<key>.<name>" for an entry whose name has
// only letters, digits, "_" and "-", "<key>["<name>"]" for any other entry name (so a name
// with a dot does not read as a path of keys), or "<key>[<element>]" for a list element.
func Target(path string, a Action) string {
	if a.List {
		return path + " " + a.Key + "[" + a.Name + "]"
	}
	if a.Key != "" || a.Name != "" {
		if plainName(a.Name) {
			return path + " " + a.Key + "." + a.Name
		}
		return path + " " + a.Key + "[" + strconv.Quote(a.Name) + "]"
	}
	return path
}

// plainName reports whether name is not empty and has only [A-Za-z0-9_-].
func plainName(name string) bool {
	if name == "" {
		return false
	}
	return strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == ""
}

// Diff writes a unified diff, from git diff --no-index, of every Write, Edited and SetEntry:
// "now/" is what is on disk, "livery/" what livery wants. A Remove or UnsetEntry has no diff:
// its Report line says what goes. Paths are shown as Report shows them.
func Diff(w io.Writer, r Result) error {
	scope := lock.Scope{Home: r.Home, Project: r.Project}
	git, err := exec.LookPath("git")
	if err != nil {
		return exit.Environmentf("git: not found on PATH (%v); install git, then run livery again", err)
	}
	tmp, err := os.MkdirTemp("", "livery-diff-*")
	if err != nil {
		return exit.Environmentf("cannot create a temporary directory: %v; check TMPDIR, then run livery again", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	for _, a := range r.Actions {
		switch a.Op {
		case Write, Edited, SetEntry:
		default:
			continue
		}
		oldData, newData := a.Old, a.New
		if a.Key != "" || a.Name != "" {
			if a.Key != "" && !a.List {
				oldData = maskValues(oldData, newData)
				oldData, newData = indentBoth(oldData, newData)
			}
			oldData, newData = withNewline(oldData), withNewline(newData)
		}
		if err := os.WriteFile(filepath.Join(tmp, "a"), oldData, 0o600); err != nil {
			return exit.Environmentf("%s: cannot write: %v; check TMPDIR, then run livery again", tmp, err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "b"), newData, 0o600); err != nil {
			return exit.Environmentf("%s: cannot write: %v; check TMPDIR, then run livery again", tmp, err)
		}
		cmd := exec.Command(git, "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=now/", "--dst-prefix=livery/", "a", "b")
		cmd.Dir = tmp
		cmd.Env = source.GitEnv()
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) || ee.ExitCode() != 1 {
				return exit.Environmentf("git diff: %v: %s; check the git installation, then run livery again", err, strings.TrimSpace(stderr.String()))
			}
		}
		name := Target(scope.Show(a.Path), a)
		if a.Op == Write && a.Key == "" && a.Old != nil && out.Len() == 0 {
			state := "not executable"
			if a.Exec {
				state = "executable"
			}
			if _, err := fmt.Fprintf(w, "mode    %s: %s\n", name, state); err != nil {
				return err
			}
			continue
		}
		if _, err := io.WriteString(w, renameHeaders(out.String(), name)); err != nil {
			return err
		}
	}
	return nil
}

// maskValues returns the entry JSON old with every string value one level down in an
// object-valued key replaced by "***", unless wanted has the same string at the same place. Keys,
// their order and the formatting stay. Anything that is not a JSON object is returned unchanged.
func maskValues(old, wanted []byte) []byte {
	var want map[string]any
	_ = json.Unmarshal(wanted, &want)
	dec := json.NewDecoder(bytes.NewReader(old))
	type frame struct {
		obj    bool
		key    string
		isKey  bool
		parent string
	}
	var stack []frame
	var out []byte
	last := 0
	prev := int64(0)
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF && len(stack) == 0 {
				break
			}
			return old
		}
		start := prev
		end := dec.InputOffset()
		prev = end
		if len(stack) == 0 && start == 0 {
			if d, ok := tok.(json.Delim); !ok || d != '{' {
				return old
			}
		}
		top := len(stack) - 1
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				f := frame{obj: d == '{', isKey: true}
				if top >= 0 && stack[top].obj {
					f.parent = stack[top].key
				}
				stack = append(stack, f)
			default:
				stack = stack[:top]
				if top-1 >= 0 && stack[top-1].obj {
					stack[top-1].isKey = true
				}
			}
			continue
		}
		if top >= 0 && stack[top].obj && stack[top].isKey {
			stack[top].key, _ = tok.(string)
			stack[top].isKey = false
			continue
		}
		if s, ok := tok.(string); ok && top == 1 && stack[0].obj && stack[1].obj {
			if sub, _ := want[stack[1].parent].(map[string]any); sub == nil || sub[stack[1].key] != s {
				b := old[start:end]
				i := 0
				for i < len(b) && strings.IndexByte(" \t\r\n:,", b[i]) >= 0 {
					i++
				}
				out = append(out, old[last:int(start)+i]...)
				out = append(out, `"***"`...)
				last = int(end)
			}
		}
		if top >= 0 && stack[top].obj {
			stack[top].isKey = true
		}
	}
	return append(out, old[last:]...)
}

// indentBoth re-indents the entries old and wanted, two spaces per level, when both are valid
// JSON, so a changed value shows as one changed line; otherwise it returns them unchanged.
func indentBoth(old, wanted []byte) ([]byte, []byte) {
	var a, b bytes.Buffer
	if json.Indent(&a, bytes.TrimSpace(old), "", "  ") != nil || json.Indent(&b, bytes.TrimSpace(wanted), "", "  ") != nil {
		return old, wanted
	}
	return a.Bytes(), b.Bytes()
}

func withNewline(b []byte) []byte {
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return b
	}
	return append(append([]byte(nil), b...), '\n')
}

// renameHeaders replaces the temporary names in the header lines of a git diff (everything
// before the first hunk) with name, and drops git's "diff --git", "index" and "new file mode" /
// "deleted file mode" lines.
func renameHeaders(diff, name string) string {
	lines := strings.SplitAfter(diff, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			break
		}
		switch {
		case strings.HasPrefix(l, "diff --git "), strings.HasPrefix(l, "index "),
			strings.HasPrefix(l, "new file mode "), strings.HasPrefix(l, "deleted file mode "):
			lines[i] = ""
		case strings.HasPrefix(l, "--- now/a"):
			lines[i] = "--- now/" + name + strings.TrimPrefix(l, "--- now/a")
		case strings.HasPrefix(l, "+++ livery/b"):
			lines[i] = "+++ livery/" + name + strings.TrimPrefix(l, "+++ livery/b")
		case l == "Binary files now/a and livery/b differ\n":
			lines[i] = "Binary files now/" + name + " and livery/" + name + " differ\n"
		}
	}
	return strings.Join(lines, "")
}
