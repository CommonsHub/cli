package cmd

import (
	"fmt"
	"os"
)

type statusLine struct {
	active bool
	tty    bool
}

func newStatusLine() *statusLine {
	info, err := os.Stdout.Stat()
	return &statusLine{tty: err == nil && (info.Mode()&os.ModeCharDevice) != 0}
}

func (s *statusLine) Update(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	// Always bubble up to the outer StatusLine (chb pull / providers pull
	// installs one on stderr); this keeps the operator informed even when
	// the outer caller silenced our stdout to keep the layout tight.
	Progress(msg)
	if s == nil || !s.tty {
		return
	}
	fmt.Printf("\r\033[K  %s", msg)
	s.active = true
}

func (s *statusLine) Clear() {
	if s == nil || !s.active || !s.tty {
		return
	}
	fmt.Print("\r\033[K")
	s.active = false
}
