package dashboard

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/darshm/reverseproxy/internal/proxy"
)

func TestViewFitsTerminalAndEscapesRequestText(t *testing.T) {
	m := model{
		config:  Config{Listen: "127.0.0.1:8080", Backend: "http://127.0.0.1:9000", Timeout: time.Second, RateLimit: 12},
		started: time.Now(), snapshot: proxy.Snapshot{Circuit: "OPEN", RetryAfter: time.Second},
		events: []Event{{Time: time.Now(), Method: "GET", Path: "/orders\x1b[2J\n", Status: 503, RequestID: "req-12"}},
	}
	for _, size := range [][2]int{{76, 28}, {100, 32}, {120, 36}, {160, 48}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			frame := view(m, size[0], size[1])
			if strings.Contains(frame, "\x1b[2J") {
				t.Fatal("request text injected a terminal control sequence")
			}
			lines := strings.Split(frame, "\r\n")
			if len(lines) > size[1] {
				t.Fatalf("%d lines exceed %d terminal rows", len(lines), size[1])
			}
			for _, line := range lines {
				plain := strings.ReplaceAll(ansi.ReplaceAllString(line, ""), "\x1b[K", "")
				if utf8.RuneCountInString(plain) >= size[0] {
					t.Errorf("line exceeds terminal columns: %q", plain)
				}
			}
			if !strings.Contains(frame, "503") || !strings.Contains(frame, "OPEN") {
				t.Fatal("request failure or circuit state missing")
			}
		})
	}
}

func TestErrorFilterIncludesAbortedSuccessfulStatus(t *testing.T) {
	m := model{
		started: time.Now(), errorsOnly: true,
		snapshot: proxy.Snapshot{Circuit: "CLOSED"},
		events: []Event{
			{Method: "GET", Path: "/healthy", Status: 200},
			{Method: "GET", Path: "/truncated", Status: 200, Aborted: true},
			{Method: "GET", Path: "/throttled", Status: 429},
		},
	}
	frame := view(m, 120, 36)
	if strings.Contains(frame, "/healthy") || !strings.Contains(frame, "/truncated") || !strings.Contains(frame, "/throttled") {
		t.Fatal("error filter does not match observable failed requests")
	}
}

func TestStoreKeepsOnlyRecentCompletedRequests(t *testing.T) {
	store := &Store{}
	for i := 0; i < 250; i++ {
		if _, err := fmt.Fprintf(store, "{\"msg\":\"request completed\",\"path\":\"/%d\",\"status\":200}\n", i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Write([]byte("{\"msg\":\"Relay listening\"}")); err != nil {
		t.Fatal(err)
	}
	events := store.Events()
	if len(events) != 200 || events[0].Path != "/50" || events[199].Path != "/249" {
		t.Fatal("store did not retain the last 200 completed requests")
	}
	events[0].Path = "/changed"
	if store.Events()[0].Path != "/50" {
		t.Fatal("caller can mutate stored request history")
	}
}
