// Package lock reads and writes the lock file: the record of what livery installed and owns.
package lock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

// Merged records the entries livery owns under one key of a shared JSON file. Key is a path of
// object keys joined by "." ("mcp.servers"); a key without "." is a top-level key.
type Merged struct {
	Path string `json:"path"`
	Key  string `json:"key"`
	// List is true when Key holds an array of strings and Entries are elements of it.
	List bool `json:"list,omitempty"`
	// Entries maps an entry name, or a list element, to its checksum.
	Entries map[string]string `json:"entries"`
}

// Schema is the lock format this livery reads and writes.
const Schema = 2

// ErrNewer is wrapped by the error Read returns for a lock with a higher schema than this
// livery reads: errors.Is(err, ErrNewer) tells that case apart without matching text.
var ErrNewer = errors.New("lock written by a newer livery")

// Lock is the lock file. Paths under home are stored as "~/...".
type Lock struct {
	Schema    int               `json:"schema"`
	Livery    string            `json:"livery"`
	Source    string            `json:"source"`
	Channel   string            `json:"channel"`
	Tag       string            `json:"tag,omitempty"`
	Commit    string            `json:"commit"`
	Teams     []string          `json:"teams"`
	Harnesses []string          `json:"harnesses"`
	Files     map[string]string `json:"files"`
	Merged    []Merged          `json:"merged,omitempty"`
}

// ReadIn reads the lock at path and checks it against s. A missing file is the first-install
// state: found is false and err is nil.
func ReadIn(path string, s Scope) (l Lock, found bool, err error) {
	l, found, err = read(path, s)
	if err != nil || !found {
		return l, found, err
	}
	if err := s.Check(path, l); err != nil {
		return Lock{}, false, err
	}
	return l, true, nil
}

// read reads the lock at path; s only decides the flag in the message for a lock of an older livery.
func read(path string, s Scope) (l Lock, found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Lock{}, false, nil
	}
	if err != nil {
		return Lock{}, false, exit.Environmentf("%s: cannot read the lock: %v; fix its permissions or remove it, then run livery doctor", path, err)
	}
	// Decode schema leniently first, so a newer lock with unknown fields still asks for an upgrade.
	var head struct {
		Schema int `json:"schema"`
	}
	if json.Unmarshal(data, &head) == nil && head.Schema > Schema {
		e := exit.Findingf("%s: written by a newer livery (lock schema %d, this livery reads up to %d); upgrade livery", path, head.Schema, Schema)
		e.Cause = ErrNewer
		return Lock{}, false, e
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) == nil && obj != nil && json.Unmarshal(data, &head) == nil && head.Schema < Schema {
		flag := ""
		if s.Project != "" {
			flag = " --project"
		}
		return Lock{}, false, exit.Findingf("%s: written by an older livery that this version cannot read; remove it, then run livery setup%s", path, flag)
	}
	if err := decodeStrict(data, &l); err != nil {
		return Lock{}, false, notLock(path, err)
	}
	l.Schema = Schema
	if l.Livery == "" || l.Source == "" || l.Commit == "" {
		return Lock{}, false, notLock(path, errors.New("livery, source or commit is missing"))
	}
	return l, true, nil
}

// decodeStrict decodes data, one JSON object without unknown fields, into v.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("data after the JSON object")
	}
	return nil
}

func notLock(path string, err error) error {
	return exit.Findingf("%s: not a livery lock (%v); remove or move the file, then run livery setup to write a new one", path, err)
}

// Write writes l to path atomically as JSON indented with two spaces and a trailing newline.
// It creates the directory (0755); the file is 0644. Merged is sorted by Path then Key.
// Teams and Harnesses are written as [] when empty, so the file does not depend on whether
// the caller held a nil or an empty list.
func (l Lock) Write(path string) error {
	data, err := l.encode()
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

// SameAsFile reports whether the lock at path already holds l, apart from the livery version.
// A missing or unreadable lock is not the same.
func (l Lock) SameAsFile(path string) bool {
	old, found, err := read(path, Scope{})
	if err != nil || !found {
		return false
	}
	l.Livery = old.Livery
	want, err := l.encode()
	if err != nil {
		return false
	}
	have, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Equal(want, have)
}

// SortMerged sorts allMerged in place by Path, then Key, then a plain key before a list.
func SortMerged(allMerged []Merged) {
	sort.Slice(allMerged, func(i, j int) bool {
		a, b := allMerged[i], allMerged[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return !a.List && b.List
	})
}

func (l Lock) encode() ([]byte, error) {
	l.Schema = Schema
	if l.Teams == nil {
		l.Teams = []string{}
	}
	if l.Harnesses == nil {
		l.Harnesses = []string{}
	}
	l.Merged = append([]Merged(nil), l.Merged...)
	SortMerged(l.Merged)
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return exit.Environmentf("%s: cannot create the lock directory: %v", dir, err)
	}
	if err := paths.WriteAtomic(path, data, 0o644, ".lock-*.tmp", nil); err != nil {
		var ce *paths.CreateError
		if errors.As(err, &ce) {
			err = ce.Err
		}
		return exit.Environmentf("%s: cannot write the lock: %v", path, err)
	}
	return nil
}

// Sum is the checksum of data: "sha256:<hex>".
func Sum(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// SumJSON is Sum of the canonical form of v: decoded and re-encoded compact with sorted keys,
// so reformatting the JSON does not change the checksum. Number spelling is not normalised
// (1 and 1.0 differ): a changed spelling counts as an edit, which blocks the run instead of
// overwriting something the user may have meant.
func SumJSON(v json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()
	var x any
	if err := dec.Decode(&x); err != nil {
		return "", err
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", errors.New("data after the JSON value")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(x); err != nil {
		return "", err
	}
	return Sum(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// Tilde returns abs as "~/..." when it is under home, else abs unchanged.
func Tilde(home, abs string) string {
	home = strings.TrimSuffix(filepath.Clean(home), "/")
	if home == "" {
		return abs
	}
	if abs == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(abs, home+"/"); ok {
		return "~/" + rest
	}
	return abs
}

// Expand is the reverse of Tilde: "~" and "~/..." become paths under home.
func Expand(home, p string) string {
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// Scope turns the absolute paths livery writes into the paths its lock stores, and back. With
// Project empty it is the machine scope: paths under Home are stored as "~/..." (Tilde). With
// Project set (an absolute, cleaned folder) every path is stored relative to it with "/"
// separators, and a path outside it is refused.
type Scope struct {
	Home    string
	Project string
}

// ScopeOf is the scope env stands for.
func ScopeOf(env adapter.Env) Scope { return Scope{Home: env.Home, Project: env.Project} }

// Store returns the lock path of abs. In project scope a path that is not strictly inside the
// project is a Finding.
func (s Scope) Store(abs string) (string, error) {
	if s.Project == "" {
		return Tilde(s.Home, abs), nil
	}
	rel, ok := s.rel(abs)
	if !ok {
		return "", exit.Findingf("%s: refusing to write outside the project %s; remove the item from the shared repository or run livery without --project", abs, s.Project)
	}
	return rel, nil
}

// Load is the reverse of Store: the absolute, cleaned path of the lock path p. Project lock
// paths must have passed Check.
func (s Scope) Load(p string) string {
	if s.Project == "" {
		return filepath.Clean(Expand(s.Home, p))
	}
	return filepath.Join(s.Project, filepath.FromSlash(p))
}

// Show is abs as messages print it: "~/..." in machine scope, relative to the project in
// project scope (abs unchanged when it is outside).
func (s Scope) Show(abs string) string {
	if s.Project == "" {
		return Tilde(s.Home, abs)
	}
	if rel, ok := s.rel(abs); ok {
		return rel
	}
	return abs
}

func (s Scope) rel(abs string) (string, bool) {
	rel, err := filepath.Rel(s.Project, filepath.Clean(abs))
	if err != nil || !filepath.IsAbs(abs) || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// validRel reports whether p is a project lock path: relative, "/"-separated, clean, without
// "~" or "..".
func validRel(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || strings.Contains(p, `\`) {
		return false
	}
	for _, e := range strings.Split(p, "/") {
		if e == "" || e == "." || e == ".." {
			return false
		}
	}
	return true
}

// Check returns the not-a-livery-lock Finding naming file when l, read from file, holds a path
// that is not a project lock path; in machine scope it accepts every lock.
func (s Scope) Check(file string, l Lock) error {
	if s.Project == "" {
		return nil
	}
	for p := range l.Files {
		if err := checkRel(p); err != nil {
			return notLock(file, err)
		}
	}
	for _, m := range l.Merged {
		if err := checkRel(m.Path); err != nil {
			return notLock(file, err)
		}
	}
	return nil
}

// reserved are the project folders livery never installs into: git's own (hooks there run code)
// and livery's config and lock.
var reserved = []string{".git", ".livery"}

// checkRel returns why p is not a project lock path, or nil.
func checkRel(p string) error {
	if !validRel(p) {
		return fmt.Errorf("path %q is not relative to the project", p)
	}
	first, _, _ := strings.Cut(p, "/")
	for _, r := range reserved {
		if strings.EqualFold(first, r) { // macOS folders are case-insensitive by default
			return fmt.Errorf("path %q is inside %s, where livery never installs", p, r)
		}
	}
	return nil
}
