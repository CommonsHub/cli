package cmd

import "fmt"

func PrintHelp(version string) {
	f := Fmt
	fmt.Printf(`
%schb%s %sv%s%s — Commons Hub Brussels CLI

%sUSAGE%s
  %schb%s <command> [options]

%sCOMMANDS%s
  %sevents%s              List upcoming events
  %sevents sync%s         Fetch events from Luma feeds
  %srooms%s               List all rooms with pricing
  %sbookings%s            List upcoming room bookings
  %sbookings sync%s       Sync room booking calendars
  %stransactions sync%s   Fetch blockchain transactions
  %smessages sync%s       Fetch Discord messages
  %ssync%s                Sync everything (events, transactions, bookings, messages)
  %sreport%s <period>     Generate monthly/yearly report

%sOPTIONS%s
  %s--help, -h%s          Show help for a command
  %s--version, -v%s       Show version

%sEXAMPLES%s
  %s$ chb events                          # next 10 upcoming events
  $ chb events sync                      # sync events from Luma
  $ chb transactions sync                # sync blockchain transactions
  $ chb messages sync                    # sync Discord messages
  $ chb report 2025/11                   # monthly report
  $ chb report 2025                      # yearly report%s

%sENVIRONMENT%s
  %sDATA_DIR%s            Data directory (default: ./data)
  %sLUMA_API_KEY%s        Luma API key (enables rich event data)
  %sETHERSCAN_API_KEY%s   Etherscan/Gnosisscan API key
  %sDISCORD_BOT_TOKEN%s   Discord bot token
`,
		f.Bold, f.Reset, f.Dim, version, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Bold, f.Reset,
		f.Dim, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
	)
}

func PrintEventsHelp() {
	f := Fmt
	fmt.Printf(`
%schb events%s — List events from the local data directory

%sUSAGE%s
  %schb events%s [options]

%sOPTIONS%s
  %s-n%s <count>           Number of events to show (default: 10)
  %s--since%s <YYYYMMDD>   Only events starting after this date (default: today)
  %s--skip%s <count>       Skip first N events
  %s--all%s                Show all events (no date filter)
  %s--help, -h%s           Show this help
`,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
	)
}

func PrintEventsSyncHelp() {
	f := Fmt
	fmt.Printf(`
%schb events sync%s — Fetch events from Luma and regenerate data

%sUSAGE%s
  %schb events sync%s [options]

%sOPTIONS%s
  %s--since%s <YYYYMMDD>   Start syncing from this date (default: last month)
  %s--force%s              Re-fetch even if cached data exists
  %s--history%s            Rebuild entire event history
  %s--help, -h%s           Show this help

%sSOURCES%s
  • Luma ICS feed (calendar events)
  • Luma API (covers, guests, tags — requires LUMA_API_KEY)
`,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Bold, f.Reset,
	)
}

func PrintBookingsHelp() {
	f := Fmt
	fmt.Printf(`
%schb bookings%s — List room bookings from cached calendar data

%sUSAGE%s
  %schb bookings%s [options]

%sOPTIONS%s
  %s-n%s <count>           Number of bookings to show (default: 10)
  %s--skip%s <count>       Skip first N bookings
  %s--date%s <YYYYMMDD>    Show bookings for a specific date
  %s--room%s <slug>        Filter by room slug
  %s--all%s                Show all bookings (no date filter)
  %s--help, -h%s           Show this help
`,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
	)
}

func PrintBookingsSyncHelp() {
	f := Fmt
	fmt.Printf(`
%schb bookings sync%s — Sync room booking calendars from Google Calendar

%sUSAGE%s
  %schb bookings sync%s [options]

%sOPTIONS%s
  %s--room%s <slug>        Only sync a specific room (e.g. satoshi)
  %s--force%s              Re-fetch even if cached data exists
  %s--since%s <YYYYMMDD>   Start syncing from this date
  %s--help, -h%s           Show this help
`,
		f.Bold, f.Reset,
		f.Bold, f.Reset,
		f.Cyan, f.Reset,
		f.Bold, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
		f.Yellow, f.Reset,
	)
}
