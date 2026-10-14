package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Defaults()
	s.SharedFolders = []string{t.TempDir()}
	if e := Save(path, s); e != nil {
		t.Fatal(e)
	}
	loaded, e := Load(path)
	if e != nil || loaded.IdleMinutes != 10 || len(loaded.SharedFolders) != 1 {
		t.Fatalf("round trip: %+v %v", loaded, e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("settings must be private")
	}
}
func TestRejectUnsafeSettings(t *testing.T) {
	for _, mutate := range []func(*Settings){func(s *Settings) { s.IdleMinutes = 0 }, func(s *Settings) { s.CountdownSeconds = 0 }, func(s *Settings) { s.MemoryGiB = 999 }, func(s *Settings) { s.SharedFolders = []string{"relative/path"} }} {
		s := Defaults()
		mutate(&s)
		if s.Validate() == nil {
			t.Fatal("invalid settings accepted")
		}
	}
}
