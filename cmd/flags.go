package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func HasFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func GetOption(args []string, flags ...string) string {
	for _, flag := range flags {
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(a, flag+"=") {
				return strings.SplitN(a, "=", 2)[1]
			}
		}
	}
	return ""
}

func GetNumber(args []string, flags []string, defaultVal int) int {
	val := GetOption(args, flags...)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

// ParseYearMonthArg extracts a positional year or year/month argument from args.
// Accepts formats: "2025", "2025/11", "2025/1".
// Returns (year, month, found). If only year, month is "".
// month is always zero-padded (e.g. "01").
func ParseYearMonthArg(args []string) (year string, month string, found bool) {
	// Skip flags and their values
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "--") || strings.HasPrefix(a, "-") {
			// Flags that take a value
			if a == "--since" || a == "--month" || a == "--channel" || a == "--room" || a == "-n" {
				skipNext = true
			}
			continue
		}
		// Try to parse as year or year/month
		parts := strings.SplitN(a, "/", 2)
		if len(parts[0]) != 4 {
			continue
		}
		y, err := strconv.Atoi(parts[0])
		if err != nil || y < 2000 || y > 2100 {
			continue
		}
		year = parts[0]
		found = true
		if len(parts) == 2 {
			m, err := strconv.Atoi(parts[1])
			if err != nil || m < 1 || m > 12 {
				continue
			}
			month = fmt.Sprintf("%02d", m)
		}
		return
	}
	return "", "", false
}

func ParseSinceDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	clean := strings.ReplaceAll(s, "-", "")
	if len(clean) != 8 {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102", clean)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
