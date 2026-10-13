package idle

import (
	"testing"
	"time"
)

func TestCountdownAndActivity(t *testing.T) {
	p := Policy{Enabled: true, Timeout: time.Minute, Countdown: 10 * time.Second, MaxAge: 20 * time.Second}
	now := time.Unix(1000, 0)
	for i := 0; i <= 70; i += 10 {
		at := now.Add(time.Duration(i) * time.Second)
		d := p.Evaluate(at, Snapshot{At: at, Healthy: true})
		if d.ShouldStop != (i == 70) {
			t.Fatalf("at %d: %+v", i, d)
		}
		if i == 60 && d.State != "idle countdown" {
			t.Fatal(d)
		}
	}
	at := now.Add(71 * time.Second)
	d := p.Evaluate(at, Snapshot{At: at, Healthy: true, Blockers: []Blocker{{Kind: "exec", Name: "open shell"}}})
	if d.ShouldStop || d.Deadline != nil {
		t.Fatal("activity must cancel countdown")
	}
}
func TestUnknownStaleSleepAndDisabled(t *testing.T) {
	for _, scenario := range []string{"unknown", "stale", "sleep", "disabled", "clock"} {
		t.Run(scenario, func(t *testing.T) {
			p := Policy{Enabled: true, Timeout: time.Second, Countdown: time.Second, MaxAge: 10 * time.Second}
			now := time.Unix(100, 0)
			p.Evaluate(now, Snapshot{At: now, Healthy: true})
			at := now.Add(3 * time.Second)
			s := Snapshot{At: at, Healthy: true}
			switch scenario {
			case "unknown":
				s.Healthy = false
			case "stale":
				s.At = now.Add(-time.Minute)
			case "sleep":
				at = now.Add(time.Hour)
				s.At = at
			case "disabled":
				p.Enabled = false
			case "clock":
				at = now.Add(-time.Second)
				s.At = at
			}
			if d := p.Evaluate(at, s); d.ShouldStop || d.Deadline != nil {
				t.Fatalf("must reset: %+v", d)
			}
		})
	}
}
