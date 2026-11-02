package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/darshm/reverseproxy/internal/proxy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var healthy atomic.Bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			<-r.Context().Done()
		case "/unstable":
			if !healthy.Load() {
				http.Error(w, "backend failed", http.StatusBadGateway)
				return
			}
			_, _ = io.WriteString(w, "backend recovered\n")
		default:
			fmt.Fprintf(w, "%s %s\n", r.Method, r.URL.RequestURI())
		}
	}))
	defer backend.Close()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	p, err := proxy.New(proxy.Config{Backend: backend.URL, Logger: logger, UpstreamTimeout: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	defer p.CloseIdleConnections()
	server := httptest.NewServer(p)
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	fmt.Printf("Backend started at %s\nProxy started at %s\n", backend.URL, server.URL)
	request := func(baseURL, path string, want int) (http.Header, error) {
		response, err := client.Get(baseURL + path)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			return nil, err
		}
		fmt.Printf("GET %s -> %d request_id=%s\n%s", path, response.StatusCode, response.Header.Get("X-Request-ID"), body)
		if path == "/healthz" {
			fmt.Println()
		}
		if response.StatusCode != want {
			return nil, fmt.Errorf("%s returned %d, want %d", path, response.StatusCode, want)
		}
		if response.Header.Get("X-Request-ID") == "" {
			return nil, fmt.Errorf("%s has no request ID", path)
		}
		return response.Header, nil
	}
	for _, scenario := range []struct {
		path   string
		status int
	}{
		{"/echo?message=hello", http.StatusOK},
		{"/healthz", http.StatusOK},
		{"/slow", http.StatusBadGateway},
		{"/echo", http.StatusOK},
		{"/unstable", http.StatusBadGateway},
		{"/unstable", http.StatusBadGateway},
		{"/unstable", http.StatusBadGateway},
		{"/unstable", http.StatusBadGateway},
		{"/unstable", http.StatusBadGateway},
	} {
		if _, err := request(server.URL, scenario.path, scenario.status); err != nil {
			return err
		}
	}
	header, err := request(server.URL, "/unstable", http.StatusServiceUnavailable)
	if err != nil {
		return err
	}
	healthy.Store(true)
	wait, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || wait < 1 || wait > 10 {
		return fmt.Errorf("invalid circuit retry delay %q", header.Get("Retry-After"))
	}
	fmt.Printf("Waiting %d seconds for the recovery probe\n", wait)
	time.Sleep(time.Duration(wait) * time.Second)
	if _, err := request(server.URL, "/unstable", http.StatusOK); err != nil {
		return err
	}
	if _, err := request(server.URL, "/metrics", http.StatusOK); err != nil {
		return err
	}
	limited, err := proxy.New(proxy.Config{Backend: backend.URL, Logger: logger, RateLimitPerSecond: 1})
	if err != nil {
		return err
	}
	defer limited.CloseIdleConnections()
	limitedServer := httptest.NewServer(limited)
	defer limitedServer.Close()
	fmt.Println("Separate proxy with a one-request-per-second limit")
	if _, err := request(limitedServer.URL, "/echo", http.StatusOK); err != nil {
		return err
	}
	if _, err := request(limitedServer.URL, "/echo", http.StatusTooManyRequests); err != nil {
		return err
	}
	fmt.Println("PASS forwarding, liveness, timeout, circuit opening and recovery, metrics, rate limiting")
	return nil
}
