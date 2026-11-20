// tunonly-helper applies or removes the fixed cg_tunonly nftables table
// for ClashGo's tun-only mode. It is installed root-owned with
// cap_net_admin=ep and accepts exactly one argument — on, off or status.
// The ruleset is compiled in; callers cannot influence it.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const tableName = "cg_tunonly"

// coreMark must match internal/config.CoreRoutingMark.
const coreMark = "0x4347"

// The uplink is this machine's wired interface (enp5s0). The core's own
// sockets carry coreMark and are always allowed out; the node addresses
// stay reachable even if a core socket misses the mark; LAN and DHCP are
// local recovery surface and stay open. Everything else leaving the
// uplink is dropped — such traffic has to enter the TUN device instead.
const ruleset = `table inet cg_tunonly {
	chain out {
		type filter hook output priority 0; policy accept;
		oifname "enp5s0" meta mark 0x4347 accept
		oifname "enp5s0" ip daddr 45.32.51.34 udp dport { 443, 8443 } accept
		oifname "enp5s0" ip daddr 45.32.51.34 tcp dport 443 accept
		oifname "enp5s0" ip daddr 192.168.2.0/24 accept
		oifname "enp5s0" udp dport { 67, 68 } accept
		oifname "enp5s0" ip6 daddr != fe80::/10 drop
		oifname "enp5s0" drop
	}
}
`

// raiseAmbient lifts CAP_NET_ADMIN into the ambient set so the nft
// child process inherits it — file capabilities alone do not cross an
// exec into a different binary. Raw syscalls keep this helper free of
// module dependencies.
const (
	sysCapget         = 125
	sysCapset         = 126
	sysPrctl          = 157
	prCapAmbient      = 47
	prCapAmbientRaise = 2
	capNetAdmin       = 12
	capVersion3       = 0x20080522
)

type capHeader struct {
	version uint32
	pid     int32
}

type capData struct {
	effective, permitted, Inheritable uint32
}

func raiseAmbient() error {
	hdr := capHeader{version: capVersion3}
	var data [2]capData
	if _, _, errno := syscall.Syscall(sysCapget, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0); errno != 0 {
		return errno
	}
	data[0].Inheritable |= 1 << capNetAdmin
	if _, _, errno := syscall.Syscall(sysCapset, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data[0])), 0); errno != 0 {
		return errno
	}
	if _, _, errno := syscall.Syscall(sysPrctl, prCapAmbient, prCapAmbientRaise, capNetAdmin); errno != 0 {
		return errno
	}
	return nil
}

func nft(args ...string) (string, error) {
	out, err := exec.Command("/usr/bin/nft", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tunonly-helper on|off|status")
		os.Exit(2)
	}
	if err := raiseAmbient(); err != nil {
		fmt.Fprintf(os.Stderr, "raise ambient CAP_NET_ADMIN: %v\n", err)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "on":
		_, _ = nft("delete", "table", "inet", tableName) // idempotent re-apply
		cmd := exec.Command("/usr/bin/nft", "-f", "-")
		cmd.Stdin = strings.NewReader(ruleset)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "apply table: %v: %s\n", err, strings.TrimSpace(string(out)))
			os.Exit(1)
		}
		fmt.Println("on")
	case "off":
		if _, err := nft("list", "table", "inet", tableName); err != nil {
			fmt.Println("off") // already absent: off is idempotent
			return
		}
		if _, err := nft("delete", "table", "inet", tableName); err != nil {
			fmt.Fprintln(os.Stderr, "delete table failed")
			os.Exit(1)
		}
		fmt.Println("off")
	case "status":
		if _, err := nft("list", "table", "inet", tableName); err != nil {
			fmt.Println("off")
			return
		}
		fmt.Println("on")
	default:
		fmt.Fprintln(os.Stderr, "usage: tunonly-helper on|off|status")
		os.Exit(2)
	}
}
