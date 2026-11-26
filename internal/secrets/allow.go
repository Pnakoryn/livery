package secrets

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
)

// Allow is a list of accepted findings: one "path fingerprint" pair per line. The zero Allow
// accepts nothing.
type Allow struct {
	used    map[allowEntry]bool
	entries []allowEntry
}

type allowEntry struct {
	path, fingerprint string
}

var fingerprintForm = regexp.MustCompile(`^[0-9a-f]{12}$`)

// ParseAllow reads an allow file; name is the file named in errors. Empty lines and lines
// starting with # are ignored. Anything else that is not a "path fingerprint" pair is an exit 2
// error naming the file and line.
func ParseAllow(name string, data []byte) (Allow, error) {
	a := Allow{used: map[allowEntry]bool{}}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), len(data)+1)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return Allow{}, exit.Usagef("%s:%d: want \"path fingerprint\" separated by a space, got %d fields: fix or remove the line, then run the command again", name, n, len(fields))
		}
		if !fingerprintForm.MatchString(fields[1]) {
			return Allow{}, exit.Usagef("%s:%d: the fingerprint must be 12 lowercase hex characters as livery reports it: fix or remove the line, then run the command again", name, n)
		}
		e := allowEntry{path: fields[0], fingerprint: fields[1]}
		if _, dup := a.used[e]; dup {
			continue
		}
		a.used[e] = false
		a.entries = append(a.entries, e)
	}
	if err := sc.Err(); err != nil {
		return Allow{}, exit.Usagef("%s: %v: fix the file, then run the command again", name, err)
	}
	return a, nil
}

// Allows reports whether f is listed, and marks the pair used. The zero Allow lists nothing.
func (a Allow) Allows(f Finding) bool {
	e := allowEntry{path: f.Path, fingerprint: f.Fingerprint}
	if _, ok := a.used[e]; !ok {
		return false
	}
	a.used[e] = true
	return true
}

// Unused lists the pairs Allows was never asked for, as "path fingerprint", in file order.
func (a Allow) Unused() []string {
	var out []string
	for _, e := range a.entries {
		if !a.used[e] {
			out = append(out, e.path+" "+e.fingerprint)
		}
	}
	return out
}
