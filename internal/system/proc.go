package system

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"cpuguard/internal/model"
)

type ProcTable struct {
	Processes map[int]model.HostProcess
}

func ReadProcTable() (*ProcTable, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	table := &ProcTable{Processes: map[int]model.HostProcess{}}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		process, err := readProcess(pid)
		if err != nil {
			continue
		}
		table.Processes[pid] = process
	}
	for pid, proc := range table.Processes {
		parent := table.Processes[proc.PPID]
		parent.Children = append(parent.Children, pid)
		table.Processes[proc.PPID] = parent
	}
	return table, nil
}

func readProcess(pid int) (model.HostProcess, error) {
	statData, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return model.HostProcess{}, err
	}
	stat := string(statData)
	start := strings.IndexByte(stat, '(')
	end := strings.LastIndexByte(stat, ')')
	if start < 0 || end < 0 || end+4 >= len(stat) {
		return model.HostProcess{}, fmt.Errorf("unexpected stat format for pid %d", pid)
	}
	name := stat[start+1 : end]
	rest := strings.Fields(stat[end+2:])
	if len(rest) < 2 {
		return model.HostProcess{}, fmt.Errorf("stat fields missing for pid %d", pid)
	}
	ppid, _ := strconv.Atoi(rest[1])
	cmdlineData, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")
	statusData, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	uid, user := parseStatusUID(statusData)
	cgroupPath, _ := readCgroupPath(pid)
	return model.HostProcess{
		PID:        pid,
		PPID:       ppid,
		Name:       name,
		Cmdline:    strings.TrimSpace(cmdline),
		UID:        uid,
		User:       user,
		CgroupPath: cgroupPath,
	}, nil
}

func parseStatusUID(statusData []byte) (int, string) {
	scanner := bufio.NewScanner(strings.NewReader(string(statusData)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		uid, _ := strconv.Atoi(fields[1])
		return uid, lookupUser(uid)
	}
	return 0, ""
}

func lookupUser(uid int) string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	target := ":" + strconv.Itoa(uid) + ":"
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, target) {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return ""
}

func readCgroupPath(pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) == 3 && parts[1] == "" {
			return parts[2], nil
		}
	}
	return "", nil
}

func DiscoverHostSubjects(rule model.Rule) ([]model.HostSubject, error) {
	table, err := ReadProcTable()
	if err != nil {
		return nil, err
	}
	re, err := compileRegex(rule.Selector.CmdlineRegex)
	if err != nil {
		return nil, err
	}
	excludeRe, err := compileRegex(rule.Exclude.CmdlineRegex)
	if err != nil {
		return nil, err
	}
	var subjects []model.HostSubject
	for _, proc := range table.Processes {
		if !matchesProcessSelector(proc, rule.Selector, re) {
			continue
		}
		if shouldExcludeProcess(proc, rule.Exclude, excludeRe) || isUnsafeProcess(proc) {
			continue
		}
		subjects = append(subjects, model.HostSubject{
			Rule:        rule,
			Root:        proc,
			Descendants: descendantsFor(table.Processes, proc.PID),
		})
	}
	return subjects, nil
}

func descendantsFor(processes map[int]model.HostProcess, root int) []int {
	stack := []int{root}
	var result []int
	seen := map[int]struct{}{}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		result = append(result, pid)
		for _, child := range processes[pid].Children {
			stack = append(stack, child)
		}
	}
	return result
}

func compileRegex(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}
	return regexp.Compile(expr)
}

func matchesProcessSelector(proc model.HostProcess, sel model.Selector, re *regexp.Regexp) bool {
	if sel.MatchAll {
		return true
	}
	if sel.ProcessName != "" && proc.Name != sel.ProcessName {
		return false
	}
	if sel.User != "" && proc.User != sel.User {
		return false
	}
	if sel.UID != nil && proc.UID != *sel.UID {
		return false
	}
	if sel.MinPID > 0 && proc.PID < sel.MinPID {
		return false
	}
	if re != nil && !re.MatchString(proc.Cmdline) {
		return false
	}
	return sel.ProcessName != "" || sel.User != "" || sel.UID != nil || sel.MinPID > 0 || re != nil
}

func shouldExcludeProcess(proc model.HostProcess, sel model.Selector, re *regexp.Regexp) bool {
	if sel.MatchAll {
		return true
	}
	if sel.ProcessName != "" && proc.Name == sel.ProcessName {
		return true
	}
	if sel.User != "" && proc.User == sel.User {
		return true
	}
	if sel.UID != nil && proc.UID == *sel.UID {
		return true
	}
	if sel.MinPID > 0 && proc.PID < sel.MinPID {
		return true
	}
	if re != nil && re.MatchString(proc.Cmdline) {
		return true
	}
	return false
}

func isUnsafeProcess(proc model.HostProcess) bool {
	policy := ProtectedPolicy()
	if proc.PID <= policy.PIDMin {
		return true
	}
	if proc.Cmdline == "" {
		return true
	}
	if isCriticalProcessName(proc.Name) {
		return true
	}
	if isCriticalCmdline(proc.Cmdline) {
		return true
	}
	if isCriticalCgroup(proc.CgroupPath) {
		return true
	}
	for _, fragment := range policy.CgroupFragments {
		if strings.Contains(proc.CgroupPath, fragment) {
			return true
		}
	}
	return proc.Name == "cpuguardd" || proc.Name == "cpuguardctl"
}

func ProtectedPolicy() model.ProtectedPolicy {
	return model.ProtectedPolicy{
		PIDMin: 100,
		ProcessNames: []string{
			"systemd",
			"kthreadd",
			"kworker",
			"ksoftirqd",
			"migration",
			"rcu_preempt",
			"rcu_sched",
			"watchdog",
			"dbus-broker",
			"dbus-daemon",
			"NetworkManager",
			"systemd-journald",
			"systemd-udevd",
			"systemd-logind",
			"systemd-resolved",
			"systemd-timesyncd",
			"sshd",
			"containerd",
			"dockerd",
			"runc",
			"init",
			"cpuguardd",
			"cpuguardctl",
		},
		ProcessPrefixes: []string{
			"kworker/",
			"ksoftirqd/",
			"migration/",
			"watchdog/",
		},
		CmdlineFragments: []string{
			"/usr/lib/systemd/",
			"/lib/systemd/",
			"/usr/bin/containerd",
			"/usr/bin/dockerd",
			"/usr/bin/dbus-broker",
			"/usr/sbin/sshd",
			"/usr/local/bin/cpuguardd",
		},
		CgroupPrefixes: []string{
			"/init.scope",
			"/system.slice",
			"/user.slice/user-0.slice",
		},
		CgroupFragments: []string{
			"/docker-",
			"/docker/",
			"/containerd/",
		},
		Notes: []string{
			"kernel threads and empty command lines are always protected",
			"cpuguard protects itself and common init/container runtime processes",
			"this built-in protection cannot be disabled by configuration",
		},
	}
}

func protectedStringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func isCriticalProcessName(name string) bool {
	policy := ProtectedPolicy()
	names := protectedStringSet(policy.ProcessNames)
	if _, ok := names[name]; ok {
		return true
	}
	for _, prefix := range policy.ProcessPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isCriticalCmdline(cmdline string) bool {
	for _, fragment := range ProtectedPolicy().CmdlineFragments {
		if strings.Contains(cmdline, fragment) {
			return true
		}
	}
	return false
}

func isCriticalCgroup(path string) bool {
	for _, prefix := range ProtectedPolicy().CgroupPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
