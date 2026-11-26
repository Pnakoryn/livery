package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
	"github.com/Pnakoryn/livery/internal/exit"
)

// MCPSpec is what differs between the adapters when they read an mcpServers object.
type MCPSpec struct {
	// Origin is the origin of the items found in the file.
	Origin string
	// IsServerKey says which keys of a server are kept; any other key is dropped.
	IsServerKey func(key string) bool
	// Key is called first for every key of a server. handled stops the default treatment;
	// drop adds the key to the dropped keys; a non-empty skip makes the server a skip with that
	// reason (the last one wins).
	Key func(key string, raw json.RawMessage) (handled, drop bool, skip string)
}

// MCPServers reads the mcpServers object of the JSON file and turns each server into a found
// MCP item or a skip. A missing file or one without mcpServers gives nothing; a linked file is
// skipped; a file that is not JSON or an mcpServers that is not an object is an error.
func MCPServers(file string, spec MCPSpec) ([]adapter.Found, []adapter.Skipped, error) {
	var found []adapter.Found
	var skipped []adapter.Skipped
	skip := func(reason string) { skipped = append(skipped, adapter.Skipped{Path: file, Reason: reason}) }

	fi, err := os.Lstat(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", file, err)
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		skip(SymlinkReason)
		return nil, skipped, nil
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, exit.Environmentf("%s: cannot read: %v; fix the permissions and run again", file, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, exit.Findingf("%s: not valid JSON: %v; fix or remove the file and run again", file, err)
	}
	raw, ok := top["mcpServers"]
	if !ok {
		return nil, nil, nil
	}
	names, vals, err := content.OrderedObject(raw)
	if err != nil {
		// raw is already valid JSON, so the only failure is a value that is not an object.
		return nil, nil, exit.Findingf("%s: mcpServers must be a JSON object: not an object; fix the file and run again", file)
	}
	for i, name := range names {
		if !content.ValidMCPName(name) {
			skip("server " + QuoteName(name) + ": name must match " + content.MCPNameRule + "; rename it")
			continue
		}
		keys, fields, err := content.OrderedObject(vals[i])
		if err != nil {
			skip("server " + name + ": not a JSON object")
			continue
		}
		var dropped, keptKeys []string
		kept := map[string]json.RawMessage{}
		reason := ""
		for j, key := range keys {
			if spec.Key != nil {
				handled, drop, s := spec.Key(key, fields[j])
				if s != "" {
					reason = s
				}
				if drop {
					dropped = append(dropped, key)
				}
				if handled {
					continue
				}
			}
			if spec.IsServerKey(key) {
				if _, seen := kept[key]; !seen {
					keptKeys = append(keptKeys, key)
				}
				kept[key] = fields[j]
			} else {
				dropped = append(dropped, key)
			}
		}
		if reason != "" {
			skip("server " + name + ": " + reason)
			continue
		}
		_, hasCmd := kept["command"]
		_, hasURL := kept["url"]
		if hasCmd == hasURL {
			skip("server " + name + ": needs exactly one of command and url")
			continue
		}
		var buf bytes.Buffer
		buf.WriteByte('{')
		for n, key := range keptKeys {
			if n > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(key)
			buf.Write(kb)
			buf.WriteByte(':')
			buf.Write(kept[key])
		}
		buf.WriteByte('}')
		var compact bytes.Buffer
		if err := json.Compact(&compact, buf.Bytes()); err != nil {
			skip("server " + name + ": not valid JSON")
			continue
		}
		if len(dropped) > 0 {
			skipped = append(skipped, adapter.Skipped{Path: file, Reason: DroppedReason(name, dropped), Partial: true})
		}
		found = append(found, adapter.Found{
			Item:  content.Item{Kind: content.MCP, Name: name, Origin: spec.Origin, Server: json.RawMessage(compact.Bytes())},
			Paths: []string{file},
			Path:  file, Key: "mcpServers", Name: name,
		})
	}
	return found, skipped, nil
}
