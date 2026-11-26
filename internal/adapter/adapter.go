// Package adapter is the contract between livery's core and one harness: the core computes
// what to install, an adapter says where it goes.
package adapter

import (
	"encoding/json"

	"github.com/Pnakoryn/livery/internal/content"
)

// Env is the machine, and the scope livery works in, as an adapter sees it.
type Env struct {
	Home   string
	Getenv func(string) string
	// Project is the project root, absolute and cleaned; empty means machine scope.
	Project string
	// Dir is the working directory.
	Dir string
}

// File is one file an adapter wants on disk. Path is absolute.
type File struct {
	Path string
	Data []byte
	Exec bool
	// Origin is "<layer>: <origin>", for messages.
	Origin string
}

// Entry is one named value under a key of a JSON file the harness also writes, or, with List,
// one string element of an array in it.
//
// Key is a path of object keys joined by ".": "mcpServers" is a top-level key, "mcp.servers"
// the key "servers" inside the object under "mcp". A JSON key that itself contains "." cannot
// be addressed.
type Entry struct {
	Path, Key, Name string
	// Value is the entry's JSON; unused when List is set.
	Value json.RawMessage
	// List is true when Key is the path of an array of strings and Name is an element of it.
	List   bool
	Origin string
}

// EntryKeys says that in the object at Key of the shared file Path, livery owns only Keys of
// each entry: a key outside Keys (one the harness adds, for example) never makes an entry edited
// and is kept when livery updates the entry.
type EntryKeys struct {
	Path, Key string
	Keys      []string
}

// Plan is everything one adapter wants. Root is the harness folder (for the empty-directory
// cleanup).
type Plan struct {
	Root    string
	Files   []File
	Entries []Entry
	// EntryKeys are the keys livery owns in the entries of each shared object (see EntryKeys).
	EntryKeys []EntryKeys
	// Notes are lines the report prints after the changes; they never change the exit code.
	Notes []string
}

// Adapter places content for one harness.
type Adapter interface {
	// Name is the harness name, equal to its package directory.
	Name() string
	// Root is the harness's config folder, absolute.
	Root(env Env) string
	// Render turns items into the files and JSON entries the harness needs.
	Render(items []content.Item, env Env) (Plan, error)
	// EntryKeys lists, for each shared object Render writes entries to, every key an entry
	// rendered there can have.
	EntryKeys(env Env) []EntryKeys
}

// Found is one item read from a harness folder.
type Found struct {
	// Item has Layer empty; its Origin is the path relative to the harness root.
	Item content.Item
	// Paths are the absolute files the item was read from.
	Paths []string
	// Path, Key and Name are set for an item read from a shared JSON file (an MCP server):
	// the same reference as Entry.
	Path, Key, Name string
}

// Skipped is something the import saw and did not take, with the reason. With Partial the item
// was imported and only something of it was left out (a note, not a skip).
type Skipped struct {
	Path, Reason string
	Partial      bool
}

// Importer is implemented by adapters that can read their harness folder back. It only reads.
type Importer interface {
	Import(env Env) (found []Found, skipped []Skipped, err error)
}
