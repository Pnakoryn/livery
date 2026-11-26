package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/exit"
	"github.com/Pnakoryn/livery/internal/paths"
)

// runLockFile is run.lock in the cache folder: setup, update and uninstall hold an exclusive
// flock on it while they write. It is never deleted; the kernel releases the lock when the
// process ends, so a crashed run leaves no stale lock.
func runLockFile(env adapter.Env) string { return filepath.Join(paths.CacheDir(env), "run.lock") }

// takeRunLock takes the run lock without waiting. The returned function releases it.
func takeRunLock(env adapter.Env, command string) (func(), error) {
	path := runLockFile(env)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, exit.Environmentf("%s: cannot create the directory: %v; check its permissions, then run livery %s%s", filepath.Dir(path), err, command, projectArg(env))
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, exit.Environmentf("%s: cannot open: %v; check its permissions, then run livery %s%s", path, err, command, projectArg(env))
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, exit.Environmentf("another livery run is in progress (%s); wait for it to finish, then run livery %s%s", path, command, projectArg(env))
		}
		return nil, exit.Environmentf("%s: cannot lock: %v; check the file system, then run livery %s%s", path, err, command, projectArg(env))
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
