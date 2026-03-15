package main

import (
	"fmt"
	"os"

	"github.com/commonshub/commonshub.brussels/cli/cmd"
)

const VERSION = "1.1.0"

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		cmd.PrintHelp(VERSION)
		return
	}

	switch args[0] {
	case "--help", "-h", "help":
		cmd.PrintHelp(VERSION)
	case "--version", "-v":
		fmt.Printf("chb v%s\n", VERSION)
	case "events":
		if len(args) > 1 && args[1] == "sync" {
			if err := cmd.EventsSync(args[2:], VERSION); err != nil {
				fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cmd.Fmt.Red, cmd.Fmt.Reset, err)
				os.Exit(1)
			}
		} else {
			cmd.EventsList(args[1:])
		}
	case "rooms":
		cmd.Rooms(args[1:])
	case "bookings":
		if len(args) > 1 && args[1] == "sync" {
			if err := cmd.BookingsSync(args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "%sError:%s %v\n", cmd.Fmt.Red, cmd.Fmt.Reset, err)
				os.Exit(1)
			}
		} else {
			cmd.BookingsList(args[1:])
		}
	default:
		fmt.Fprintf(os.Stderr, "%sUnknown command: %s%s\n\n", cmd.Fmt.Red, args[0], cmd.Fmt.Reset)
		cmd.PrintHelp(VERSION)
		os.Exit(1)
	}
}
