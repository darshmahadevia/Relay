package dashboard

import (
	"encoding/json"
	"sync"
	"time"
)

type Event struct {
	Time       time.Time `json:"time"`
	Message    string    `json:"msg"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"duration_ms"`
	RequestID  string    `json:"request_id"`
	Aborted    bool      `json:"aborted"`
}

type Store struct {
	mu     sync.Mutex
	events []Event
}

func (s *Store) Write(data []byte) (int, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return 0, err
	}
	if event.Message == "request completed" {
		s.mu.Lock()
		if len(s.events) == 200 {
			copy(s.events, s.events[1:])
			s.events = s.events[:199]
		}
		s.events = append(s.events, event)
		s.mu.Unlock()
	}
	return len(data), nil
}

func (s *Store) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}
