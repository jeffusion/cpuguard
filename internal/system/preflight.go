package system

import (
	"fmt"
	"os"
	"strings"
)

func Preflight() error {
	data, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		return fmt.Errorf("cgroup v2 is required: %w", err)
	}
	if !strings.Contains(" "+string(data)+" ", " cpu ") {
		return fmt.Errorf("cgroup v2 cpu controller is not available")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("cpuguardd must run as root")
	}
	return nil
}
