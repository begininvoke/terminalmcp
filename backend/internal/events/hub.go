package events

import "sync"

// Hub fans out per-engagement events to all subscribed WebSocket clients.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan []byte]struct{} // engagementID -> set of subscriber channels
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan []byte]struct{})}
}

// Subscribe returns a channel of raw JSON event payloads for an engagement,
// plus an unsubscribe function.
func (h *Hub) Subscribe(engagementID string) (<-chan []byte, func()) {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	if h.subs[engagementID] == nil {
		h.subs[engagementID] = make(map[chan []byte]struct{})
	}
	h.subs[engagementID][ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		if set := h.subs[engagementID]; set != nil {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, engagementID)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
}

// Publish delivers a payload to every subscriber of an engagement.
// Slow subscribers are skipped rather than blocking the agent loop.
func (h *Hub) Publish(engagementID string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[engagementID] {
		select {
		case ch <- payload:
		default:
		}
	}
}
