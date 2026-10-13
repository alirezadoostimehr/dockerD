package gateway

import (
	"dockerd.local/backend/internal/idle"
	"net/http/httptest"
	"testing"
)

func TestGateRaces(t *testing.T) {
	x := NewTracker()
	x.Gate(false)
	_, generation := x.Snapshot()
	done, ok := x.Admit(idle.Blocker{Name: "exec"})
	if !ok {
		t.Fatal("admit")
	}
	if x.TryGate(generation) {
		t.Fatal("must not stop during exec")
	}
	done()
	if x.TryGate(generation) {
		t.Fatal("completed activity must invalidate old observation")
	}
	_, generation = x.Snapshot()
	if !x.TryGate(generation) {
		t.Fatal("idle should gate")
	}
	if _, ok = x.Admit(idle.Blocker{}); ok {
		t.Fatal("gate allowed work")
	}
}
func TestActivityClassification(t *testing.T) {
	for _, v := range []struct {
		method, path, upgrade string
		want                  bool
	}{{"GET", "/v1.50/containers/json", "", false}, {"GET", "/api/v1/pods?watch=true", "", false}, {"GET", "/api/v1/pods/p/portforward", "websocket", true}, {"POST", "/build", "", true}, {"GET", "/containers/c/attach", "tcp", true}} {
		r := httptest.NewRequest(v.method, v.path, nil)
		r.Header.Set("Upgrade", v.upgrade)
		if IsActivity(r) != v.want {
			t.Fatal(v)
		}
	}
}
