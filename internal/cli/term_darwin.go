//go:build darwin

package cli

import "syscall"

// termGet is the ioctl request that reads the terminal attributes.
const termGet = syscall.TIOCGETA
