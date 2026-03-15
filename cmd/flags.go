package cmd

import (
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
