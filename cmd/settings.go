package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings represents src/settings/settings.json
type Settings struct {
	Luma struct {
		CalendarID string `json:"calendarId"`
	} `json:"luma"`
	Calendars struct {
		Google string `json:"google"`
		Luma   string `json:"luma"`
	} `json:"calendars"`
}

// RoomInfo represents a single room from rooms.json
type RoomInfo struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Slug             string   `json:"slug"`
	Capacity         int      `json:"capacity"`
	Description      string   `json:"description"`
	PricePerHour     float64  `json:"pricePerHour"`
	TokensPerHour    float64  `json:"tokensPerHour"`
	Features         []string `json:"features"`
	IdealFor         []string `json:"idealFor"`
	GoogleCalendarID *string  `json:"googleCalendarId"`
	MembershipReq    bool     `json:"membershipRequired,omitempty"`
}

// RoomsConfig represents rooms.json
type RoomsConfig struct {
	Rooms []RoomInfo `json:"rooms"`
}

func LoadSettings() (*Settings, error) {
	data, err := os.ReadFile(filepath.Join("src", "settings", "settings.json"))
	if err != nil {
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func LoadRooms() ([]RoomInfo, error) {
	data, err := os.ReadFile(filepath.Join("src", "settings", "rooms.json"))
	if err != nil {
		return nil, err
	}
	var rc RoomsConfig
	if err := json.Unmarshal(data, &rc); err != nil {
		return nil, err
	}
	return rc.Rooms, nil
}
