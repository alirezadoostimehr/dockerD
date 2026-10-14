// Package idle contains the clock-driven shutdown policy. Unknown is never idle.
package idle

import "time"

type Blocker struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}
type Snapshot struct {
	At       time.Time
	Healthy  bool
	Blockers []Blocker
}
type Decision struct {
	State      string     `json:"state"`
	Blockers   []Blocker  `json:"blockers"`
	Healthy    bool       `json:"healthy"`
	IdleSince  *time.Time `json:"idleSince,omitempty"`
	Deadline   *time.Time `json:"deadline,omitempty"`
	ShouldStop bool       `json:"-"`
}
type Policy struct {
	Enabled                    bool
	Timeout, Countdown, MaxAge time.Duration
	since, last                time.Time
}

func (p *Policy) Reset() { p.since = time.Time{}; p.last = time.Time{} }
func (p *Policy) Evaluate(now time.Time, s Snapshot) Decision {
	d := Decision{State: "active", Healthy: s.Healthy, Blockers: append([]Blocker{}, s.Blockers...)}
	if p.MaxAge <= 0 {
		p.MaxAge = 20 * time.Second
	}
	// Strip the monotonic component: macOS monotonic time may pause during sleep.
	gap := !p.last.IsZero() && (now.Round(0).Sub(p.last.Round(0)) > p.MaxAge || now.Round(0).Before(p.last.Round(0)))
	p.last = now
	if !s.Healthy || s.At.IsZero() || now.Sub(s.At) > p.MaxAge || s.At.After(now) {
		d.Healthy = false
		d.Blockers = append(d.Blockers, Blocker{"monitor", "Activity monitoring unavailable", "Automatic shutdown is paused until activity can be verified."})
	}
	if !p.Enabled || len(d.Blockers) > 0 || gap {
		p.since = time.Time{}
		return d
	}
	if p.since.IsZero() {
		p.since = now
	}
	since := p.since
	d.IdleSince = &since
	deadline := since.Add(p.Timeout + p.Countdown)
	d.Deadline = &deadline
	if now.Sub(since) >= p.Timeout {
		d.State = "idle countdown"
	}
	d.ShouldStop = !now.Before(deadline)
	return d
}
