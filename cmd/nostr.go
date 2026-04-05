package cmd

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// NostrEvent represents a Nostr event (kind 1111 for txinfo)
type NostrEvent struct {
	ID        string     `json:"id"`
	PubKey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// TxMetadata holds enrichment data for a blockchain transaction
type TxMetadata struct {
	TxHash       string            `json:"txHash"`
	Description  string            `json:"description"`
	Tags         map[string]string `json:"tags"` // project, category, etc.
	NostrEventID string            `json:"nostrEventId"`
	Author       string            `json:"author"`
	CreatedAt    int64             `json:"createdAt"`
}

// AddressMetadata holds enrichment data for a blockchain address
type AddressMetadata struct {
	Address      string            `json:"address"`
	Name         string            `json:"name"`
	About        string            `json:"about"`
	Picture      string            `json:"picture,omitempty"`
	Tags         map[string]string `json:"tags"`
	NostrEventID string            `json:"nostrEventId"`
	Author       string            `json:"author"`
	CreatedAt    int64             `json:"createdAt"`
}

// NostrMetadataCache is the structure saved to disk per chain
type NostrMetadataCache struct {
	FetchedAt    string                      `json:"fetchedAt"`
	ChainID      int                         `json:"chainId"`
	Transactions map[string]*TxMetadata      `json:"transactions"` // keyed by txHash (lowercase)
	Addresses    map[string]*AddressMetadata `json:"addresses"`    // keyed by address (lowercase)
}

var nostrRelays = []string{
	"wss://nostr.commonshub.brussels",
	"wss://nostr-pub.wellorder.net",
	"wss://nostr.swiss-enigma.ch",
	"wss://relay.nostr.band",
	"wss://relay.damus.io",
}

const (
	nostrConnectTimeout = 5 * time.Second
	nostrDataTimeout    = 10 * time.Second
	nostrBatchSize      = 50
)

// FetchNostrMetadata fetches NIP-73 / txinfo metadata for transactions and addresses.
// It queries all configured Nostr relays in parallel and deduplicates results.
func FetchNostrMetadata(chainID int, txHashes []string, addresses []string) (map[string]*TxMetadata, map[string]*AddressMetadata, error) {
	// Build URI list
	var uris []string
	for _, hash := range txHashes {
		uris = append(uris, fmt.Sprintf("ethereum:%d:tx:%s", chainID, strings.ToLower(hash)))
	}
	for _, addr := range addresses {
		uris = append(uris, fmt.Sprintf("ethereum:%d:address:%s", chainID, strings.ToLower(addr)))
	}

	if len(uris) == 0 {
		return map[string]*TxMetadata{}, map[string]*AddressMetadata{}, nil
	}

	// Batch URIs in groups of nostrBatchSize
	var batches [][]string
	for i := 0; i < len(uris); i += nostrBatchSize {
		end := i + nostrBatchSize
		if end > len(uris) {
			end = len(uris)
		}
		batches = append(batches, uris[i:end])
	}

	// Collect all events (deduplicated by ID)
	eventsMu := sync.Mutex{}
	allEvents := map[string]NostrEvent{} // key: event ID

	var wg sync.WaitGroup
	for _, relay := range nostrRelays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			events, err := fetchFromRelay(relayURL, batches)
			if err != nil {
				// Relay unavailable — silently skip
				return
			}
			eventsMu.Lock()
			defer eventsMu.Unlock()
			for id, ev := range events {
				// Keep most recent event if duplicate ID somehow appears
				if existing, ok := allEvents[id]; !ok || ev.CreatedAt > existing.CreatedAt {
					allEvents[id] = ev
				}
			}
		}(relay)
	}
	wg.Wait()

	// Parse events into TxMetadata and AddressMetadata
	txMeta := map[string]*TxMetadata{}
	addrMeta := map[string]*AddressMetadata{}

	for _, ev := range allEvents {
		// Find the "i" tag to determine what this event annotates
		for _, tag := range ev.Tags {
			if len(tag) < 2 || tag[0] != "i" {
				continue
			}
			uri := tag[1]

			// ethereum:<chainId>:tx:<hash>
			if isTxURI(uri, chainID) {
				hash := extractURIPart(uri, "tx")
				if hash == "" {
					continue
				}
				hash = strings.ToLower(hash)
				existing, ok := txMeta[hash]
				if !ok || ev.CreatedAt > existing.CreatedAt {
					txMeta[hash] = parseTxMetadata(hash, ev)
				}
			}

			// ethereum:<chainId>:address:<addr>
			if isAddressURI(uri, chainID) {
				addr := extractURIPart(uri, "address")
				if addr == "" {
					continue
				}
				addr = strings.ToLower(addr)
				existing, ok := addrMeta[addr]
				if !ok || ev.CreatedAt > existing.CreatedAt {
					addrMeta[addr] = parseAddressMetadata(addr, ev)
				}
			}
		}
	}

	return txMeta, addrMeta, nil
}

// fetchFromRelay connects to a single relay and fetches events for all batches.
func fetchFromRelay(relayURL string, batches [][]string) (map[string]NostrEvent, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: nostrConnectTimeout,
	}
	conn, _, err := dialer.Dial(relayURL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(nostrDataTimeout))

	events := map[string]NostrEvent{}

	for _, batch := range batches {
		subID := fmt.Sprintf("chb-%d", rand.Int63())

		filter := map[string]interface{}{
			"kinds": []int{1111},
			"#i":    batch,
		}
		req, _ := json.Marshal([]interface{}{"REQ", subID, filter})
		if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
			return events, err
		}

		// Read until EOSE or timeout
		for {
			conn.SetReadDeadline(time.Now().Add(nostrDataTimeout))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				// Timeout or connection closed — stop reading this batch
				break
			}

			var raw []json.RawMessage
			if err := json.Unmarshal(msg, &raw); err != nil || len(raw) < 2 {
				continue
			}

			var msgType string
			if err := json.Unmarshal(raw[0], &msgType); err != nil {
				continue
			}

			switch msgType {
			case "EVENT":
				if len(raw) < 3 {
					continue
				}
				var ev NostrEvent
				if err := json.Unmarshal(raw[2], &ev); err != nil {
					continue
				}
				if ev.Kind == 1111 {
					events[ev.ID] = ev
				}
			case "EOSE":
				// End of stored events — send CLOSE and move to next batch
				close_, _ := json.Marshal([]interface{}{"CLOSE", subID})
				conn.WriteMessage(websocket.TextMessage, close_)
				goto nextBatch
			}
		}
	nextBatch:
	}

	return events, nil
}

// isTxURI checks if a URI matches ethereum:<chainID>:tx:...
func isTxURI(uri string, chainID int) bool {
	prefix := fmt.Sprintf("ethereum:%d:tx:", chainID)
	return strings.HasPrefix(strings.ToLower(uri), strings.ToLower(prefix))
}

// isAddressURI checks if a URI matches ethereum:<chainID>:address:...
func isAddressURI(uri string, chainID int) bool {
	prefix := fmt.Sprintf("ethereum:%d:address:", chainID)
	return strings.HasPrefix(strings.ToLower(uri), strings.ToLower(prefix))
}

// extractURIPart extracts the hash/address after the kind segment.
// uri format: ethereum:<chainId>:<kind>:<value>
func extractURIPart(uri string, kind string) string {
	parts := strings.SplitN(uri, ":", 4)
	if len(parts) != 4 {
		return ""
	}
	if !strings.EqualFold(parts[2], kind) {
		return ""
	}
	return parts[3]
}

// parseTxMetadata builds a TxMetadata from a Nostr event.
func parseTxMetadata(txHash string, ev NostrEvent) *TxMetadata {
	m := &TxMetadata{
		TxHash:       txHash,
		Description:  ev.Content,
		Tags:         map[string]string{},
		NostrEventID: ev.ID,
		Author:       ev.PubKey,
		CreatedAt:    ev.CreatedAt,
	}
	skipTags := map[string]bool{"i": true, "k": true, "e": true, "p": true}
	for _, tag := range ev.Tags {
		if len(tag) < 2 || skipTags[tag[0]] {
			continue
		}
		m.Tags[tag[0]] = tag[1]
	}
	return m
}

// parseAddressMetadata builds an AddressMetadata from a Nostr event.
func parseAddressMetadata(addr string, ev NostrEvent) *AddressMetadata {
	m := &AddressMetadata{
		Address:      addr,
		Tags:         map[string]string{},
		NostrEventID: ev.ID,
		Author:       ev.PubKey,
		CreatedAt:    ev.CreatedAt,
		About:        ev.Content,
	}
	skipTags := map[string]bool{"i": true, "k": true, "e": true, "p": true}
	for _, tag := range ev.Tags {
		if len(tag) < 2 || skipTags[tag[0]] {
			continue
		}
		switch tag[0] {
		case "name":
			m.Name = tag[1]
		case "about":
			m.About = tag[1]
		case "picture":
			m.Picture = tag[1]
		default:
			m.Tags[tag[0]] = tag[1]
		}
	}
	return m
}
