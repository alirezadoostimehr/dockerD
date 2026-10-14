package config

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Settings struct {
	CPUs             int      `json:"cpus"`
	MemoryGiB        int      `json:"memoryGiB"`
	DiskGiB          int      `json:"diskGiB"`
	Kubernetes       bool     `json:"kubernetes"`
	SharedFolders    []string `json:"sharedFolders"`
	IdleEnabled      bool     `json:"idleEnabled"`
	IdleMinutes      int      `json:"idleMinutes"`
	CountdownSeconds int      `json:"countdownSeconds"`
}

func Defaults() Settings {
	return Settings{CPUs: min(4, runtime.NumCPU()), MemoryGiB: min(4, MaxMemoryGiB()), DiskGiB: 60, Kubernetes: true, SharedFolders: []string{}, IdleEnabled: true, IdleMinutes: 10, CountdownSeconds: 60}
}

func MaxMemoryGiB() int {
	if runtime.GOOS != "darwin" {
		return 128
	}
	b, e := exec.Command("/usr/sbin/sysctl", "-n", "hw.memsize").Output()
	if e != nil {
		return 4
	}
	n, e := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if e != nil {
		return 4
	}
	return max(2, min(128, int(n/(1<<30)/2)))
}

func (s Settings) Validate() error {
	if s.CPUs < 1 || s.CPUs > runtime.NumCPU() {
		return errors.New("CPU allocation must be between 1 and the host CPU count")
	}
	if s.MemoryGiB < 2 || s.MemoryGiB > MaxMemoryGiB() {
		return errors.New("memory must be at least 2 GiB and no more than half of host memory (maximum 128 GiB)")
	}
	if s.DiskGiB < 20 || s.DiskGiB > 1024 {
		return errors.New("disk must be between 20 and 1024 GiB")
	}
	if s.IdleMinutes < 1 || s.IdleMinutes > 1440 {
		return errors.New("idle timeout must be between 1 and 1440 minutes")
	}
	if s.CountdownSeconds < 10 || s.CountdownSeconds > 300 {
		return errors.New("countdown must be between 10 and 300 seconds")
	}
	for _, p := range s.SharedFolders {
		if !filepath.IsAbs(p) {
			return errors.New("shared folders must use absolute paths")
		}
		info, e := os.Stat(p)
		if e != nil || !info.IsDir() {
			return errors.New("shared folder does not exist: " + p)
		}
	}
	return nil
}

func Load(path string) (Settings, error) {
	s := Defaults()
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	if e != nil {
		return s, e
	}
	return s, s.Validate()
}
func Save(path string, s Settings) error {
	if e := s.Validate(); e != nil {
		return e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(path+".tmp", b, 0600); e != nil {
		return e
	}
	return os.Rename(path+".tmp", path)
}
