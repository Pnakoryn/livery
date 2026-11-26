// Package apply writes what livery installs: files it owns and entries in shared JSON files.
package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

// member is one key and its value, exactly as read.
type member struct {
	key   string
	raw   []byte // the key as written in the file, quotes included; nil for a new key
	value json.RawMessage
}

// Doc is a JSON object with its top-level key order kept.
type Doc struct {
	path    string
	members []member
	// noNewline is true when the parsed input did not end in a newline; Bytes keeps that.
	noNewline bool
}

// ParseDoc parses data as a JSON object. Empty or whitespace-only data is an empty object.
// path is only used in messages.
func ParseDoc(path string, data []byte) (*Doc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Doc{path: path}, nil
	}
	members, err := parseObject(data)
	if err != nil {
		if errors.Is(err, errNotObject) {
			return nil, exit.Findingf("%s: not a JSON object; fix or remove the file, then run livery again", path)
		}
		return nil, exit.Findingf("%s: not valid JSON (%v); fix the file, then run livery again", path, err)
	}
	return &Doc{path: path, members: members, noNewline: !bytes.HasSuffix(data, []byte("\n"))}, nil
}

var errNotObject = errors.New("not an object")

// parseObject reads one JSON object and keeps its members in order with their raw values.
func parseObject(data []byte) ([]member, error) {
	if !json.Valid(data) {
		// json.Valid also rejects trailing data and comments; get a message from Unmarshal.
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return nil, errors.New("invalid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errNotObject
	}
	members := []member{}
	for dec.More() {
		start := int(dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("object key is not a string")
		}
		end := int(dec.InputOffset())
		rawKey := bytes.TrimLeft(data[start:end], " \t\r\n,")
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		members = append(members, member{key: key, raw: append([]byte(nil), rawKey...), value: raw})
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data after the JSON object")
	}
	return members, nil
}

// find returns the index of key in members, the last one if it repeats (as encoding/json
// reads it), or -1.
func find(members []member, key string) int {
	for i := len(members) - 1; i >= 0; i-- {
		if members[i].key == key {
			return i
		}
	}
	return -1
}

// compact writes members as a JSON object without added whitespace.
func compact(members []member) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		if m.raw != nil {
			b.Write(m.raw)
		} else {
			b.WriteString(content.JSONString(m.key))
		}
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// Key paths: every key argument of Doc is a path of object keys joined by ".": "mcpServers" is
// a top-level key, "mcp.servers" the key "servers" in the object under "mcp". A JSON key that
// itself contains "." cannot be addressed.

// notObject is the Finding for the path element elems[:n] that is not a JSON object. It names
// the element and, when the key goes deeper, the full key.
func (d *Doc) notObject(key string, elems []string, n int) error {
	at := strings.Join(elems[:n], ".")
	if at == key {
		return exit.Findingf("%s: %q is not a JSON object; fix the file, then run livery again", d.path, key)
	}
	return exit.Findingf("%s: %q (on the way to %q) is not a JSON object; fix the file, then run livery again", d.path, at, key)
}

// at returns the value at key; found is false when an element of the path is missing. An
// element above the last one that is not an object is a Finding.
func (d *Doc) at(key string) (value json.RawMessage, found bool, err error) {
	elems := strings.Split(key, ".")
	members := d.members
	for i, k := range elems {
		j := find(members, k)
		if j < 0 {
			return nil, false, nil
		}
		if i == len(elems)-1 {
			return members[j].value, true, nil
		}
		members, err = parseObject(members[j].value)
		if err != nil {
			return nil, true, d.notObject(key, elems, i+1)
		}
	}
	return nil, false, nil // not reached: elems is never empty
}

// putAt stores value at key. A missing element of the path is created as an object appended at
// the end of its parent; existing keys keep their place and their spelling.
func (d *Doc) putAt(key string, value json.RawMessage) error {
	elems := strings.Split(key, ".")
	members, err := d.putIn(d.members, key, elems, 0, value)
	if err != nil {
		return err
	}
	d.members = members
	return nil
}

func (d *Doc) putIn(members []member, key string, elems []string, i int, value json.RawMessage) ([]member, error) {
	j := find(members, elems[i])
	if i < len(elems)-1 {
		var sub []member
		if j >= 0 {
			var err error
			if sub, err = parseObject(members[j].value); err != nil {
				return nil, d.notObject(key, elems, i+1)
			}
		}
		sub, err := d.putIn(sub, key, elems, i+1, value)
		if err != nil {
			return nil, err
		}
		value = compact(sub)
	}
	if j >= 0 {
		members[j].value = value
		return members, nil
	}
	return append(members, member{key: elems[i], value: value}), nil
}

// sub returns the members of the object at key; found is false when key is missing.
func (d *Doc) sub(key string) (members []member, found bool, err error) {
	v, found, err := d.at(key)
	if err != nil || !found {
		return nil, found, err
	}
	members, err = parseObject(v)
	if err != nil {
		elems := strings.Split(key, ".")
		return nil, true, d.notObject(key, elems, len(elems))
	}
	return members, true, nil
}

// Entries returns the object at key; a missing key gives an empty map.
func (d *Doc) Entries(key string) (map[string]json.RawMessage, error) {
	members, _, err := d.sub(key)
	if err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(members))
	for _, m := range members {
		out[m.key] = m.value
	}
	return out, nil
}

// Set sets entry name at key to value. An existing entry is replaced in place, a new one is
// appended, and a missing key (or object on its path) is added at the end of its parent.
func (d *Doc) Set(key, name string, value json.RawMessage) error {
	if !json.Valid(value) {
		return exit.Findingf("%s: the value for %q in %q is not valid JSON", d.path, name, key)
	}
	members, _, err := d.sub(key)
	if err != nil {
		return err
	}
	v := append(json.RawMessage(nil), value...)
	if i := find(members, name); i >= 0 {
		members[i].value = v
	} else {
		members = append(members, member{key: name, value: v})
	}
	return d.putAt(key, compact(members))
}

// Remove removes entry name at key. A missing name or key is no error; removing the last
// entry leaves an empty object at key.
func (d *Doc) Remove(key, name string) error {
	members, found, err := d.sub(key)
	if err != nil || !found {
		return err
	}
	kept := members[:0]
	removed := false
	for _, m := range members {
		if m.key == name {
			removed = true
			continue
		}
		kept = append(kept, m)
	}
	if removed {
		return d.putAt(key, compact(kept))
	}
	return nil
}

// list returns the elements of the array of strings at key, raw and decoded; found is false
// when key is missing. Any other value is a Finding naming key.
func (d *Doc) list(key string) (raw []json.RawMessage, items []string, found bool, err error) {
	v, found, err := d.at(key)
	if err != nil || !found {
		return nil, nil, found, err
	}
	notList := exit.Findingf("%s: %q is not a JSON array of strings; fix the file, then run livery again", d.path, key)
	if t := bytes.TrimSpace(v); len(t) == 0 || t[0] != '[' {
		return nil, nil, true, notList
	}
	if err := json.Unmarshal(v, &raw); err != nil {
		return nil, nil, true, notList
	}
	items = make([]string, len(raw))
	for i, r := range raw {
		if t := bytes.TrimSpace(r); len(t) == 0 || t[0] != '"' {
			return nil, nil, true, notList
		}
		if err := json.Unmarshal(r, &items[i]); err != nil {
			return nil, nil, true, notList
		}
	}
	return raw, items, true, nil
}

// compactList writes raw as a JSON array.
func compactList(raw []json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, r := range raw {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(r)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// Items returns the array of strings at key; a missing key is an empty list.
func (d *Doc) Items(key string) ([]string, error) {
	_, items, _, err := d.list(key)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []string{}
	}
	return items, nil
}

// AddItem appends s to the array of strings at key when it is not in it, creating the array
// and the objects on the way.
func (d *Doc) AddItem(key, s string) error {
	raw, items, _, err := d.list(key)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it == s {
			return nil
		}
	}
	raw = append(raw, []byte(content.JSONString(s)))
	return d.putAt(key, compactList(raw))
}

// RemoveItem removes s from the array of strings at key. A missing key or element is no error;
// removing the last element leaves [] at key.
func (d *Doc) RemoveItem(key, s string) error {
	raw, items, found, err := d.list(key)
	if err != nil || !found {
		return err
	}
	kept := raw[:0]
	removed := false
	for i, it := range items {
		if it == s {
			removed = true
			continue
		}
		kept = append(kept, raw[i])
	}
	if removed {
		return d.putAt(key, compactList(kept))
	}
	return nil
}

// hasItem reports whether s is in the array of strings at key.
func (d *Doc) hasItem(key, s string) (bool, error) {
	items, err := d.Items(key)
	if err != nil {
		return false, err
	}
	for _, it := range items {
		if it == s {
			return true, nil
		}
	}
	return false, nil
}

// onlyEmpty reports whether d holds nothing but empty values along keys: every top-level key
// of d starts a key path in keys, and following those paths every value is an empty object,
// or, at the end of a path whose keys value is true (a list), an empty object or array.
func (d *Doc) onlyEmpty(keys map[string]bool) bool {
	return onlyEmptyIn(d.members, keys)
}

func onlyEmptyIn(members []member, paths map[string]bool) bool {
	for _, m := range members {
		isList, exact := paths[m.key]
		deeper := map[string]bool{}
		prefix := m.key + "."
		for p, l := range paths {
			if strings.HasPrefix(p, prefix) {
				deeper[strings.TrimPrefix(p, prefix)] = l
			}
		}
		if !exact && len(deeper) == 0 {
			return false
		}
		if exact && isList {
			var arr []json.RawMessage
			if t := bytes.TrimSpace(m.value); len(t) > 0 && t[0] == '[' && json.Unmarshal(t, &arr) == nil && len(arr) == 0 {
				continue
			}
		}
		sub, err := parseObject(m.value)
		if err != nil {
			return false
		}
		if len(sub) == 0 {
			continue
		}
		if len(deeper) == 0 || !onlyEmptyIn(sub, deeper) {
			return false
		}
	}
	return true
}

// Bytes returns the document indented with two spaces. It ends in a newline unless the parsed
// input did not (Claude Code writes ~/.claude.json without one).
func (d *Doc) Bytes() []byte {
	var b bytes.Buffer
	if err := json.Indent(&b, compact(d.members), "", "  "); err != nil {
		// Every value was validated when read or set, so this cannot happen.
		panic("apply: invalid JSON in Doc: " + err.Error())
	}
	if !d.noNewline {
		b.WriteByte('\n')
	}
	return b.Bytes()
}
