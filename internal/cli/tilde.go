package cli

import (
	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/lock"
)

// expandHome turns a leading "~" or "~/" of a value into the home folder of the run, as the shell
// would. Other values (URLs, user@host:path, "~user/...") are returned unchanged.
func expandHome(env adapter.Env, p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	return lock.Expand(env.Home, p)
}
