package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadResumesAndVerifies(t *testing.T) {
	payload := []byte("a verified guest image")
	sum := sha256.Sum256(payload)
	var resumed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if start == 5 {
			resumed = true
		}
		if start >= len(payload) {
			w.WriteHeader(416)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
		w.WriteHeader(206)
		w.Write(payload[start:])
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "image")
	os.WriteFile(path+".part", payload[:5], 0600)
	if e := FetchVerified(context.Background(), Artifact{URL: server.URL, SHA256: hex.EncodeToString(sum[:])}, path, func(string) {}); e != nil {
		t.Fatal(e)
	}
	if !resumed || !checksum(path, hex.EncodeToString(sum[:])) {
		t.Fatal("resume or verification failed")
	}
}
func TestDownloadRejectsCorruptPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("corrupted")) }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "image")
	e := FetchVerified(ctx, Artifact{URL: server.URL, SHA256: "invalid"}, path, func(s string) {
		if s == "Download interrupted; resuming verified image download…" {
			cancel()
		}
	})
	if e == nil {
		t.Fatal("accepted invalid checksum")
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("published corrupt image")
	}
}
