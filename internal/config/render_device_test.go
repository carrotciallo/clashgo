package config

import (
	"strings"
	"testing"
)

// The rendered config must rename the TUN device away from the core's
// product default.
func TestRenderStampsTunDevice(t *testing.T) {
	out := Render("{}", nil, Settings{TUNEnabled: true, MixedPort: 7897, ControllerPort: 9097})
	if !strings.Contains(out, "device: "+TunDeviceName) {
		t.Fatalf("rendered config lacks device %q:\n%s", TunDeviceName, out)
	}
	if strings.Contains(out, "device: Meta") {
		t.Fatalf("rendered config still names the device Meta:\n%s", out)
	}
}
