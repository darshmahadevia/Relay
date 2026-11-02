package showcase

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type mode int64

const (
	automatic mode = iota
	outage
	recovery
	burst
)

type Demo struct {
	URL     string
	server  *http.Server
	started atomic.Int64
	mode    atomic.Int64
	wg      sync.WaitGroup
}

func New() (*Demo, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	d := &Demo{URL: "http://" + listener.Addr().String()}
	d.started.Store(time.Now().UnixNano())
	d.server = &http.Server{Handler: http.HandlerFunc(d.backend), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = d.server.Serve(listener) }()
	return d, nil
}

func (d *Demo) current() mode {
	if selected := mode(d.mode.Load()); selected != automatic {
		return selected
	}
	switch elapsed := time.Since(time.Unix(0, d.started.Load())); {
	case elapsed >= 6*time.Second && elapsed < 14*time.Second:
		return outage
	case elapsed >= 14*time.Second && elapsed < 20*time.Second:
		return recovery
	case elapsed >= 23*time.Second && elapsed < 30*time.Second:
		return burst
	default:
		return automatic
	}
}

func (d *Demo) Stage() string {
	switch d.current() {
	case outage:
		return "UPSTREAM OUTAGE  /  Five failures open the circuit. New requests fail fast."
	case recovery:
		return "RECOVERY  /  The backend is healthy. One probe tests it after the cooldown."
	case burst:
		return "TRAFFIC BURST  /  Requests exceed the 12 req/s quota. Excess traffic gets 429."
	default:
		return "HEALTHY TRAFFIC  /  Real HTTP requests flow through the proxy to a local backend."
	}
}

func (d *Demo) Control(key byte) {
	switch key {
	case 'f':
		d.mode.Store(int64(outage))
	case 'r':
		d.mode.Store(int64(recovery))
	case 'b':
		d.mode.Store(int64(burst))
	case 'a':
		d.started.Store(time.Now().UnixNano())
		d.mode.Store(int64(automatic))
	}
}

func (d *Demo) backend(w http.ResponseWriter, r *http.Request) {
	selected := d.current()
	if selected == outage {
		http.Error(w, "demo upstream unavailable", http.StatusBadGateway)
		return
	}
	delay := 40 * time.Millisecond
	if selected == recovery {
		delay = 800 * time.Millisecond
	}
	select {
	case <-r.Context().Done():
		return
	case <-time.After(delay):
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"method\":%q,\"path\":%q,\"ok\":true}\n", r.Method, r.URL.Path)
}

func (d *Demo) Run(ctx context.Context, target string) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		transport := &http.Transport{MaxIdleConnsPerHost: 8, MaxConnsPerHost: 8}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		paths := []string{"/v1/orders", "/v1/catalog", "/v1/events", "/v1/users"}
		semaphore := make(chan struct{}, 8)
		var requests sync.WaitGroup
		defer requests.Wait()
		var ticks int
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ticks++
				if d.current() != burst && ticks%5 != 0 {
					continue
				}
				select {
				case semaphore <- struct{}{}:
				default:
					continue
				}
				path := paths[(ticks/5)%len(paths)]
				requests.Add(1)
				go func() {
					defer requests.Done()
					defer func() { <-semaphore }()
					method := http.MethodGet
					if path == "/v1/events" {
						method = http.MethodPost
					}
					request, err := http.NewRequestWithContext(ctx, method, target+path, strings.NewReader("{}"))
					if err != nil {
						return
					}
					response, err := client.Do(request)
					if err == nil {
						_, _ = io.Copy(io.Discard, response.Body)
						_ = response.Body.Close()
					}
				}()
			}
		}
	}()
}

func (d *Demo) Close() {
	d.wg.Wait()
	_ = d.server.Close()
}
