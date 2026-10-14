package api

import (
	engine "dockerd.local/backend/internal/runtime"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStoppedServiceRejectsActionsAndExposesStatus(t *testing.T) {
	m := engine.New(engine.Paths{Home: t.TempDir(), Bundle: t.TempDir()})
	s, e := New(m)
	if e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("POST", "/v1/action", strings.NewReader(`{"action":"pull","value":"nginx:alpine"}`))
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "start the engine first") {
		t.Fatalf("unsafe action: %s", response.Body)
	}
	response = httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/v1/status", nil))
	var status Status
	if e = json.Unmarshal(response.Body.Bytes(), &status); e != nil {
		t.Fatal(e)
	}
	if status.State != "stopped" || status.Settings.IdleMinutes != 10 {
		t.Fatal(status)
	}
}
func TestRuntimeSettingsCannotChangeWhileRunning(t *testing.T) {
	m := engine.New(engine.Paths{Home: t.TempDir(), Bundle: t.TempDir()})
	s, e := New(m)
	if e != nil {
		t.Fatal(e)
	}
	s.state = "active"
	cfg := s.settings
	cfg.Kubernetes = !cfg.Kubernetes
	b, _ := json.Marshal(cfg)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest("PUT", "/v1/settings", strings.NewReader(string(b))))
	if response.Code != 400 || !strings.Contains(response.Body.String(), "stop the engine") {
		t.Fatal(response.Body)
	}
}
