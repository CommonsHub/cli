package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type EventsStatsMonth struct {
	Month string `json:"month"`
	Count int    `json:"count"`
}

type EventsStatsResult struct {
	Total    int                `json:"total"`
	Upcoming int                `json:"upcoming"`
	Months   []EventsStatsMonth `json:"months"`
}

func EventsStats(args []string) {
	if HasFlag(args, "--help", "-h") {
		PrintEventsStatsHelp()
		return
	}

	jsonOut := GetOption(args, "--format") == "json"
	events := loadAllEvents()
	now := time.Now()

	monthCounts := map[string]int{}
	upcoming := 0

	for _, e := range events {
		t, err := time.Parse(time.RFC3339, e.StartAt)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05.000Z", e.StartAt)
			if err != nil {
				continue
			}
		}
		ym := t.Format("2006-01")
		monthCounts[ym]++
		if t.After(now) {
			upcoming++
		}
	}

	// Sort months descending
	var months []EventsStatsMonth
	for ym, count := range monthCounts {
		months = append(months, EventsStatsMonth{Month: ym, Count: count})
	}
	sort.Slice(months, func(i, j int) bool {
		return months[i].Month > months[j].Month
	})

	if months == nil {
		months = []EventsStatsMonth{}
	}
	result := EventsStatsResult{
		Total:    len(events),
		Upcoming: upcoming,
		Months:   months,
	}

	if jsonOut {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("📊 Events: %d total (%d upcoming)\n", result.Total, result.Upcoming)
	for _, m := range result.Months {
		fmt.Printf("  %s: %d events\n", m.Month, m.Count)
	}
}
