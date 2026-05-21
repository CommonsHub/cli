package cmd

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// TerminalWidth returns the column count for stderr (where the
// streaming status line is drawn). Tries ioctl(TIOCGWINSZ) on stderr
// first, then $COLUMNS, then falls back to a safe default. Cached at
// process start so repeated reads stay cheap.
func TerminalWidth() int {
	if w := cachedTerminalWidth(); w > 0 {
		return w
	}
	return 100
}

var (
	termWidthCache   int
	termWidthLooked  bool
)

func cachedTerminalWidth() int {
	if termWidthLooked {
		return termWidthCache
	}
	termWidthLooked = true
	if w := termWidthFromIoctl(int(os.Stderr.Fd())); w > 0 {
		termWidthCache = w
		return w
	}
	if s := os.Getenv("COLUMNS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			termWidthCache = n
			return n
		}
	}
	return 0
}

// winsize matches Linux/macOS struct winsize for TIOCGWINSZ. Only the
// Col field is read.
type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

func termWidthFromIoctl(fd int) int {
	ws := &winsize{}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)),
	)
	if errno != 0 || ws.Col == 0 {
		return 0
	}
	return int(ws.Col)
}
