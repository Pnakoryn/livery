//go:build linux

package cli

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestIsTerminalPseudoTerminal(t *testing.T) {
	t.Parallel()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("/dev/ptmx cannot be opened: %v", err)
	}
	defer func() { _ = master.Close() }()

	var unlock int32 // TIOCSPTLCK with 0 unlocks the slave
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Fatalf("TIOCSPTLCK: %v", errno)
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		t.Fatalf("TIOCGPTN: %v", errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("pseudo-terminal slave cannot be opened: %v", err)
	}
	defer func() { _ = slave.Close() }()

	if !isTerminal(slave) {
		t.Error("isTerminal(pseudo-terminal slave) = false, want true")
	}
}
