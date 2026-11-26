//go:build e2e && linux

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const sourcePrompt = "Address of your team's shared repository"

// openPty opens a pseudo terminal and returns its master and slave side.
func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("/dev/ptmx cannot be opened: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	var unlock int32 // TIOCSPTLCK with 0 unlocks the slave
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Fatalf("TIOCSPTLCK: %v", errno)
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		t.Fatalf("TIOCGPTN: %v", errno)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("pseudo-terminal slave cannot be opened: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}

// With a terminal on stdin and stdout, `livery setup` asks for the source; without one it never
// prompts and exits 2 naming --source. isTerminal on the real file descriptors decides.
func TestSetupPromptsOnlyOnATerminal(t *testing.T) {
	t.Run("terminal", func(t *testing.T) {
		j := newHome(t)
		master, slave := openPty(t)
		cmd := exec.Command(binary, "setup")
		cmd.Dir = j.home
		cmd.Env = []string{"HOME=" + j.home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0"}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

		var mu sync.Mutex
		var got bytes.Buffer
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := master.Read(buf)
				mu.Lock()
				got.Write(buf[:n])
				mu.Unlock()
				if err != nil {
					return
				}
			}
		}()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			seen := strings.Contains(got.String(), sourcePrompt)
			mu.Unlock()
			if seen {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("no %q prompt on the terminal; got %q", sourcePrompt, got.String())
	})

	t.Run("no terminal", func(t *testing.T) {
		j := newHome(t)
		out, errs := j.expect(2, "setup") // livery runs it with stdin from /dev/null
		if !strings.Contains(errs, "--source") {
			t.Errorf("stderr does not name --source: %q", errs)
		}
		if strings.Contains(out, sourcePrompt) || strings.Contains(out, "Apply?") {
			t.Errorf("stdout contains a prompt: %q", out)
		}
	})
}
