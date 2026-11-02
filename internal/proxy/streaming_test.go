package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTruncatedResponsesCountAsFailuresAndOpenCircuit(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", "100000")
		_, _ = io.WriteString(w, strings.Repeat("x", 8192))
	}))
	defer backend.Close()
	var logs bytes.Buffer
	p, err := New(Config{Backend: backend.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer p.CloseIdleConnections()
	server := httptest.NewUnstartedServer(p)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Start()
	defer server.Close()
	client := server.Client()
	for i := 0; i < defaultCircuitFailureThreshold; i++ {
		response, err := client.Get(server.URL + "/truncated")
		if err != nil {
			t.Fatalf("response headers did not reach the client: %v", err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("partial response status = %d, want 200", response.StatusCode)
		}
		_, err = io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err == nil {
			t.Fatal("truncated upstream response appeared complete")
		}
	}
	response, err := client.Get(server.URL + "/truncated")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status after five truncated responses = %d, want 503", response.StatusCode)
	}
	if calls.Load() != 5 {
		t.Errorf("backend calls = %d, want 5", calls.Load())
	}
	response, err = client.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []string{
		"proxy_requests_total 6\n",
		"proxy_errors_total 6\n",
		"proxy_upstream_failures_total 5\n",
		"proxy_aborted_total 5\n",
		"proxy_in_flight 0\n",
	} {
		if !strings.Contains(string(data), metric) {
			t.Errorf("metrics missing %q\n%s", metric, data)
		}
	}
	decoder := json.NewDecoder(&logs)
	aborted := 0
	for decoder.More() {
		var entry struct {
			Message string `json:"msg"`
			Aborted bool   `json:"aborted"`
			Status  int    `json:"status"`
		}
		if err := decoder.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		if entry.Message == "request completed" && entry.Aborted {
			if entry.Status != http.StatusOK {
				t.Errorf("aborted response log status = %d, want 200", entry.Status)
			}
			aborted++
		}
	}
	if aborted != 5 {
		t.Errorf("aborted request log entries = %d, want 5", aborted)
	}
}
