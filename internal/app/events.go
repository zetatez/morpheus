package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type globalEvent struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
}

type eventBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan globalEvent]struct{}
}

func newEventBroadcaster() *eventBroadcaster {
	return &eventBroadcaster{
		subscribers: make(map[chan globalEvent]struct{}),
	}
}

func (b *eventBroadcaster) subscribe() chan globalEvent {
	ch := make(chan globalEvent, 64)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *eventBroadcaster) unsubscribe(ch chan globalEvent) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
}

func (s *APIServer) handleGlobalEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.eventBroadcaster.subscribe()
	defer s.eventBroadcaster.unsubscribe(ch)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	closeNotify := w.(http.CloseNotifier).CloseNotify()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-closeNotify:
			return
		case <-ticker.C:
			_, err := w.Write([]byte(": ping\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, err = w.Write([]byte("data: "))
			if err != nil {
				return
			}
			_, err = w.Write(data)
			if err != nil {
				return
			}
			_, err = w.Write([]byte("\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *APIServer) handleGlobalSyncEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.eventBroadcaster.subscribe()
	defer s.eventBroadcaster.unsubscribe(ch)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	closeNotify := w.(http.CloseNotifier).CloseNotify()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-closeNotify:
			return
		case <-ticker.C:
			_, err := w.Write([]byte(": ping\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, err = w.Write([]byte("data: "))
			if err != nil {
				return
			}
			_, err = w.Write(data)
			if err != nil {
				return
			}
			_, err = w.Write([]byte("\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
