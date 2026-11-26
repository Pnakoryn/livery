package source

import (
	"slices"
	"testing"
)

func TestValidChannel(t *testing.T) {
	cases := map[string]bool{
		"stable":     true,
		"next":       true,
		"v1.2.3":     true,
		"v0.0.0":     true,
		"v10.20.30":  true,
		"":           false,
		"nightly":    false,
		"Stable":     false,
		"1.2.3":      false,
		"v1.2":       false,
		"v1.2.3.4":   false,
		"v1.2.3-rc1": false,
		"v01.2.3":    false,
		"v1.2.x":     false,
		" v1.2.3":    false,
		"v1.2.3\n":   false,
	}
	for in, want := range cases {
		if got := ValidChannel(in); got != want {
			t.Errorf("ValidChannel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPickStable(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
		ok   bool
	}{
		{"no tags", nil, "", false},
		{"numeric, not lexical", []string{"v0.9.0", "v0.10.0", "v0.2.11"}, "v0.10.0", true},
		{"ignores non-release tags", []string{"v1.0.0", "v1.0.0-rc1", "nightly", "v2", "1.5.0", "v0.99.99"}, "v1.0.0", true},
		{"only non-release tags", []string{"latest", "v1.2"}, "", false},
		{"major wins", []string{"v2.0.0", "v10.0.0", "v9.9.9"}, "v10.0.0", true},
		{"minor before patch", []string{"v1.2.3", "v1.2.10", "v1.10.0"}, "v1.10.0", true},
		{"overflowing versions are ignored", []string{"v99999999999999999999.0.0", "v1.0.0", "v0.99999999999999999999.0"}, "v1.0.0", true},
		{"overflowing versions only", []string{"v1.0.99999999999999999999"}, "", false},
		{"leading zeros are not releases", []string{"v01.0.0", "v0.1.0"}, "v0.1.0", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := PickStable(c.tags)
			if got != c.want || ok != c.ok {
				t.Errorf("PickStable(%v) = %q, %v; want %q, %v", c.tags, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestDefaultBranch(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"main", "ref: refs/heads/main\tHEAD\n30cc080fa01f6d3b8d5f0a1c2b3d4e5f60718293\tHEAD\n", "main", true},
		{"branch with a slash", "ref: refs/heads/release/2026\tHEAD\nabc\tHEAD\n", "release/2026", true},
		{"no symref line", "30cc080fa01f6d3b8d5f0a1c2b3d4e5f60718293\tHEAD\n", "", false},
		{"empty", "", "", false},
		{"empty branch name", "ref: refs/heads/\tHEAD\n", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := DefaultBranch(c.in)
			if got != c.want || ok != c.ok {
				t.Errorf("DefaultBranch(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestReleaseTagsOrderMultiDigit(t *testing.T) {
	got := ReleaseTags([]string{"v0.9.0", "v0.10.0", "junk", "v0.2.10", "v0.2.9", "v1.0.0", "v0.10.1"})
	want := []string{"v1.0.0", "v0.10.1", "v0.10.0", "v0.9.0", "v0.2.10", "v0.2.9"}
	if !slices.Equal(got, want) {
		t.Errorf("ReleaseTags = %v, want %v", got, want)
	}
}
