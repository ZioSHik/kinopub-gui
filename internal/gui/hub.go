package gui

import "sync"

// Event is the envelope broadcast to all connected SSE clients.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Hub is a minimal Server-Sent-Events fan-out. Each subscriber owns a buffered
// channel; slow consumers drop events (the periodic snapshot reconciles state)
// rather than blocking the producers.
type Hub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

func newHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

func (h *Hub) subscribe() chan Event {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan Event) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *Hub) broadcast(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// Subscriber is behind; drop this event. The next full snapshot
			// (job events carry the complete job state) will catch it up.
		}
	}
}

func (h *Hub) subscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
