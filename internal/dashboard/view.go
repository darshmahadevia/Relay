package dashboard

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ink   = "dce6e8"
	muted = "91a4b0"
	lime  = "b7f46d"
	blue  = "8bd5e8"
	amber = "f4bf75"
	red   = "ff8a80"
	rule  = "34434b"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func paint(text, hex string) string {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return text
	}
	var r, g, b int
	_, _ = fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[38;2;220;230;232m", r, g, b, text)
}

func clean(text string, width int) string {
	var out strings.Builder
	for _, r := range text {
		if r < 32 || r > 126 {
			r = '?'
		}
		out.WriteRune(r)
	}
	text = out.String()
	if len(text) > width {
		return text[:max(0, width-1)] + "~"
	}
	return text
}

func statusColor(status int) string {
	if status >= 500 {
		return red
	}
	if status >= 400 {
		return amber
	}
	return lime
}

func view(m model, width, height int) string {
	if width < 76 || height < 28 {
		message := "Relay needs at least 76 columns x 28 rows. Resize the terminal. [q] quit"
		return clean(message, max(1, width-1)) + "\x1b[0J"
	}
	w := min(width-5, 140)
	var lines []string
	add := func(line string) { lines = append(lines, "  "+line) }
	split := func(left, right string) string {
		length := utf8.RuneCountInString(ansi.ReplaceAllString(left+right, ""))
		return left + strings.Repeat(" ", max(1, w-length)) + right
	}
	line := paint(strings.Repeat("─", w), rule)
	stateColor := lime
	if m.snapshot.Circuit == "OPEN" {
		stateColor = red
	} else if m.snapshot.Circuit == "HALF-OPEN" {
		stateColor = amber
	}
	mode := "LIVE"
	if m.config.Demo != nil {
		mode = "LOCAL DEMO"
	}
	if m.paused {
		mode = "DISPLAY FROZEN"
	}
	uptime := time.Since(m.started).Truncate(time.Second)
	add("")
	add(split(paint("relay", lime)+paint("  /  HTTP reverse proxy", muted), paint(fmt.Sprintf("%s   %02d:%02d", mode, int(uptime.Minutes()), int(uptime.Seconds())%60), muted)))
	add(line)
	add("")
	add(split(paint("CLIENT  ──────>  RELAY  ──────>  UPSTREAM", blue), "CIRCUIT  "+paint(m.snapshot.Circuit, stateColor)))
	add(paint(clean(m.config.Listen, 24)+"  ->  "+clean(m.config.Backend, w-32), muted))
	limit := fmt.Sprintf("%d req/s", m.config.RateLimit)
	if m.config.RateLimit == 0 {
		limit = "off"
	}
	add(split(paint(fmt.Sprintf("Deadline %s   /   Limit %s   /   Connections pooled", m.config.Timeout, limit), muted), paint(circuitDetail(m), stateColor)))
	add("")
	stage := "Waiting for requests. Point a client at the listening address."
	if m.config.Demo != nil {
		stage = m.stage
	} else if m.snapshot.Requests > 0 {
		stage = "Observing live traffic. Controls affect this display only."
	}
	add(paint(clean(stage, w), ink))
	add("")
	add(split(paint("TRAFFIC", ink), paint(fmt.Sprintf("%5.1f req/s    last 60 seconds", m.rate), lime)))
	maxRate := 10.0
	for _, p := range m.history {
		maxRate = max(maxRate, math.Ceil(p.requests/10)*10)
	}
	graphWidth := min(w-8, 60)
	history := m.history
	if len(history) > graphWidth {
		history = history[len(history)-graphWidth:]
	}
	for row := 3; row >= 0; row-- {
		var bars strings.Builder
		bars.WriteString(strings.Repeat(" ", graphWidth-len(history)))
		for _, p := range history {
			level := min(8, max(0, int(math.Round((p.requests/maxRate*4-float64(row))*8))))
			color := lime
			if p.errors > 0 {
				color = amber
			}
			bars.WriteString(paint(string([]rune(" ▁▂▃▄▅▆▇█")[level]), color))
		}
		label := "     "
		if row == 3 {
			label = fmt.Sprintf("%4.0f ", maxRate)
		} else if row == 0 {
			label = "   0 "
		}
		graph := paint(label+"│", rule) + bars.String()
		if w >= 100 {
			details := []string{
				fmt.Sprintf("In flight          %d", m.snapshot.InFlight),
				fmt.Sprintf("Aborted transfers  %d", m.snapshot.Aborted),
				fmt.Sprintf("Circuit rejects    %d", m.snapshot.CircuitRejected),
				fmt.Sprintf("Upstream failures  %d", m.snapshot.UpstreamFailures),
			}
			graph = split(graph, paint(details[row], muted))
		}
		add(graph)
	}
	add(paint("     └"+strings.Repeat("─", graphWidth), rule) + paint(" now", muted))
	add(split(paint("     green: traffic   amber: 5xx or 429 in sample", muted), paint("1 sample / second", muted)))
	add("")
	var durations []int64
	for _, event := range m.events {
		durations = append(durations, event.DurationMS)
	}
	slices.Sort(durations)
	p95 := "--"
	if len(durations) > 0 {
		p95 = fmt.Sprintf("%d ms", durations[(len(durations)-1)*95/100])
	}
	if w >= 100 {
		add(paint(fmt.Sprintf("REQUESTS %-9d IN FLIGHT %-5d P95 %-9s ERRORS %-7d LIMITED %d", m.snapshot.Requests, m.snapshot.InFlight, p95, m.snapshot.Errors, m.snapshot.RateLimited), ink))
		add(paint("P95: last 200 completed requests   /   Errors: 5xx and aborted transfers", muted))
	} else {
		add(paint(fmt.Sprintf("REQUESTS %d   IN FLIGHT %d   P95 %s", m.snapshot.Requests, m.snapshot.InFlight, p95), ink))
		add(paint(fmt.Sprintf("ERRORS %d   LIMITED %d   CIRCUIT REJECTS %d", m.snapshot.Errors, m.snapshot.RateLimited, m.snapshot.CircuitRejected), muted))
	}
	add(line)
	filter := "ALL REQUESTS"
	if m.errorsOnly {
		filter = "ERRORS ONLY"
	}
	add(split(paint("RECENT REQUESTS", ink), paint(filter, muted)))
	pathWidth := w - 48
	add(paint(fmt.Sprintf("%-10s %-7s %-*s %-7s %8s  %s", "TIME", "METHOD", pathWidth, "PATH", "STATUS", "LATENCY", "REQUEST ID"), muted))
	available := height - len(lines) - 3
	count := 0
	for i := len(m.events) - 1; i >= 0 && count < available; i-- {
		e := m.events[i]
		if m.errorsOnly && e.Status < 400 && !e.Aborted {
			continue
		}
		latency := fmt.Sprintf("%d ms", e.DurationMS)
		if e.DurationMS == 0 {
			latency = "<1 ms"
		}
		status := fmt.Sprintf("%d", e.Status)
		color := statusColor(e.Status)
		if e.Aborted {
			status += "!"
			color = red
		}
		add(paint(e.Time.Format("15:04:05"), muted) + "  " + paint(fmt.Sprintf("%-7s", clean(e.Method, 7)), blue) + fmt.Sprintf("%-*s ", pathWidth, clean(e.Path, pathWidth)) + paint(fmt.Sprintf("%-7s", status), color) + fmt.Sprintf("%8s  ", latency) + paint(clean(e.RequestID, 12), muted))
		count++
	}
	if count == 0 && available > 0 {
		add(paint("No requests in this view yet.", muted))
	}
	for len(lines) < height-3 {
		add("")
	}
	add(line)
	help := "[space] freeze   [e] errors   [q] drain & quit"
	if m.config.Demo != nil {
		help = "[f] fail  [r] recover  [b] burst  [a] auto   [space] freeze  [e] errors  [q] quit"
		if w < 90 {
			help = "[q] quit  [space] freeze  [e] errors  [f/r/b/a] demo controls"
		}
	}
	add(paint(clean(help, w), muted))
	for i := range lines {
		lines[i] = fit(lines[i], width-1) + "\x1b[K"
	}
	background := "\x1b[48;2;11;16;21m\x1b[38;2;220;230;232m"
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		background = ""
	}
	return background + strings.Join(lines, "\r\n")
}

func fit(text string, width int) string {
	var result strings.Builder
	for len(text) > 0 && width > 0 {
		if strings.HasPrefix(text, "\x1b[") {
			end := strings.IndexByte(text, 'm')
			if end >= 0 {
				result.WriteString(text[:end+1])
				text = text[end+1:]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text)
		result.WriteRune(r)
		text = text[size:]
		width--
	}
	return result.String()
}

func circuitDetail(m model) string {
	switch m.snapshot.Circuit {
	case "OPEN":
		if m.snapshot.RetryAfter > 0 {
			return fmt.Sprintf("Probe in %.1fs", m.snapshot.RetryAfter.Seconds())
		}
		return "Probe ready"
	case "HALF-OPEN":
		return "One recovery probe in flight"
	default:
		return "Accepting upstream requests"
	}
}
