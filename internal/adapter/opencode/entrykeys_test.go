package opencode

import (
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/adapter/adaptertest"
)

func TestEntryKeysCoverRendering(t *testing.T) {
	home := t.TempDir()
	for _, env := range []adapter.Env{
		{Home: home, Getenv: func(string) string { return "" }},
		{Home: home, Getenv: func(string) string { return "" }, Project: t.TempDir()},
	} {
		adaptertest.EntryKeysCover(t, New(), env)
	}
}
