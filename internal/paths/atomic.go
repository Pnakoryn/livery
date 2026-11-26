package paths

import (
	"os"
	"path/filepath"
)

// CreateError is returned by WriteAtomic when the temporary file cannot be created.
type CreateError struct{ Err error }

func (e *CreateError) Error() string { return e.Err.Error() }
func (e *CreateError) Unwrap() error { return e.Err }

// CheckError is returned by WriteAtomic when check failed: the file was not replaced.
type CheckError struct{ Err error }

func (e *CheckError) Error() string { return e.Err.Error() }
func (e *CheckError) Unwrap() error { return e.Err }

// WriteAtomic writes data to a temporary file next to path (named after pattern, see
// os.CreateTemp), syncs it, and renames it over path with the given mode. The directory of path
// must exist. check, when not nil, runs right before the rename; its error comes back as a
// *CheckError. The temporary file is removed on failure. Other errors are the raw I/O errors,
// a *CreateError when the temporary file cannot be created.
func WriteAtomic(path string, data []byte, mode os.FileMode, pattern string, check func() error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return &CreateError{err}
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Chmod(mode)
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil && check != nil {
		if err := check(); err != nil {
			return &CheckError{err}
		}
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	return werr
}
