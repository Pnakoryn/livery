package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// NameRule is the rule for the name of a skill, instruction, team or native item, as shown in
// messages.
const NameRule = "^[a-z0-9][a-z0-9-]*$"

var nameRe = regexp.MustCompile(NameRule)

// ValidName says whether s is a valid item or team name.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// MCPKeys returns the keys of an MCP server in the shared repository, in the order they are
// written. The caller may change the result.
func MCPKeys() []string { return []string{"command", "args", "env", "url", "headers"} }

// IsMCPKey says whether k is one of MCPKeys.
func IsMCPKey(k string) bool { return slices.Contains(MCPKeys(), k) }

// ItemLess is the order of SortItems.
func ItemLess(a, b Item) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Harness != b.Harness {
		return a.Harness < b.Harness
	}
	return a.Name < b.Name
}

// SortItems sorts items by kind, harness and name; equal items keep their order.
func SortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool { return ItemLess(items[i], items[j]) })
}

// JSONString is s as a JSON string, without escaping <, > and &.
func JSONString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(buf.String(), "\n")
}

// OrderedObject splits a JSON object into its keys (in document order) and raw values.
func OrderedObject(raw []byte) ([]string, []json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, fmt.Errorf("not a JSON object")
	}
	var keys []string
	var vals []json.RawMessage
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		keys = append(keys, kt.(string))
		vals = append(vals, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, nil, err
	}
	return keys, vals, nil
}
