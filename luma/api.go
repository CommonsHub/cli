package luma

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

const baseURL = "https://public-api.luma.com"

// Event represents a Luma event from the API
type Event struct {
	APIID          string            `json:"api_id"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	StartAt        string            `json:"start_at"`
	EndAt          string            `json:"end_at,omitempty"`
	Timezone       string            `json:"timezone,omitempty"`
	URL            string            `json:"url,omitempty"`
	CoverURL       string            `json:"cover_url,omitempty"`
	GeoAddressJSON json.RawMessage   `json:"geo_address_json,omitempty"`
	MeetingURL     string            `json:"meeting_url,omitempty"`
	Visibility     string            `json:"visibility,omitempty"`
	EventType      string            `json:"event_type,omitempty"`
	Capacity       int               `json:"capacity,omitempty"`
	GuestCount     int               `json:"guest_count,omitempty"`
	Hosts          []json.RawMessage `json:"hosts,omitempty"`
	HostedBy       string            `json:"hosted_by,omitempty"`
	Tags           json.RawMessage   `json:"tags,omitempty"`
}

// GeoAddress represents the location info
type GeoAddress struct {
	FullAddress string `json:"full_address"`
	Description string `json:"description"`
	Address     string `json:"address"`
}

func apiKey() string {
	return os.Getenv("LUMA_API_KEY")
}

// GetEvent fetches a single event by API ID
func GetEvent(eventID string) (*Event, error) {
	key := apiKey()
	if key == "" {
		return nil, nil
	}

	reqURL := fmt.Sprintf("%s/v1/event/get?api_id=%s", baseURL, url.QueryEscape(eventID))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("x-luma-api-key", key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 || resp.StatusCode == 404 {
		return nil, nil // Community event or deleted — expected
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Luma API error: %d %s – %s", resp.StatusCode, resp.Status, string(body))
	}

	var data struct {
		Event Event `json:"event"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.Event.APIID == "" {
		// Try direct decode (some endpoints return event at root)
		return nil, nil
	}
	return &data.Event, nil
}

// ListCalendarEventsResponse is the API response for calendar listing
type ListCalendarEventsResponse struct {
	Entries   []json.RawMessage `json:"entries"`
	HasMore   bool              `json:"has_more"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// CalendarEntry is a single entry from the calendar list
type CalendarEntry struct {
	APIID string `json:"api_id"`
	Event Event  `json:"event"`
	Tags  json.RawMessage `json:"tags,omitempty"`
}

// GetAllCalendarEvents fetches all events for a calendar in a time range
func GetAllCalendarEvents(calendarID, after, before string) ([]CalendarEntry, error) {
	key := apiKey()
	if key == "" {
		return nil, nil
	}

	var all []CalendarEntry
	cursor := ""

	for {
		params := url.Values{
			"sort_column": {"start_at"},
		}
		if after != "" {
			params.Set("after", after)
		}
		if before != "" {
			params.Set("before", before)
		}
		if cursor != "" {
			params.Set("pagination_cursor", cursor)
		}

		reqURL := fmt.Sprintf("%s/v1/calendar/list-events?%s", baseURL, params.Encode())
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return all, err
		}
		req.Header.Set("accept", "application/json")
		req.Header.Set("x-luma-api-key", key)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return all, err
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return all, fmt.Errorf("Luma API error: %d", resp.StatusCode)
		}

		var result ListCalendarEventsResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return all, err
		}

		for _, raw := range result.Entries {
			var entry CalendarEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				continue
			}
			all = append(all, entry)
		}

		if !result.HasMore || result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	return all, nil
}
