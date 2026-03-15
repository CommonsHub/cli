package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
)

type BookingsStatsMonth struct {
	Month string   `json:"month"`
	Count int      `json:"count"`
	Rooms []string `json:"rooms"`
}

type BookingsStatsResult struct {
	Total  int                  `json:"total"`
	Months []BookingsStatsMonth `json:"months"`
}

func BookingsStats(args []string) {
	if HasFlag(args, "--help", "-h") {
		PrintBookingsStatsHelp()
		return
	}

	jsonOut := GetOption(args, "--format") == "json"
	bookings, _ := loadAllBookings()

	type monthInfo struct {
		count int
		rooms map[string]bool
	}
	monthData := map[string]*monthInfo{}

	for _, b := range bookings {
		ym := b.Start.Format("2006-01")
		if monthData[ym] == nil {
			monthData[ym] = &monthInfo{rooms: map[string]bool{}}
		}
		monthData[ym].count++
		monthData[ym].rooms[b.Room] = true
	}

	var months []BookingsStatsMonth
	for ym, info := range monthData {
		var rooms []string
		for r := range info.rooms {
			rooms = append(rooms, r)
		}
		sort.Strings(rooms)
		months = append(months, BookingsStatsMonth{
			Month: ym,
			Count: info.count,
			Rooms: rooms,
		})
	}
	sort.Slice(months, func(i, j int) bool {
		return months[i].Month > months[j].Month
	})

	if months == nil {
		months = []BookingsStatsMonth{}
	}
	result := BookingsStatsResult{Total: len(bookings), Months: months}

	if jsonOut {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("📊 Bookings: %d total\n", result.Total)
	for _, m := range result.Months {
		fmt.Printf("  %s: %d bookings [%s]\n", m.Month, m.Count, joinStrings(m.Rooms))
	}
}
