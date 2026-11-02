package proxy

import "time"

type Snapshot struct {
	Requests, Errors, Aborted, RateLimited, CircuitRejected, UpstreamFailures uint64
	InFlight                                                                  int64
	Circuit                                                                   string
	RetryAfter                                                                time.Duration
}

func (p *Proxy) Snapshot() Snapshot {
	s := Snapshot{
		Requests: p.metrics.requests.Load(), Errors: p.metrics.errors.Load(),
		Aborted: p.metrics.aborted.Load(), RateLimited: p.metrics.rateLimited.Load(),
		CircuitRejected: p.metrics.circuitOpen.Load(), UpstreamFailures: p.metrics.upstreamFailures.Load(),
		InFlight: p.metrics.inFlight.Load(),
	}
	p.breaker.mu.Lock()
	defer p.breaker.mu.Unlock()
	s.Circuit = "CLOSED"
	switch p.breaker.state {
	case circuitOpen:
		s.Circuit = "OPEN"
		s.RetryAfter = max(0, p.breaker.cooldown-p.breaker.now().Sub(p.breaker.openedAt))
	case circuitHalfOpen:
		s.Circuit = "HALF-OPEN"
	}
	return s
}
