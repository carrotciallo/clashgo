package core

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// CheckTUNLaunch detects a restricted parent that would suppress the core's
// existing file capabilities. It does not elevate privileges or change policy.
func CheckTUNLaunch() error {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return fmt.Errorf("检查TUN启动权限: %w", err)
	}
	return checkTUNLaunchStatus(string(status))
}

func checkTUNLaunchStatus(status string) error {
	var restricted bool
	var permitted uint64
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "NoNewPrivs:":
			restricted = fields[1] == "1"
		case "CapPrm:":
			permitted, _ = strconv.ParseUint(fields[1], 16, 64)
		}
	}
	const networkCaps = uint64(1<<12 | 1<<13)
	if restricted && permitted&networkCaps != networkCaps {
		return fmt.Errorf("TUN无法启动：当前启动环境 NoNewPrivs=1，核心文件的网络能力会被屏蔽；请从正常桌面或用户服务启动 ClashGo")
	}
	return nil
}

// CheckTUNDevice verifies the live interface, not just the requested enable flag.
func CheckTUNDevice(config CaptureConfig) error {
	return checkTUNDevice(config, net.InterfaceByName)
}

func checkTUNDevice(config CaptureConfig, lookup func(string) (*net.Interface, error)) error {
	if !config.TUN.Enable {
		return fmt.Errorf("TUN运行配置未启用")
	}
	if config.TUN.Device == "" {
		return fmt.Errorf("TUN运行配置未返回网卡名称")
	}
	device, err := lookup(config.TUN.Device)
	if err != nil {
		return fmt.Errorf("TUN网卡未建立: %w", err)
	}
	if device.Flags&net.FlagUp == 0 || device.Flags&net.FlagLoopback != 0 {
		return fmt.Errorf("TUN网卡未就绪")
	}
	if device.MTU != 1500 {
		return fmt.Errorf("TUN网卡MTU=%d，要求1500", device.MTU)
	}
	return nil
}

// WaitTUNReady bounds asynchronous listener initialization. An HTTP-ready core
// can still have failed TUN setup, so /version is not sufficient for capture.
func (c *Client) WaitTUNReady(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		config, err := c.CaptureConfig(ctx)
		if err == nil {
			err = CheckTUNDevice(config)
		}
		if err == nil {
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("TUN未就绪（%v）: %w", last, ctx.Err())
		case <-ticker.C:
		}
	}
}
