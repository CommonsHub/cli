package cmd

import (
	"os"
)

// Colors holds ANSI escape codes for terminal output
type Colors struct {
	Reset  string
	Bold   string
	Dim    string
	Red    string
	Green  string
	Yellow string
	Blue   string
	Cyan   string
	Gray   string
}

var Fmt Colors

func init() {
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode()&os.ModeCharDevice) == 0 || os.Getenv("NO_COLOR") != "" {
		// Not a TTY or NO_COLOR set — no colors
		Fmt = Colors{}
	} else {
		Fmt = Colors{
			Reset:  "\x1b[0m",
			Bold:   "\x1b[1m",
			Dim:    "\x1b[2m",
			Red:    "\x1b[31m",
			Green:  "\x1b[32m",
			Yellow: "\x1b[33m",
			Blue:   "\x1b[34m",
			Cyan:   "\x1b[36m",
			Gray:   "\x1b[90m",
		}
	}
}
