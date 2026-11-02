package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestShutdownDrainsActiveRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires SIGTERM")
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "completed")
	}))
	defer backend.Close()
	defer unblock()

	binary := filepath.Join(t.TempDir(), "proxy")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build proxy: %v\n%s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	logFile, err := os.CreateTemp(t.TempDir(), "proxy.log")
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	process := exec.Command(binary, "-listen", address, "-backend", backend.URL)
	process.Stdout, process.Stderr = logFile, logFile
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- process.Wait() }()
	defer func() {
		_ = process.Process.Kill()
	}()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	baseURL := "http://" + address
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			ready = response.StatusCode == http.StatusOK
			if ready {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		logs, _ := os.ReadFile(logFile.Name())
		t.Fatalf("proxy did not become ready\n%s", logs)
	}
	result := make(chan error, 1)
	go func() {
		response, err := client.Get(baseURL + "/work")
		if err == nil {
			var body []byte
			body, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err == nil && (response.StatusCode != http.StatusOK || string(body) != "completed") {
				err = io.ErrUnexpectedEOF
			}
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("backend did not receive active request")
	}
	if err := process.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		t.Fatalf("proxy exited before active request completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	if err := <-result; err != nil {
		t.Fatalf("active request did not complete during shutdown: %v", err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not exit after draining")
	}
}
