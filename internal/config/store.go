// Store owns ClashGo's on-disk layout. All state is plain files:
//
//	<ConfigDir>/master.yaml         the user's master config (source of truth)
//	<ConfigDir>/subscriptions.json  subscription registry
//	<ConfigDir>/settings.json       app settings
//	<StateDir>/runtime/             rendered config + core home (geodata, providers)
//	<StateDir>/logs/                core + app logs
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Settings are the app-level preferences edited on the settings page.
type Settings struct {
	CorePath         string `json:"corePath"`         // empty = auto-discover
	MixedPort        int    `json:"mixedPort"`        // default 7897
	ControllerPort   int    `json:"controllerPort"`   // default 9097
	ControllerSecret string `json:"controllerSecret"` // generated on first run
	ArmProxyOnStart  bool   `json:"armProxyOnStart"`  // arm system proxy once core is healthy
	AutoStart        bool   `json:"autoStart"`        // systemd user unit enabled
	SilentAutostart  bool   `json:"silentAutostart"`  // launched via --autostart: tray only, no window. Defaults true (see LoadSettings).
	TUNEnabled       bool   `json:"tunEnabled"`       // TUN mode; needs caps on the core binary (pkexec once)
	// Profiles: the master config is one of several switchable
	// profiles (Shadowrocket's config files). "" names the built-in
	// master.yaml; any other name is profiles/<name>.yaml.
	ActiveProfile  string `json:"activeProfile"`
	DefaultProfile string `json:"defaultProfile"`
	// Services lists the dashboard service checks the user enabled
	// (names from the built-in catalog); empty means all of them.
	Services []string `json:"services,omitempty"`
	// CollapsedSubs lists subscriptions whose node list is
	// folded away on the nodes page (UI state, persisted so the
	// fold survives restarts).
	CollapsedSubs []string `json:"collapsedSubs,omitempty"`
}

// ProviderSource is what rendering needs to know about a subscription.
type ProviderSource struct {
	Name string
	URL  string
	// Split marks a subscription whose payload was a whole client
	// config, flattened at fetch time (see subs.SplitFullConfig).
	// InlineYAML then holds the flattened nodes' dialer plumbing
	// as YAML list items, spliced into the main proxies: section
	// where the chains register globally.
	Split      bool
	InlineYAML string
}

type Store struct {
	ConfigDir string
	StateDir  string
	// activeProfile selects which file MasterPath resolves to (""
	// = the built-in master.yaml). Set once at startup from the
	// persisted settings and again on every profile switch.
	activeProfile string
}

func NewStore() (*Store, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return &Store{
		ConfigDir: filepath.Join(cfgDir, "clashgo"),
		StateDir:  filepath.Join(stateHome, "clashgo"),
	}, nil
}

// MasterPath resolves to the ACTIVE profile's file: the built-in
// master.yaml, or profiles/<name>.yaml after a profile switch.
// Everything that reads or writes "the master config" (rendering,
// snapshots, rule edits) goes through here and follows the switch.
func (s *Store) MasterPath() string {
	if s.activeProfile != "" {
		return s.ProfilePath(s.activeProfile)
	}
	return filepath.Join(s.ConfigDir, "master.yaml")
}

func (s *Store) ProfilesDir() string { return filepath.Join(s.ConfigDir, "profiles") }

func (s *Store) ProfilePath(name string) string {
	return filepath.Join(s.ProfilesDir(), name+".yaml")
}

func (s *Store) ActiveProfile() string { return s.activeProfile }

// SetActiveProfile points MasterPath at the named profile (""
// = built-in). Callers persist the choice in Settings themselves.
func (s *Store) SetActiveProfile(name string) { s.activeProfile = name }

func (s *Store) ProfileExists(name string) bool {
	if name == "" {
		return true
	}
	_, err := os.Stat(s.ProfilePath(name))
	return err == nil
}

// ProfileInfo describes one switchable config file.
type ProfileInfo struct {
	Name    string // "" = built-in master.yaml
	Path    string
	Size    int64
	ModTime time.Time
}

// Profiles lists the built-in config first, then profiles/*.yaml in
// name order — the list the config page renders for quick switching.
func (s *Store) Profiles() []ProfileInfo {
	out := []ProfileInfo{}
	if fi, err := os.Stat(filepath.Join(s.ConfigDir, "master.yaml")); err == nil {
		out = append(out, ProfileInfo{Name: "", Path: filepath.Join(s.ConfigDir, "master.yaml"), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	entries, _ := os.ReadDir(s.ProfilesDir())
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if fi, err := os.Stat(s.ProfilePath(n)); err == nil {
			out = append(out, ProfileInfo{Name: n, Path: s.ProfilePath(n), Size: fi.Size(), ModTime: fi.ModTime()})
		}
	}
	return out
}

// ValidProfileName keeps profile names safe as file names while
// allowing the CJK names users actually pick.
func ValidProfileName(name string) bool {
	if name == "" || len([]rune(name)) > 40 {
		return false
	}
	return !strings.ContainsAny(name, `/\:*?"<>|`) && !strings.HasPrefix(name, ".")
}

// CreateProfile writes a new profile seeded with content (usually
// the active config, so the user edits a copy). Refuses to overwrite.
func (s *Store) CreateProfile(name, content string) error {
	if !ValidProfileName(name) {
		return fmt.Errorf("配置名不合法：%q", name)
	}
	if err := os.MkdirAll(s.ProfilesDir(), 0o755); err != nil {
		return err
	}
	p := s.ProfilePath(name)
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("配置 %s 已存在", name)
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

// DeleteProfile removes a profile file. The caller refuses the
// active and default profiles.
func (s *Store) DeleteProfile(name string) error {
	if name == "" {
		return fmt.Errorf("内置配置不能删除")
	}
	return os.Remove(s.ProfilePath(name))
}
func (s *Store) SubsPath() string     { return filepath.Join(s.ConfigDir, "subscriptions.json") }
func (s *Store) SettingsPath() string { return filepath.Join(s.ConfigDir, "settings.json") }
func (s *Store) RuntimeDir() string   { return filepath.Join(s.StateDir, "runtime") }
func (s *Store) RenderedPath() string {
	return filepath.Join(s.RuntimeDir(), "config.yaml")
}
func (s *Store) ProvidersDir() string { return filepath.Join(s.RuntimeDir(), "providers") }
func (s *Store) LogsDir() string      { return filepath.Join(s.StateDir, "logs") }

// EnsureLayout creates the directory tree and first-run files. It never
// overwrites an existing master config or settings.
func (s *Store) EnsureLayout() error {
	for _, d := range []string{s.ConfigDir, s.ProfilesDir(), s.RuntimeDir(), s.ProvidersDir(), s.LogsDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if _, err := os.Stat(s.MasterPath()); os.IsNotExist(err) {
		if err := os.WriteFile(s.MasterPath(), []byte(DefaultMaster), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat(s.SettingsPath()); os.IsNotExist(err) {
		secret, err := randomSecret()
		if err != nil {
			return err
		}
		def := Settings{MixedPort: 7897, ControllerPort: 9097, ControllerSecret: secret, ArmProxyOnStart: true, SilentAutostart: true}
		if err := s.SaveSettings(def); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadSettings() (Settings, error) {
	var st Settings
	b, err := os.ReadFile(s.SettingsPath())
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("settings.json: %w", err)
	}
	// silentAutostart defaults ON: a settings file written before
	// the option existed must behave as enabled until the user
	// turns it off.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err == nil {
		if _, ok := raw["silentAutostart"]; !ok {
			st.SilentAutostart = true
		}
	}
	if st.MixedPort == 0 {
		st.MixedPort = 7897
	}
	if st.ControllerPort == 0 {
		st.ControllerPort = 9097
	}
	return st, nil
}

func (s *Store) SaveSettings(st Settings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.SettingsPath(), b, 0o600)
}

func (s *Store) LoadMaster() (string, error) {
	b, err := os.ReadFile(s.MasterPath())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SaveMaster replaces the master config atomically. Callers render
// and validate before saving, so a bad config never lands here.
func (s *Store) SaveMaster(content string) error {
	return writeFileAtomic(s.MasterPath(), []byte(content), 0o644)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func randomSecret() (string, error) {
	var b [16]byte
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b[:]), nil
}
