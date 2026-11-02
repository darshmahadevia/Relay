package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkHTTP(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1024)
	var backendCalls atomic.Uint64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls.Add(1)
		_, _ = w.Write(payload)
	}))
	defer backend.Close()
	logFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	defer logFile.Close()
	p, err := New(Config{Backend: backend.URL, Logger: slog.New(slog.NewJSONHandler(logFile, nil))})
	if err != nil {
		b.Fatal(err)
	}
	defer p.CloseIdleConnections()
	server := httptest.NewServer(p)
	defer server.Close()

	for _, target := range []struct{ name, url string }{{"direct", backend.URL}, {"proxy", server.URL}} {
		b.Run(target.name, func(b *testing.B) {
			transport := &http.Transport{MaxIdleConns: 100, MaxIdleConnsPerHost: 100, MaxConnsPerHost: 100}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			check := func() bool {
				response, err := client.Get(target.url + "/payload")
				if err != nil {
					return false
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				return err == nil && response.StatusCode == http.StatusOK && bytes.Equal(body, payload)
			}
			for i := 0; i < 20; i++ {
				if !check() {
					b.Fatal("warmup request failed")
				}
			}
			var failures atomic.Uint64
			var mu sync.Mutex
			latencies := make([]int64, 0, b.N)
			before := backendCalls.Load()
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				var samples []int64
				for pb.Next() {
					started := time.Now()
					ok := check()
					samples = append(samples, time.Since(started).Nanoseconds())
					if !ok {
						failures.Add(1)
					}
				}
				mu.Lock()
				latencies = append(latencies, samples...)
				mu.Unlock()
			})
			b.StopTimer()
			calls := backendCalls.Load() - before
			b.ReportMetric(float64(failures.Load()), "errors")
			b.ReportMetric(float64(calls), "backend-requests")
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/s")
			if failures.Load() != 0 || calls != uint64(b.N) {
				b.Fatalf("errors = %d, backend requests = %d, want %d successful requests", failures.Load(), calls, b.N)
			}
			slices.Sort(latencies)
			b.ReportMetric(float64(latencies[(len(latencies)-1)*50/100])/1e6, "p50-ms")
			b.ReportMetric(float64(latencies[(len(latencies)-1)*95/100])/1e6, "p95-ms")
		})
	}
}
