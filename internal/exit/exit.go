// Package exit holds the exit-code contract: 0 ok, 1 finding, 2 bad input, 3 environment.
package exit

import (
	"fmt"
	"strings"
)

// Code is a non-zero process exit code.
type Code int

const (
	// Finding is a failed doctor check, drift, or a collision.
	Finding Code = 1
	// Usage is bad input: a flag, the config, the content of the source, or a missing choice without a terminal.
	Usage Code = 2
	// Environment is something missing on the machine (git, a harness, the network).
	Environment Code = 3
)

// Error is an error that carries the exit code the process ends with. Its message names the
// file or flag and the next command to run.
type Error struct {
	Code    Code
	Message string
	// Cause, when set, is what errors.Is and errors.As see below this error; it is not printed.
	Cause error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns Cause.
func (e *Error) Unwrap() error { return e.Cause }

// OneLine returns the message of err on one line: each line break becomes lineBreak, then
// every run of white space one space.
func OneLine(err error, lineBreak string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(err.Error(), "\n", lineBreak)), " ")
}

// Findingf returns an Error with code Finding.
func Findingf(format string, a ...any) *Error {
	return &Error{Code: Finding, Message: fmt.Sprintf(format, a...)}
}

// Usagef returns an Error with code Usage.
func Usagef(format string, a ...any) *Error {
	return &Error{Code: Usage, Message: fmt.Sprintf(format, a...)}
}

// Environmentf returns an Error with code Environment.
func Environmentf(format string, a ...any) *Error {
	return &Error{Code: Environment, Message: fmt.Sprintf(format, a...)}
}
