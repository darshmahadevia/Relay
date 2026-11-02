package dashboard

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/darshm/reverseproxy/internal/proxy"
	"golang.org/x/term"
)

type Config struct {
	Listen, Backend string
	Timeout         time.Duration
	RateLimit       int
	Store           *Store
	Snapshot        func() proxy.Snapshot
	Demo            func() string
	Control         func(byte)
}

type point struct{ requests, errors float64 }

type model struct {
	config     Config
	started    time.Time
	snapshot   proxy.Snapshot
	events     []Event
	history    []point
	rate       float64
	paused     bool
	errorsOnly bool
	stage      string
}

func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb"
}

func Run(ctx context.Context, config Config) error {
	if !IsTerminal() {
		return fmt.Errorf("the dashboard needs an interactive terminal; omit -ui for JSON logs")
	}
	previous, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), previous)
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?25h\x1b[?1049l")
	keys := make(chan byte, 16)
	go func() {
		buffer := make([]byte, 32)
		for {
			n, err := os.Stdin.Read(buffer)
			if err != nil {
				return
			}
			for _, key := range buffer[:n] {
				select {
				case keys <- key:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	m := model{config: config, started: time.Now()}
	var frozen model
	lastTime := time.Now()
	last := config.Snapshot()
	frames := time.NewTicker(250 * time.Millisecond)
	defer frames.Stop()
	for {
		m.snapshot, m.events = config.Snapshot(), config.Store.Events()
		if config.Demo != nil {
			m.stage = config.Demo()
		}
		now := time.Now()
		if elapsed := now.Sub(lastTime).Seconds(); elapsed >= 1 {
			m.rate = float64(m.snapshot.Requests-last.Requests) / elapsed
			m.history = append(m.history, point{m.rate, float64(m.snapshot.Errors-last.Errors+m.snapshot.RateLimited-last.RateLimited) / elapsed})
			if len(m.history) > 60 {
				m.history = m.history[len(m.history)-60:]
			}
			last, lastTime = m.snapshot, now
		}
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			return err
		}
		display := m
		if m.paused {
			display = frozen
			display.paused, display.errorsOnly = true, m.errorsOnly
		}
		if _, err := io.WriteString(os.Stdout, "\x1b[H"+view(display, width, height)); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case key := <-keys:
			switch key {
			case 'q', 'Q', 3:
				return nil
			case ' ':
				if !m.paused {
					frozen = m
					frozen.history = append([]point(nil), m.history...)
				}
				m.paused = !m.paused
			case 'e', 'E':
				m.errorsOnly = !m.errorsOnly
			default:
				if config.Control != nil {
					config.Control(key)
				}
			}
		case <-frames.C:
		}
	}
}
