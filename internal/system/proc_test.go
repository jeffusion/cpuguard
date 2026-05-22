package system

import (
	"testing"

	"cpuguard/internal/model"
)

func TestIsUnsafeProcessProtectsCriticalProcesses(t *testing.T) {
	cases := []model.HostProcess{
		{PID: 1, Name: "systemd", Cmdline: "/sbin/init", CgroupPath: "/init.scope"},
		{PID: 200, Name: "dockerd", Cmdline: "/usr/bin/dockerd", CgroupPath: "/system.slice/docker.service"},
		{PID: 201, Name: "cpuguardd", Cmdline: "/usr/local/bin/cpuguardd", CgroupPath: "/system.slice/cpuguard.service"},
		{PID: 202, Name: "kworker/0:1", Cmdline: "", CgroupPath: "/"},
	}
	for _, tc := range cases {
		if !isUnsafeProcess(tc) {
			t.Fatalf("expected %s pid %d to be unsafe", tc.Name, tc.PID)
		}
	}
}

func TestIsUnsafeProcessAllowsRegularUserProcess(t *testing.T) {
	proc := model.HostProcess{
		PID:        5000,
		Name:       "node",
		Cmdline:    "node dev-server.js",
		CgroupPath: "/user.slice/user-1000.slice/session-2.scope",
	}
	if isUnsafeProcess(proc) {
		t.Fatalf("expected regular user process to be eligible")
	}
}

func TestIsUnsafeProcessAllowsManagedCpuguardSubjects(t *testing.T) {
	proc := model.HostProcess{
		PID:        5001,
		Name:       "cpuguard-burn",
		Cmdline:    "cpuguard-burn -duration 90s -recover-duration 90s",
		CgroupPath: "/cpuguard/host-cpuguard-burn/5001",
	}
	if isUnsafeProcess(proc) {
		t.Fatalf("expected cpuguard-managed user process to remain eligible for recovery sampling")
	}
}

func TestProtectedPolicyIncludesCriticalProcessRules(t *testing.T) {
	policy := ProtectedPolicy()
	if policy.PIDMin != 100 {
		t.Fatalf("pid min = %d, want 100", policy.PIDMin)
	}
	if len(policy.ProcessNames) == 0 || len(policy.CmdlineFragments) == 0 || len(policy.CgroupPrefixes) == 0 {
		t.Fatalf("policy is incomplete: %+v", policy)
	}
}
