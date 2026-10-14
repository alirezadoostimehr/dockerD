package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func checksum(path, want string) bool {
	f, e := os.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == want
}

// FetchVerified uses bounded range requests so a transient connection failure
// does not discard a several-hundred-megabyte VM image. Nothing is used before
// the complete file passes its pinned SHA-256 check.
func FetchVerified(ctx context.Context, artifact Artifact, path string, progress func(string)) error {
	if checksum(path, artifact.SHA256) {
		return nil
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	client := &http.Client{Timeout: 90 * time.Second}
	part := path + ".part"
	var last error
	for attempt := 0; attempt < 6; attempt++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		e := func() error {
			f, e := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0600)
			if e != nil {
				return e
			}
			defer f.Close()
			offset, e := f.Seek(0, io.SeekEnd)
			if e != nil {
				return e
			}
			for {
				progress(fmt.Sprintf("Downloading Linux · %d MiB received", offset/(1<<20)))
				req, e := http.NewRequestWithContext(ctx, "GET", artifact.URL, nil)
				if e != nil {
					return e
				}
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+(8<<20)-1))
				response, e := client.Do(req)
				if e != nil {
					return e
				}
				if response.StatusCode == 416 {
					response.Body.Close()
					break
				}
				if response.StatusCode != 200 && response.StatusCode != 206 {
					response.Body.Close()
					return fmt.Errorf("download HTTP %d", response.StatusCode)
				}
				if response.StatusCode == 200 {
					if e = f.Truncate(0); e != nil {
						response.Body.Close()
						return e
					}
					if _, e = f.Seek(0, io.SeekStart); e != nil {
						response.Body.Close()
						return e
					}
					offset = 0
				}
				if response.StatusCode == 206 {
					var start, end, total int64
					if _, e = fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); e != nil || start != offset {
						response.Body.Close()
						return fmt.Errorf("invalid download range")
					}
				}
				n, e := io.Copy(f, response.Body)
				response.Body.Close()
				offset += n
				if e != nil {
					return e
				}
				if response.StatusCode == 200 {
					break
				}
			}
			if e = f.Sync(); e != nil {
				return e
			}
			progress("Verifying Linux image checksum…")
			if !checksum(part, artifact.SHA256) {
				_ = os.Remove(part)
				return fmt.Errorf("Linux image checksum did not match the pinned release")
			}
			return os.Rename(part, path)
		}()
		if e == nil {
			return nil
		}
		last = e
		progress("Download interrupted; resuming verified image download…")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return fmt.Errorf("image download failed after retries: %w", last)
}
func (m *Manager) ensureImage(ctx context.Context) error {
	if _, e := os.Stat(filepath.Join(m.Paths.LimaHome(), Instance, "basedisk")); e == nil {
		return nil
	}
	b, e := os.ReadFile(filepath.Join(m.Paths.Resources, "runtime-lock.json"))
	if e != nil {
		return e
	}
	var manifest Manifest
	if e = json.Unmarshal(b, &manifest); e != nil {
		return e
	}
	return FetchVerified(ctx, manifest.Guest.Image, filepath.Join(m.Paths.Home, "images", "linux.img"), m.SetProgress)
}
