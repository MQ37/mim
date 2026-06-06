// main.go — terminal init/restore, event loop, signal handling.
// This is the entry point and lifecycle manager. Subagents do NOT modify this file.

package main

import (
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

func main() {
	if err := run(); err != nil {
		// Terminal is already restored by defer in run().
		// Write error to stderr, which should still work.
		os.Stderr.WriteString("mim: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	// Enter raw mode.
	oldState, err := makeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer restore(int(os.Stdin.Fd()), oldState)

	// Hide cursor on exit (best-effort).
	defer os.Stdout.WriteString("\033[?25h\033[?1049l\033[0m")

	// Enter alternate screen, hide cursor.
	os.Stdout.WriteString("\033[?1049h\033[?25l")
	os.Stdout.WriteString("\033[2J\033[H")

	// Get terminal size.
	tw, th, err := getTermSize(int(os.Stdin.Fd()))
	if err != nil {
		tw, th = 80, 24
	}

	app := &App{
		termW:      tw,
		termH:      th,
		treeVisible: true,
	}
	app.treeW = tw * 30 / 100
	if app.treeW < 15 {
		app.treeW = 15
	}
	if app.treeW > 40 {
		app.treeW = 40
	}

	// SIGWINCH handling.
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)

	// Initial tree build from cwd.
	cwd, _ := os.Getwd()
	t, err := newTree(cwd)
	if err != nil {
		t = &Tree{rootPath: cwd}
	}
	app.tree = *t
	app.focus = TreeFocus

	// Render initial frame.
	app.render()

	// Main event loop.
	for !app.quit {
		// Wait for either a keypress or a signal.
		key := readKey()
		if key == nil {
			// Check for pending SIGWINCH.
			select {
			case <-sigwinch:
			if tw2, th2, err := getTermSize(int(os.Stdin.Fd())); err == nil && tw2 > 0 && th2 > 0 {
				tw, th = tw2, th2
				app.termW = tw
				app.termH = th
				app.treeW = tw * 30 / 100
				if app.treeW < 15 {
					app.treeW = 15
				}
				if app.treeW > 40 {
					app.treeW = 40
				}
			}
			app.render()
			default:
			}
			continue
		}

		app.dispatch(key)
		app.render()

		// Drain any pending SIGWINCH after render (non-blocking).
		select {
		case <-sigwinch:
			if tw2, th2, err := getTermSize(int(os.Stdin.Fd())); err == nil && tw2 > 0 && th2 > 0 {
				tw, th = tw2, th2
				app.termW = tw
				app.termH = th
				app.treeW = tw * 30 / 100
				if app.treeW < 15 {
					app.treeW = 15
				}
				if app.treeW > 40 {
					app.treeW = 40
				}
			}
			app.render()
		default:
		}
	}

	return nil
}

// --- Terminal raw mode (vendored from golang.org/x/term for Linux) ---

func makeRaw(fd int) (*termState, error) {
	var t syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	if errno != 0 {
		return nil, errno
	}
	oldState := termState{termios: t}

	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0

	_, _, errno = syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	if errno != 0 {
		return nil, errno
	}
	return &oldState, nil
}

func restore(fd int, state *termState) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&state.termios)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func getTermSize(fd int) (int, int, error) {
	var ws struct {
		row    uint16
		col    uint16
		xpixel uint16
		ypixel uint16
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0, 0, errno
	}
	return int(ws.col), int(ws.row), nil
}

// --- Keyboard input ---

// readKey reads a single keypress from stdin and returns the raw byte sequence.
// For regular keys this is a 1-byte slice. Escape sequences (arrows, etc.)
// are multi-byte. Returns nil on error (e.g. stdin closed).
func readKey() []byte {
	var buf [1]byte
	n, err := os.Stdin.Read(buf[:])
	if err != nil || n == 0 {
		return nil
	}
	if buf[0] != 0x1b {
		return buf[:]
	}

	// ESC received — check if it starts an escape sequence.
	// Use syscall.Read with O_NONBLOCK to avoid mixing os.File with raw fcntl.
	syscall.SetNonblock(0, true)
	defer syscall.SetNonblock(0, false)

	seq := []byte{0x1b}
	for {
		n, err := syscall.Read(0, buf[:])
		if n == 0 || err != nil {
			break
		}
		seq = append(seq, buf[0])
		if len(seq) >= 10 {
			break
		}
	}
	return seq
}


