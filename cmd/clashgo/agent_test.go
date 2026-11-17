package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/subs"
)

// The status contract's hard rule: no secrets ever leave the app —
// neither the controller secret nor subscription URLs (which carry
// the airport token). This pins the offline builder, which reads the
// raw persisted files.
func TestOfflineStatusHidesSecrets(t *testing.T) {
	cfgHome := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", stateHome)

	store, err := config.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	set := config.Settings{
		MixedPort: 7897, ControllerPort: 9097,
		ControllerSecret: "topsecret-value", TUNEnabled: true,
	}
	if err := store.SaveSettings(set); err != nil {
		t.Fatal(err)
	}
	reg, err := subs.Load(store.SubsPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add("test-sub", "https://example.com/feed?token=deadbeef"); err != nil {
		t.Fatal(err)
	}

	st := offlineStatus(store)
	if st.Schema != agentSchema {
		t.Errorf("schema = %d, want %d", st.Schema, agentSchema)
	}
	if st.Capture.Mode != "off" || st.Instance.Running {
		t.Errorf("offline status must report a stopped instance, got mode=%q running=%v", st.Capture.Mode, st.Instance.Running)
	}
	if st.Capture.MixedPort != 7897 || st.Capture.ControllerPort != 9097 {
		t.Errorf("ports not carried over: %+v", st.Capture)
	}
	if len(st.Subscriptions) != 1 || st.Subscriptions[0].Name != "test-sub" {
		t.Errorf("subscriptions = %+v", st.Subscriptions)
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"topsecret-value", "deadbeef", "example.com/feed"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("status leaks %q: %s", secret, data)
		}
	}
}

func TestControlCodecRoundTrip(t *testing.T) {
	req := ctrlRequest{Cmd: "mode.set", Args: json.RawMessage(`{"mode":"tun"}`)}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var back ctrlRequest
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Cmd != "mode.set" || string(back.Args) != `{"mode":"tun"}` {
		t.Errorf("round trip = %+v", back)
	}

	resp := ctrlResponse{OK: false, Error: ctrlErr("invalid_mode", "只接受 tun")}
	data, err = json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"code":"invalid_mode"`) {
		t.Errorf("error shape = %s", data)
	}
}

func TestParseCLIFlags(t *testing.T) {
	f := parseCLIFlags([]string{"set", "off", "--yes", "--dry-run"})
	if len(f.pos) != 2 || f.pos[0] != "set" || f.pos[1] != "off" || !f.yes || !f.dry {
		t.Errorf("flags = %+v", f)
	}
	f = parseCLIFlags([]string{"--since", "2h", "--human"})
	if f.since != "2h" || !f.human || len(f.pos) != 0 {
		t.Errorf("flags = %+v", f)
	}
}

func TestOpNeedsYes(t *testing.T) {
	cases := []struct {
		cmd  string
		args []string
		want bool
	}{
		{"quit", nil, true},
		{"core", []string{"stop"}, true},
		{"core", []string{"restart"}, false},
		{"mode", []string{"set", "off"}, true},
		{"mode", []string{"set", "tun"}, false},
		{"node", []string{"select", "x"}, false},
	}
	for _, c := range cases {
		if got := opNeedsYes(c.cmd, parseCLIFlags(c.args)); got != c.want {
			t.Errorf("opNeedsYes(%s %v) = %v, want %v", c.cmd, c.args, got, c.want)
		}
	}
}

func TestProcParsers(t *testing.T) {
	text := "Name:\tmihomo\nNoNewPrivs:\t0\nCapEff:\t0000000000003000\n"
	if n, ok := parseNoNewPrivs(text); !ok || n != 0 {
		t.Errorf("NoNewPrivs = %d, %v", n, ok)
	}
	caps, ok := parseCapEff(text)
	if !ok || caps&(1<<12) == 0 || caps&(1<<13) == 0 {
		t.Errorf("CapEff = %#x, %v — want NET_ADMIN+NET_RAW bits", caps, ok)
	}
	if _, ok := parseNoNewPrivs("Name:\tx\n"); ok {
		t.Error("missing NoNewPrivs must not parse")
	}
	if _, ok := parseCapEff("CapEff:\tzz\n"); ok {
		t.Error("garbage CapEff must not parse")
	}
}
