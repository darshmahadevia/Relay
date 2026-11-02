package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/darshm/reverseproxy/internal/dashboard"
	"github.com/darshm/reverseproxy/internal/proxy"
	"github.com/darshm/reverseproxy/internal/showcase"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", ":8080", "address for Relay to listen on")
	backend := flag.String("backend", "http://localhost:9000", "upstream backend URL")
	rateLimit := flag.Int("rate-limit", 0, "maximum requests per second (0 disables limiting)")
	upstreamTimeout := flag.Duration("upstream-timeout", 15*time.Second, "maximum duration for an upstream request")
	ui := flag.Bool("ui", false, "show the live terminal dashboard instead of JSON logs")
	demo := flag.Bool("demo", false, "start the dashboard with a controlled localhost backend and real HTTP traffic")
	logPath := flag.String("log-file", "", "also append JSON logs to this file")
	flag.Parse()
	*ui = *ui || *demo
	if *ui && !dashboard.IsTerminal() {
		return fmt.Errorf("the dashboard needs an interactive terminal; omit -ui and -demo for JSON logs")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var scenario *showcase.Demo
	if *demo {
		var err error
		scenario, err = showcase.New()
		if err != nil {
			return err
		}
		defer scenario.Close()
		defer stop()
		*backend, *rateLimit, *upstreamTimeout = scenario.URL, 12, 2*time.Second
		if *listen == ":8080" {
			*listen = "127.0.0.1:8080"
		}
		if host, _, err := net.SplitHostPort(*listen); err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
			return fmt.Errorf("demo mode requires a loopback -listen address")
		}
	}
	store := &dashboard.Store{}
	var output io.Writer = os.Stdout
	if *ui {
		output = store
	}
	if *logPath != "" {
		file, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		output = io.MultiWriter(output, file)
	}
	logger := slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo}))
	p, err := proxy.New(proxy.Config{Backend: *backend, Logger: logger, RateLimitPerSecond: *rateLimit, UpstreamTimeout: *upstreamTimeout})
	if err != nil {
		return err
	}
	defer p.CloseIdleConnections()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: listener.Addr().String(), Handler: p,
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
		stop()
	}()
	logger.Info("Relay listening", "address", server.Addr, "backend", *backend)
	var uiError error
	if *ui {
		config := dashboard.Config{Listen: server.Addr, Backend: *backend, Timeout: *upstreamTimeout, RateLimit: *rateLimit, Store: store, Snapshot: p.Snapshot}
		if scenario != nil {
			config.Demo, config.Control = scenario.Stage, scenario.Control
			host, port, _ := net.SplitHostPort(server.Addr)
			if host == "localhost" {
				host = "127.0.0.1"
			}
			scenario.Run(ctx, "http://"+net.JoinHostPort(host, port))
		}
		uiError = dashboard.Run(ctx, config)
	} else {
		<-ctx.Done()
	}
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownError := server.Shutdown(shutdownCtx)
	if err := <-serveDone; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return errors.Join(uiError, shutdownError)
}
