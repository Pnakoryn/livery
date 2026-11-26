package cli

import (
	"runtime/debug"
	"testing"
)

func TestVersionSources(t *testing.T) {
	oldV, oldR := Version, readBuildInfo
	t.Cleanup(func() { Version, readBuildInfo = oldV, oldR })

	cases := []struct {
		name, ldflags, module, want string
	}{
		{"ldflags wins", "1.2.3", "v0.1.0", "1.2.3"},
		{"module version", defaultVersion, "v0.1.0", "0.1.0"},
		{"devel", defaultVersion, "(devel)", defaultVersion},
		{"empty", defaultVersion, "", defaultVersion},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			Version = c.ldflags
			readBuildInfo = func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{Main: debug.Module{Version: c.module}}, true
			}
			if got := version(); got != c.want {
				t.Errorf("version() = %q, want %q", got, c.want)
			}
		})
	}
}
