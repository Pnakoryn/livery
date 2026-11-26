package content

import (
	"reflect"
	"testing"
)

func TestScanFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Frontmatter
	}{
		{
			name: "no frontmatter",
			in:   "# Title\n\ntext\n",
			want: Frontmatter{Body: "# Title\n\ntext\n"},
		},
		{
			name: "closed with a value, an inline list and a dash list",
			in:   "---\n# comment\ndescription: \"Go rules\"\n\nglobs: [\"*.go\", '*.mod']\npaths:\n  - a/**\n  -\n---\n\n\nbody\n",
			want: Frontmatter{
				Opened: true,
				Fields: []FrontmatterField{
					{Line: 3, Key: "description", Value: `"Go rules"`},
					{Line: 5, Key: "globs", Value: `["*.go", '*.mod']`},
					{Line: 6, Key: "paths", Items: []FrontmatterItem{{Line: 7, Value: "a/**"}, {Line: 8}}},
				},
				Body: "body\n",
			},
		},
		{
			name: "list item before any key",
			in:   "---\n- x\nkey: v\n---\n",
			want: Frontmatter{
				Opened: true,
				Items:  []FrontmatterItem{{Line: 2, Value: "x"}},
				Fields: []FrontmatterField{{Line: 3, Key: "key", Value: "v"}},
			},
		},
		{
			name: "unclosed frontmatter",
			in:   "---\ndescription: x\nbody\n",
			want: Frontmatter{Opened: true, Unclosed: true},
		},
		{
			name: "a line without a colon stops the scan",
			in:   "---\na: 1\nnot a key\nb: 2\n---\nbody",
			want: Frontmatter{
				Opened:    true,
				Fields:    []FrontmatterField{{Line: 2, Key: "a", Value: "1"}},
				Malformed: 3,
				Body:      "body",
			},
		},
		{
			name: "CRLF line ends",
			in:   "---\r\nglobs:\r\n - \"*.md\"\r\n---\r\nline one\r\nline two\r\n",
			want: Frontmatter{
				Opened: true,
				Fields: []FrontmatterField{{Line: 2, Key: "globs", Items: []FrontmatterItem{{Line: 3, Value: `"*.md"`}}}},
				Body:   "line one\nline two\n",
			},
		},
		{
			name: "CRLF without frontmatter",
			in:   "a\r\nb\r\n",
			want: Frontmatter{Body: "a\nb\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScanFrontmatter([]byte(tt.in)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanFrontmatter(%q)\n got %#v\nwant %#v", tt.in, got, tt.want)
			}
		})
	}
}
