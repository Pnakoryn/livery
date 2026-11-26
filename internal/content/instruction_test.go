package content

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func sameInstruction(a, b Instruction) bool {
	if len(a.Globs) == 0 && len(b.Globs) == 0 {
		a.Globs, b.Globs = nil, nil
	}
	return reflect.DeepEqual(a, b)
}

func TestParseInstruction(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Instruction
	}{
		{"no frontmatter", "# Title\n\nText.\n", Instruction{Body: "# Title\n\nText.\n"}},
		{"empty frontmatter", "---\n---\nText.\n", Instruction{Body: "Text.\n"}},
		{"description", "---\ndescription: Go style\n---\n\nText.\n", Instruction{Description: "Go style", Body: "Text.\n"}},
		{"double-quoted description", "---\ndescription: \"Go: style\"\n---\nText.\n", Instruction{Description: "Go: style", Body: "Text.\n"}},
		{"single-quoted description", "---\ndescription: 'it works'\n---\nText.\n", Instruction{Description: "it works", Body: "Text.\n"}},
		{"inline globs", "---\nglobs: [\"**/*.go\", 'go.mod', Makefile]\n---\nText.\n", Instruction{Globs: []string{"**/*.go", "go.mod", "Makefile"}, Body: "Text.\n"}},
		{"block globs", "---\ndescription: x\nglobs:\n  - \"**/*.ts\"\n  - src/**\n---\nText.\n", Instruction{Description: "x", Globs: []string{"**/*.ts", "src/**"}, Body: "Text.\n"}},
		{"single string glob", "---\nglobs: \"**/*.md\"\n---\nText.\n", Instruction{Globs: []string{"**/*.md"}, Body: "Text.\n"}},
		{"empty inline list means always", "---\nglobs: []\n---\nText.\n", Instruction{Body: "Text.\n"}},
		{"trailing comma dropped", "---\nglobs: [a, ]\n---\nText.\n", Instruction{Globs: []string{"a"}, Body: "Text.\n"}},
		{"only empty items means always", "---\nglobs: [\"\", ,]\n---\nText.\n", Instruction{Body: "Text.\n"}},
		{"empty string glob means always", "---\nglobs: \"\"\n---\nText.\n", Instruction{Body: "Text.\n"}},
		{"bare dash item dropped", "---\nglobs:\n  -\n  - a\n---\nText.\n", Instruction{Globs: []string{"a"}, Body: "Text.\n"}},
		{"bare globs key means always", "---\nglobs:\n---\nText.\n", Instruction{Body: "Text.\n"}},
		{"comments and empty lines", "---\n# a comment\n\ndescription: x\n---\nText.\n", Instruction{Description: "x", Body: "Text.\n"}},
		{"crlf", "---\r\ndescription: x\r\n---\r\nText.\r\n", Instruction{Description: "x", Body: "Text.\n"}},
		{"dashes later are body", "Text.\n---\nmore\n", Instruction{Body: "Text.\n---\nmore\n"}},
		{"quoted brace glob", "---\nglobs: [\"**/*.{ts,tsx}\"]\n---\nT\n", Instruction{Globs: []string{"**/*.{ts,tsx}"}, Body: "T\n"}},
		{"unquoted brace glob", "---\nglobs: [**/*.{ts,tsx}, docs/**]\n---\nT\n", Instruction{Globs: []string{"**/*.{ts,tsx}", "docs/**"}, Body: "T\n"}},
		{"quoted commas", "---\nglobs: ['a,b', \"c\"]\n---\nT\n", Instruction{Globs: []string{"a,b", "c"}, Body: "T\n"}},
		{"single brace glob", "---\nglobs: \"**/*.{ts,tsx}\"\n---\nT\n", Instruction{Globs: []string{"**/*.{ts,tsx}"}, Body: "T\n"}},
		{"block brace glob", "---\nglobs:\n  - \"**/*.{js,jsx}\"\n---\nT\n", Instruction{Globs: []string{"**/*.{js,jsx}"}, Body: "T\n"}},
		{"globs before description", "---\nglobs: [a]\ndescription: y\n---\nB\n", Instruction{Description: "y", Globs: []string{"a"}, Body: "B\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseInstruction("instructions/x.md", []byte(c.in))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !sameInstruction(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestParseInstructionErrors(t *testing.T) {
	const path = "teams/web/instructions/style.md"
	cases := []struct {
		name string
		in   string
		has  string
	}{
		{"unknown key", "---\ninclusion: always\n---\nx\n", path + ":2:"},
		{"unknown key after a known one", "---\ndescription: x\nalwaysApply: true\n---\nx\n", path + ":3:"},
		{"never closed", "---\ndescription: x\n", path},
		{"line without a colon", "---\njust text\n---\nx\n", path},
		{"list item outside globs", "---\ndescription: x\n  - a\n---\nx\n", path},
		{"inline list never closed", "---\nglobs: [a, b\n---\nx\n", path},
		{"unterminated quote", "---\nglobs: [\"a]\n---\nx\n", path},
		{"unbalanced brace", "---\nglobs: [{a,b]\n---\nx\n", path},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseInstruction(path, []byte(c.in))
			if err == nil {
				t.Fatal("expected an error")
			}
			var e *exit.Error
			if !errors.As(err, &e) || e.Code != exit.Usage {
				t.Fatalf("want an *exit.Error with code Usage, got %#v", err)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("message %q does not name the file %q", err.Error(), path)
			}
			if !strings.Contains(err.Error(), c.has) {
				t.Errorf("message %q does not contain %q", err.Error(), c.has)
			}
		})
	}
}
