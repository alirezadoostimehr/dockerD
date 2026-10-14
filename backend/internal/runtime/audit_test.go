package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func auditFixture(t *testing.T) (*Manager, string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "audit.log")
	identity := filepath.Join(root, "identity")
	if e := os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(identity, []byte("100"), 0600)
	bin := filepath.Join(root, "lima", "bin")
	os.MkdirAll(bin, 0700)
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$5\" = stat ]; then printf '%%s %%s\\n' \"$(cat %s)\" \"$(wc -c < %s | tr -d ' ')\"; else tail -c \"$7\" %s; fi\n", quote(identity), quote(path), quote(path))
	if e := os.WriteFile(filepath.Join(bin, "limactl"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return New(Paths{Bundle: root}), path, identity
}
func TestAuditSessionLifecycleAndPartialRecords(t *testing.T) {
	m, path, _ := auditFixture(t)
	ctx := context.Background()
	start := `{"auditID":"one","stage":"RequestReceived","objectRef":{"resource":"pods","subresource":"portforward","namespace":"default","name":"web"}}` + "\n"
	os.WriteFile(path, []byte(start), 0600)
	if e := m.audit(ctx); e != nil {
		t.Fatal(e)
	}
	if len(m.sessions) != 1 {
		t.Fatal("silent open session was not retained")
	}
	if e := m.audit(ctx); e != nil || len(m.sessions) != 1 {
		t.Fatalf("no new events must not close session: %v", e)
	}
	partial := `{"auditID":"one","stage":"ResponseComplete"`
	os.WriteFile(path, []byte(start+partial), 0600)
	for i := 0; i < 2; i++ {
		if e := m.audit(ctx); e == nil {
			t.Fatal("partial event must remain unknown across polls")
		}
	}
	complete := partial + `,"objectRef":{"resource":"pods","subresource":"portforward"}}` + "\n"
	os.WriteFile(path, []byte(start+complete), 0600)
	if e := m.audit(ctx); e != nil {
		t.Fatal(e)
	}
	if len(m.sessions) != 0 {
		t.Fatal("completed session was not released")
	}
}
func TestAuditCorruptionAndRotationStayUnknown(t *testing.T) {
	for _, scenario := range []string{"corruption", "rotation"} {
		t.Run(scenario, func(t *testing.T) {
			m, path, identity := auditFixture(t)
			ctx := context.Background()
			if e := m.audit(ctx); e != nil {
				t.Fatal(e)
			}
			if scenario == "corruption" {
				os.WriteFile(path, []byte("not-json\n"), 0600)
			} else {
				os.WriteFile(identity, []byte("101"), 0600)
			}
			if e := m.audit(ctx); e == nil {
				t.Fatal("lost history accepted")
			}
			os.WriteFile(path, nil, 0600)
			os.WriteFile(identity, []byte("100"), 0600)
			if e := m.audit(ctx); e == nil {
				t.Fatal("ambiguity must persist until explicit reset")
			}
			m.ResetAudit()
			if e := m.audit(ctx); e != nil {
				t.Fatal(e)
			}
		})
	}
}
