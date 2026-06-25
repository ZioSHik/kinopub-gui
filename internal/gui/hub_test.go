package gui

import "testing"

func TestHubSubscribeBroadcastUnsubscribe(t *testing.T) {
	h := newHub()
	if h.subscriberCount() != 0 {
		t.Fatalf("fresh hub should have 0 subscribers, got %d", h.subscriberCount())
	}
	a := h.subscribe()
	b := h.subscribe()
	if h.subscriberCount() != 2 {
		t.Fatalf("want 2 subscribers, got %d", h.subscriberCount())
	}

	h.broadcast(Event{Type: "x", Data: 1})
	for _, ch := range []chan Event{a, b} {
		select {
		case ev := <-ch:
			if ev.Type != "x" {
				t.Errorf("received %q, want x", ev.Type)
			}
		default:
			t.Error("subscriber did not receive the broadcast")
		}
	}

	h.unsubscribe(a)
	if h.subscriberCount() != 1 {
		t.Fatalf("after unsubscribe want 1, got %d", h.subscriberCount())
	}
	// Unsubscribing closes the channel.
	if _, ok := <-a; ok {
		t.Error("unsubscribed channel should be closed")
	}
	// Double unsubscribe is safe.
	h.unsubscribe(a)
}

func TestHubBroadcastDropsWhenFull(t *testing.T) {
	h := newHub()
	ch := h.subscribe()
	defer h.unsubscribe(ch)
	// Fill the 64-slot buffer, then one more must be dropped (not block).
	for i := 0; i < 70; i++ {
		h.broadcast(Event{Type: "x"})
	}
	// Drain: at most the buffer capacity (64) should be present.
	n := 0
	for {
		select {
		case <-ch:
			n++
			continue
		default:
		}
		break
	}
	if n > 64 {
		t.Errorf("received %d events, buffer cap is 64 — overflow should be dropped", n)
	}
	if n == 0 {
		t.Error("expected at least some buffered events")
	}
}
