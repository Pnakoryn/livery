package lock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "lock.json")
	in := Lock{
		Livery: "0.1.0", Source: "https://example.org/team/setup.git", Channel: "stable", Tag: "v1.2.0", Commit: "0123abcd", Teams: []string{"web", "api"},
		Harnesses: []string{"z", "a"},
		Files:     map[string]string{"~/x/y": Sum([]byte("y"))},
		Merged: []Merged{
			{Path: "~/b.json", Key: "servers", Entries: map[string]string{"s": "sha256:1"}},
			{Path: "~/a.json", Key: "z", Entries: map[string]string{"t": "sha256:2"}},
			{Path: "~/a.json", Key: "m", Entries: map[string]string{"u": "sha256:3"}},
		},
	}
	if err := in.Write(path); err != nil {
		t.Fatal(err)
	}
	out, found, err := ReadIn(path, Scope{Home: "/h"})
	if err != nil || !found {
		t.Fatalf("Read: found %v, err %v", found, err)
	}
	want := in
	want.Schema = Schema
	want.Merged = []Merged{in.Merged[2], in.Merged[1], in.Merged[0]}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", out, want)
	}
	if in.Merged[0].Path != "~/b.json" {
		t.Fatal("Write changed the caller's Merged slice")
	}
	data, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(data), "}\n") || !strings.Contains(string(data), "\n  \"livery\": ") {
		t.Fatalf("not two-space indented with a trailing newline:\n%s", data)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, want 0644", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o755 {
		t.Fatalf("dir mode %v, want 0755", di.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}
	// Overwrite works.
	in.Tag = ""
	if err := in.Write(path); err != nil {
		t.Fatal(err)
	}
	out, _, _ = ReadIn(path, Scope{Home: "/h"})
	if out.Tag != "" {
		t.Fatalf("Tag %q after overwrite", out.Tag)
	}
}

func TestReadMissing(t *testing.T) {
	_, found, err := ReadIn(filepath.Join(t.TempDir(), "nope.json"), Scope{Home: "/h"})
	if found || err != nil {
		t.Fatalf("found %v, err %v", found, err)
	}
}

func TestReadNotALock(t *testing.T) {
	for _, data := range []string{`{"schema":2,"files": 1}`, `not json`, `{"schema":2,"bogus": "x"}`, `{} {}`, `null`, `{"schema":2}`, `{"schema":2,"livery":"1","source":"s"}`, `{"schema":2,"livery":"1","sources":[]}`} {
		path := filepath.Join(t.TempDir(), "lock.json")
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		_, found, err := ReadIn(path, Scope{Home: "/h"})
		var e *exit.Error
		if found || !errors.As(err, &e) || e.Code != exit.Finding {
			t.Fatalf("%q: found %v, err %v", data, found, err)
		}
		if !strings.Contains(e.Message, path) || !strings.Contains(e.Message, "not a livery lock") || !strings.Contains(e.Message, "livery setup") {
			t.Fatalf("%q: message %q", data, e.Message)
		}
	}
}

func TestSum(t *testing.T) {
	const want = "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := Sum([]byte("abc")); got != want {
		t.Fatalf("Sum = %s", got)
	}
}

func TestSumJSON(t *testing.T) {
	a, err := SumJSON(json.RawMessage(`{"a":1,"b":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := SumJSON(json.RawMessage("{ \"b\": [1, 2],\n \"a\": 1 }"))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("%s != %s", a, b)
	}
	if want := Sum([]byte(`{"a":1,"b":[1,2]}`)); a != want {
		t.Fatalf("not the sum of the compact sorted form: %s", a)
	}
	c, _ := SumJSON(json.RawMessage(`{"a":1,"b":[2,1]}`))
	if c == a {
		t.Fatal("different values gave the same checksum")
	}
	if _, err := SumJSON(json.RawMessage(`{`)); err == nil {
		t.Fatal("no error for invalid JSON")
	}
}

func TestTildeExpand(t *testing.T) {
	const home = "/home/dev"
	for _, c := range []struct{ abs, tilde string }{
		{"/home/dev/.config/x", "~/.config/x"},
		{"/home/dev", "~"},
		{"/home/devon/x", "/home/devon/x"},
		{"/etc/x", "/etc/x"},
	} {
		if got := Tilde(home, c.abs); got != c.tilde {
			t.Errorf("Tilde(%q) = %q, want %q", c.abs, got, c.tilde)
		}
		if got := Expand(home, c.tilde); got != c.abs {
			t.Errorf("Expand(%q) = %q, want %q", c.tilde, got, c.abs)
		}
	}
	if got := Tilde(home+"/", "/home/dev/a"); got != "~/a" {
		t.Errorf("Tilde with trailing slash = %q", got)
	}
}

func TestWriteEmptyListsAreNotNull(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a.json")
	b := filepath.Join(t.TempDir(), "b.json")
	if err := (Lock{Livery: "x", Source: "s", Commit: "c", Files: map[string]string{}}).Write(a); err != nil {
		t.Fatal(err)
	}
	if err := (Lock{Livery: "x", Source: "s", Commit: "c", Teams: []string{}, Files: map[string]string{}, Harnesses: []string{}}).Write(b); err != nil {
		t.Fatal(err)
	}
	da, _ := os.ReadFile(a)
	db, _ := os.ReadFile(b)
	if string(da) != string(db) || strings.Contains(string(da), "null") {
		t.Fatalf("nil and empty lists differ:\n%s\n%s", da, db)
	}
}

func TestSumJSONNumberSpellingIsKept(t *testing.T) {
	a, _ := SumJSON(json.RawMessage(`{"a":1}`))
	b, _ := SumJSON(json.RawMessage(`{"a":1.0}`))
	if a == b {
		t.Fatal("number spelling is documented as not normalised")
	}
}

func TestReadUnreadableNamesACommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	if err := os.Mkdir(path, 0o755); err != nil { // reading a directory fails
		t.Fatal(err)
	}
	_, found, err := ReadIn(path, Scope{Home: "/h"})
	var e *exit.Error
	if found || !errors.As(err, &e) || e.Code != exit.Environment {
		t.Fatalf("found %v, err %v", found, err)
	}
	if !strings.Contains(e.Message, path) || !strings.Contains(e.Message, "livery doctor") {
		t.Fatalf("message %q", e.Message)
	}
}

func TestWriteFlatSchema2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	in := Lock{Livery: "0.1.0", Source: "u", Channel: "stable", Tag: "v1", Commit: "abc", Teams: []string{"web"},
		Harnesses: []string{"kiro"}, Files: map[string]string{"~/a": "sha256:1"},
		Merged: []Merged{{Path: "~/b.json", Key: "k", Entries: map[string]string{"e": "sha256:2"}}}}
	if err := in.Write(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "{\n  \"schema\": 2,") {
		t.Fatalf("got %s", b)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["sources"]; ok {
		t.Fatalf("key sources in %s", b)
	}
	// The top-level keys, in the order they are written.
	dec := json.NewDecoder(strings.NewReader(string(b)))
	var keys []string
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"schema", "livery", "source", "channel", "tag", "commit", "teams", "harnesses", "files", "merged"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	// tag and merged are left out when empty.
	in.Tag, in.Merged = "", nil
	if err := in.Write(path); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), `"tag"`) || strings.Contains(string(b), `"merged"`) {
		t.Fatalf("empty tag or merged written:\n%s", b)
	}
}

func TestReadShapes(t *testing.T) {
	flat := Lock{Schema: Schema, Livery: "0.1.0", Source: "https://example.org/team/Setup.git", Channel: "stable", Tag: "v1.0.0",
		Commit: "abc", Teams: []string{"web"}, Harnesses: []string{"kiro"}, Files: map[string]string{"~/a": "sha256:1"}}
	const rest = `"harnesses":["kiro"],"files":{"~/a":"sha256:1"}`
	cases := []struct {
		name, data string
		code       exit.Code // 0: ok, flat is returned
		want       string    // parts of the message
	}{
		{"schema 2", `{"schema":2,"livery":"0.1.0","source":"https://example.org/team/Setup.git","channel":"stable","tag":"v1.0.0","commit":"abc","teams":["web"],` + rest + `}`, 0, ""},
		{"schema 1, one source", `{"schema":1,"livery":"0.1.0","sources":[{"name":"setup","source":"https://example.org/team/Setup.git","channel":"stable","tag":"v1.0.0","commit":"abc","teams":["web"]}],` + rest + `}`, exit.Finding, "older livery|livery setup"},
		{"no schema, top level", `{"livery":"0.1.0","source":"https://example.org/team/Setup.git","channel":"stable","tag":"v1.0.0","commit":"abc","teams":["web"],` + rest + `}`, exit.Finding, "older livery|livery setup"},
		{"schema 3", `{"schema":3,"livery":"9.0.0","future":true}`, exit.Finding, "lock schema 3|upgrade livery"},
		{"schema 2 with sources", `{"schema":2,"livery":"0.1.0","sources":[{"name":"setup","source":"u","channel":"stable","commit":"abc","teams":[]}],` + rest + `}`, exit.Finding, "not a livery lock"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lock.json")
			if err := os.WriteFile(path, []byte(c.data), 0o644); err != nil {
				t.Fatal(err)
			}
			l, found, err := ReadIn(path, Scope{Home: "/h"})
			if c.code == 0 {
				if err != nil || !found || !reflect.DeepEqual(l, flat) {
					t.Fatalf("found %v, err %v, lock %+v", found, err, l)
				}
				return
			}
			var e *exit.Error
			if found || !errors.As(err, &e) || e.Code != c.code || !strings.Contains(e.Message, path) {
				t.Fatalf("found %v, err %v", found, err)
			}
			for _, w := range strings.Split(c.want, "|") {
				if !strings.Contains(e.Message, w) {
					t.Fatalf("message %q lacks %q", e.Message, w)
				}
			}
			if c.name == "schema 3" && !errors.Is(err, ErrNewer) {
				t.Fatalf("err %v is not ErrNewer", err)
			}
		})
	}
}

func TestReadOlderProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	data := `{"schema":1,"livery":"0.1.0","sources":[],"harnesses":[],"files":{}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadIn(path, Scope{Home: "/h", Project: "/p"})
	want := path + ": written by an older livery that this version cannot read; remove it, then run livery setup --project"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v\nwant %s", err, want)
	}
}

func TestReadUndecodableIsNotALock(t *testing.T) {
	// Whatever version wrote it, a lock that does not decode is not a lock.
	for _, data := range []string{`{"schema":2,"livery":"9.9.9","future":1,"source":"u","commit":"c"}`, `{"schema":2,"livery":"0.0.0-dev","future":1}`} {
		path := filepath.Join(t.TempDir(), "lock.json")
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := ReadIn(path, Scope{Home: "/h"})
		var e *exit.Error
		if !errors.As(err, &e) || e.Code != exit.Finding || !strings.Contains(e.Message, "not a livery lock") {
			t.Fatalf("%q: err %v", data, err)
		}
	}
}

func TestReadNegativeSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	if err := os.WriteFile(path, []byte(`{"schema": -1, "livery": "0.1.0", "source": "u", "channel": "stable", "commit": "abc", "teams": [], "harnesses": [], "files": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadIn(path, Scope{Home: "/h"})
	var e *exit.Error
	if !errors.As(err, &e) || e.Code != exit.Finding || !strings.Contains(e.Message, "older livery") {
		t.Fatalf("err %v", err)
	}
}

func TestSameAsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.json")
	in := Lock{Livery: "0.1.0", Source: "u", Commit: "abc"}
	if err := in.Write(path); err != nil {
		t.Fatal(err)
	}
	other := in
	other.Livery = "0.2.0"
	if !other.SameAsFile(path) {
		t.Fatal("a different livery version alone counts as different")
	}
	other.Harnesses = []string{"x"}
	if other.SameAsFile(path) {
		t.Fatal("different lock counted as same")
	}
	// A schema 1 lock holding the same is not the same file: the next write turns it into schema 2.
	old := `{"schema":1,"livery":"0.1.0","sources":[{"name":"u","source":"u","channel":"","commit":"abc","teams":[]}],"harnesses":[],"files":null}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if in.SameAsFile(path) {
		t.Fatal("schema 1 lock counted as same")
	}
}

func TestSortMergedOrder(t *testing.T) {
	got := []Merged{
		{Path: "b", Key: "k", List: true},
		{Path: "b", Key: "k"},
		{Path: "b", Key: "a"},
		{Path: "a", Key: "z"},
	}
	SortMerged(got)
	want := []Merged{
		{Path: "a", Key: "z"},
		{Path: "b", Key: "a"},
		{Path: "b", Key: "k"},
		{Path: "b", Key: "k", List: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SortMerged = %+v, want %+v", got, want)
	}
}
