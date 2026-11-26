// Package config is livery's per-scope config file: source, channel, teams, harnesses.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
	"github.com/Pnakoryn/livery/internal/source"
)

// Config holds the choices livery keeps per machine or project.
type Config struct {
	Source    string   `json:"source"`
	Channel   string   `json:"channel"`
	Teams     []string `json:"teams"`
	Harnesses []string `json:"harnesses"`
}

// ValidTeam reports whether s is a valid team name: lowercase letters, digits and '-',
// starting with a letter or digit.
func ValidTeam(s string) bool { return content.ValidName(s) }

var fields = []string{"source", "channel", "teams", "harnesses"}

// Load reads the config at path. A missing file is found == false, not an error. setup is the
// setup command the messages name ("livery setup", or with --project in a project).
func Load(path, setup string) (c Config, found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, exit.Environmentf("cannot read %s: %v; check the file's permissions", path, err)
	}
	_, msg := checkKeys(data)
	if msg != "" {
		return Config{}, false, exit.Usagef("%s is not a valid livery config: %s; fix the file or run `%s`", path, msg, setup)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, false, exit.Usagef("%s is not a valid livery config: %v; fix the file or run `%s`", path, err, setup)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Config{}, false, exit.Usagef("%s is not a valid livery config: unexpected data after the JSON object; fix the file or run `%s`", path, setup)
	}
	return c, true, nil
}

// checkKeys returns the top-level keys and reports keys that differ in case from the field
// names and duplicate keys; encoding/json accepts both. "" means none. Other syntax errors are
// left to the real decode.
func checkKeys(data []byte) (map[string]bool, string) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, ""
	}
	seen := map[string]bool{}
	msg := ""
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return seen, msg
		}
		if msg == "" {
			for _, want := range fields {
				if key != want && strings.EqualFold(key, want) {
					msg = fmt.Sprintf("key %q must be written %q", key, want)
				}
			}
			if seen[key] {
				msg = fmt.Sprintf("key %q appears twice", key)
			}
		}
		seen[key] = true
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return seen, msg
		}
	}
	return seen, msg
}

// Save writes the config atomically as indented JSON, creating the directory.
func (c Config) Save(path string) error {
	data, err := c.JSON()
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return exit.Environmentf("cannot create %s: %v; check that the directory is writable, then run the command again", dir, err)
	}
	if err := paths.WriteAtomic(path, data, 0o644, ".config-*.tmp", nil); err != nil {
		var ce *paths.CreateError
		if errors.As(err, &ce) {
			return exit.Environmentf("cannot write in %s: %v; check that the directory is writable, then run the command again", dir, ce.Err)
		}
		return exit.Environmentf("cannot write %s: %v; check the disk space and permissions, then run the command again", path, err)
	}
	return nil
}

// JSON returns the config as Save writes it (without the final newline): empty lists as [].
func (c Config) JSON() ([]byte, error) {
	if c.Teams == nil {
		c.Teams = []string{}
	}
	if c.Harnesses == nil {
		c.Harnesses = []string{}
	}
	return json.MarshalIndent(c, "", "  ")
}

// Validate checks the config; harnesses are the valid harness names.
func (c Config) Validate(path string, harnesses []string, setup string) error {
	if c.Source == "" {
		return exit.Usagef("%s: key \"source\" is empty; set it to the git URL of your shared repository, or run `%s`", path, setup)
	}
	if source.HasCredentials(c.Source) {
		return source.CredentialsError(path+": key \"source\": ", setup)
	}
	if !source.ValidChannel(c.Channel) {
		return exit.Usagef("%s: key \"channel\" is %q; use stable, next, or a release tag vX.Y.Z", path, c.Channel)
	}
	seen := map[string]bool{}
	for _, t := range c.Teams {
		if !ValidTeam(t) {
			return exit.Usagef("%s: key \"teams\" has invalid team name %q; use lowercase letters, digits and '-', starting with a letter or digit", path, t)
		}
		if seen[t] {
			return exit.Usagef("%s: key \"teams\" lists %q twice; remove one", path, t)
		}
		seen[t] = true
	}
	if len(c.Harnesses) == 0 {
		return exit.Usagef("%s: key \"harnesses\" is empty; list at least one of: %s", path, strings.Join(harnesses, ", "))
	}
	valid := map[string]bool{}
	for _, h := range harnesses {
		valid[h] = true
	}
	seen = map[string]bool{}
	for _, h := range c.Harnesses {
		if !valid[h] {
			return exit.Usagef("%s: key \"harnesses\" has unknown harness %q; valid: %s", path, h, strings.Join(harnesses, ", "))
		}
		if seen[h] {
			return exit.Usagef("%s: key \"harnesses\" lists %q twice; remove one", path, h)
		}
		seen[h] = true
	}
	return nil
}
