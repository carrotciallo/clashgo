// Package core supervises the mihomo sidecar and talks to its
// External Controller API.
package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Supervisor owns one mihomo child process. The child is spawned with
// PDEATHSIG so it can never outlive the GUI (Phase 0 left orphans).
type Supervisor struct {
	BinPath string
	HomeDir string
	LogPath string
	// LogSealed seals the core log to mode 0000 outside the open in
	// Start (see OpenLogFile).
	LogSealed bool

	// OnUnexpectedExit, when set, runs after the child is reaped if
	// the exit was not requested through Stop.
	OnUnexpectedExit func()

	mu          sync.Mutex
	cmd         *exec.Cmd
	done        chan struct{}
	stopTimeout time.Duration
	stopping    bool
}

func (s *Supervisor) Start(configFile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return fmt.Errorf("core already running")
	}
	if err := os.MkdirAll(s.HomeDir, 0o755); err != nil {
		return err
	}
	logf, err := OpenLogFile(s.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, s.LogSealed)
	if err != nil {
		return err
	}
	cmd := exec.Command(s.BinPath, "-d", s.HomeDir, "-f", configFile)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		logf.Close()
		return err
	}
	s.cmd = cmd
	s.stopping = false
	done := make(chan struct{})
	s.done = done
	go func() {
		_ = cmd.Wait()
		_ = logf.Close()
		s.mu.Lock()
		s.cmd = nil
		unexpected := !s.stopping
		cb := s.OnUnexpectedExit
		close(done)
		s.mu.Unlock()
		if unexpected && cb != nil {
			cb()
		}
	}()
	return nil
}

// Stop reaps the child before returning, escalating after a bounded grace
// period. Never read exec.Cmd.ProcessState concurrently with Cmd.Wait.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopping = true
	cmd, done, timeout := s.cmd, s.done, s.stopTimeout
	s.mu.Unlock()
	if cmd == nil {
		return
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
	}
}

// SetBinary changes the executable only between child lifetimes.
func (s *Supervisor) SetBinary(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return fmt.Errorf("core already running")
	}
	s.BinPath = path
	return nil
}

func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil
}

func (s *Supervisor) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// LogTail returns the last n lines of the core log for error surfacing.
func (s *Supervisor) LogTail(n int) string {
	b, err := os.ReadFile(s.LogPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ValidateConfig runs `mihomo -t` against a rendered config. It is
// time-boxed: a core that wants to download missing geodata must fail
// the validation, never wedge it (Phase 0/1 finding).
func ValidateConfig(binPath, homeDir, configFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "-d", homeDir, "-f", configFile, "-t")
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("mihomo -t 超时（30s）——核心家目录可能缺 geodata 或配置触发了网络加载")
	}
	if err != nil {
		return fmt.Errorf("mihomo -t: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HasTunCaps reports whether the core binary carries the capabilities
// creating a TUN device needs (cap_net_admin + cap_net_raw).
func HasTunCaps(binPath string) bool {
	out, err := exec.Command("getcap", binPath).Output()
	if err != nil {
		return false
	}
	s := string(out)
	return strings.Contains(s, "cap_net_admin") && strings.Contains(s, "cap_net_raw")
}

// EnsureTunCaps grants the core binary its TUN capabilities through
// pkexec — one polkit prompt on the user's desktop, then the caps ride
// the binary file itself. No-op when already granted.
func EnsureTunCaps(binPath string) error {
	if HasTunCaps(binPath) {
		return nil
	}
	out, err := exec.Command("pkexec", "setcap", "cap_net_admin,cap_net_raw=+ep", binPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pkexec setcap: %v（%s）", err, strings.TrimSpace(string(out)))
	}
	if !HasTunCaps(binPath) {
		return fmt.Errorf("授权后仍未检测到 capabilities")
	}
	return nil
}

// FindCore locates the mihomo binary: explicit setting, then next to
// the executable (bundled), then PATH.
func FindCore(configured string) string {
	if configured != "" {
		return configured // explicit configuration must fail visibly, not fall back
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "mihomo")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("mihomo"); err == nil {
		return p
	}
	return ""
}

// geoFiles are the data files a core home must contain before spawn —
// a missing geoip.metadb makes mihomo block on a network download
// instead of erroring (Phase 0 finding), so we seed before starting.
var geoFiles = []string{"geoip.metadb", "Country.mmdb", "GEOIP.dat", "GEOSITE.dat", "ASN.mmdb"}

// SeedGeodata copies any missing geodata files into homeDir from the
// first source dir that has them (bundled geodata dir preferred).
// Source files are matched case-insensitively — other clients ship
// e.g. GEOIP.metadb while mihomo wants geoip.metadb, and a silent miss
// here makes the core hang on a network download instead of starting.
// Returns the names that were seeded.
func SeedGeodata(homeDir string, sourceDirs []string) []string {
	var seeded []string
	for _, name := range geoFiles {
		dst := filepath.Join(homeDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		for _, srcDir := range sourceDirs {
			if src := findCaseInsensitive(srcDir, name); src != "" {
				if b, err := os.ReadFile(src); err == nil {
					if os.WriteFile(dst, b, 0o644) == nil {
						seeded = append(seeded, name)
						break
					}
				}
			}
		}
	}
	return seeded
}

func findCaseInsensitive(dir, name string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// GeodataDirs lists candidate source dirs for seeding, best first.
func GeodataDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "geodata"))
	}
	dirs = append(dirs, "/usr/share/clashgo/geodata")
	if home, err := os.UserHomeDir(); err == nil {
		// Dev-machine convenience: an existing FlClash install is a
		// known-good geodata source.
		dirs = append(dirs, filepath.Join(home, ".local", "share", "com.follow.clash"))
	}
	return dirs
}
