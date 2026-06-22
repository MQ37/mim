// Terminal init, raw mode, event loop, signal handling.

package main

import (
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"github.com/MQ37/mim/internal/app"
)

type termState struct {
	termios syscall.Termios
}

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("mim: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	oldState, err := makeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer restore(int(os.Stdin.Fd()), oldState)

	// Enable SGR mouse tracking: 1000 = button events (clicks + wheel),
	// 1006 = SGR coordinate encoding. Disabled in the deferred cleanup
	// below and on exit.
	defer os.Stdout.WriteString("\033[?1006l\033[?1000l")
	defer os.Stdout.WriteString("\033[?25h\033[?1049l\033[0m")

	os.Stdout.WriteString("\033[?1049h\033[?25l")
	os.Stdout.WriteString("\033[?1000h\033[?1006h")
	os.Stdout.WriteString("\033[2J\033[H")

	tw, th, err := getTermSize(int(os.Stdin.Fd()))
	if err != nil {
		tw, th = 80, 24
	}

	a := &app.App{
		TermW:       tw,
		TermH:       th,
		TreeVisible: true,
	}
	a.TreeW = tw * 30 / 100
	if a.TreeW < 15 {
		a.TreeW = 15
	}
	if a.TreeW > 40 {
		a.TreeW = 40
	}

	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)

	cwd, _ := os.Getwd()
	t, err := app.NewTree(cwd)
	if err != nil {
		t = &app.Tree{RootPath: cwd}
	}
	a.Tree = *t
	a.Focus = app.TreeFocus

	a.Render()

	for !a.Quit {
		key := readKey()
		if key == nil {
			select {
			case <-sigwinch:
				handleResize(a)
				a.Render()
			default:
			}
			continue
		}

		a.Dispatch(key)
		a.Render()

		select {
		case <-sigwinch:
			handleResize(a)
			a.Render()
		default:
		}
	}

	return nil
}

func handleResize(a *app.App) {
	tw, th, err := getTermSize(int(os.Stdin.Fd()))
	if err != nil || tw <= 0 || th <= 0 {
		return
	}
	a.TermW = tw
	a.TermH = th
	a.TreeW = tw * 30 / 100
	if a.TreeW < 15 {
		a.TreeW = 15
	}
	if a.TreeW > 40 {
		a.TreeW = 40
	}
}

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

func readKey() []byte {
	var buf [1]byte
	n, err := os.Stdin.Read(buf[:])
	if err != nil || n == 0 {
		return nil
	}
	if buf[0] != 0x1b {
		return buf[:]
	}

	syscall.SetNonblock(0, true)
	defer syscall.SetNonblock(0, false)

	seq := []byte{0x1b}
	for {
		n, err := syscall.Read(0, buf[:])
		if n == 0 || err != nil {
			break
		}
		seq = append(seq, buf[0])
		// Mouse (SGR) reports can exceed 10 bytes for large coordinates
		// (e.g. \033[<0;200;80M), so allow up to 32 bytes. The loop also
		// stops as soon as the non-blocking read returns no more bytes.
		if len(seq) >= 32 {
			break
		}
	}
	return seq
}
