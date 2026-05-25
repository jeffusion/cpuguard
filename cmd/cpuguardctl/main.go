package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"cpuguard/internal/api"
	"cpuguard/internal/config"
	"cpuguard/internal/model"
	"cpuguard/internal/system"
	"cpuguard/internal/tui"
)

func main() {
	var socketPath string
	var configPath string
	var serviceName string
	var jsonOutput bool
	flag.StringVar(&socketPath, "socket", "/run/cpuguardd.sock", "path to cpuguard unix socket")
	flag.StringVar(&configPath, "config", "/etc/cpuguard/config.yaml", "path to config file")
	flag.StringVar(&serviceName, "service", "cpuguard.service", "systemd service name")
	flag.BoolVar(&jsonOutput, "json", false, "output JSON for supported commands")
	flag.Usage = usage
	flag.CommandLine.Parse(stripJSONFlag(os.Args[1:], &jsonOutput))
	client := api.NewClient(socketPath)
	if flag.NArg() < 1 {
		if terminalSession() && !jsonOutput {
			if err := tui.Run(socketPath); err != nil {
				fatal(err.Error())
			}
			return
		}
		if err := printStatus(client, serviceName, jsonOutput); err != nil {
			if code, ok := err.(exitError); ok {
				os.Exit(int(code))
			}
			fatal(err.Error())
		}
		if !jsonOutput {
			fmt.Fprintln(os.Stderr, "tip: run cpuguardctl tui from a terminal for interactive monitoring")
		}
		return
	}
	var (
		body []byte
		err  error
	)
	switch flag.Arg(0) {
	case "start":
		err = runSystemctl("start", serviceName)
	case "stop":
		err = runSystemctl("stop", serviceName)
	case "restart":
		err = runSystemctl("restart", serviceName)
	case "enable":
		err = runSystemctl("enable", serviceName)
	case "disable":
		err = runSystemctl("disable", serviceName)
	case "check-config":
		if flag.NArg() >= 2 {
			configPath = flag.Arg(1)
		}
		if _, err := config.Load(configPath); err != nil {
			fatal(err.Error())
		}
		fmt.Printf("config ok: %s\n", configPath)
		return
	case "status":
		err = printStatus(client, serviceName, jsonOutput)
	case "doctor":
		err = printDoctor(client, serviceName, configPath, socketPath, jsonOutput)
	case "limits":
		err = printLimits(client, jsonOutput)
	case "logs":
		err = printLogs(client, flag.Args()[1:], jsonOutput)
	case "protected":
		err = printProtected(client, jsonOutput)
	case "rules":
		body, err = client.Get("/v1/rules")
	case "tui":
		err = tui.Run(socketPath)
	case "reload":
		body, err = client.Post("/v1/reload")
	case "unthrottle":
		if flag.NArg() < 2 {
			fatal("missing subject id")
		}
		body, err = client.Post("/v1/subjects/" + flag.Arg(1) + "/unthrottle")
	case "hold":
		if flag.NArg() < 2 {
			fatal("missing subject id")
		}
		body, err = client.Post("/v1/subjects/" + flag.Arg(1) + "/hold")
	case "throttle":
		if flag.NArg() < 2 {
			fatal("missing subject id")
		}
		body, err = client.Post("/v1/subjects/" + flag.Arg(1) + "/throttle")
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		if code, ok := err.(exitError); ok {
			os.Exit(int(code))
		}
		fatal(err.Error())
	}
	if body == nil {
		return
	}
	fmt.Println(string(body))
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func terminalSession() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func stripJSONFlag(args []string, jsonOutput *bool) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" || arg == "-json" {
			*jsonOutput = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

func printStatus(client *api.Client, serviceName string, jsonOutput bool) error {
	active, err := systemctlOutput("is-active", serviceName)
	if err != nil {
		active = "unknown"
	}
	enabled, err := systemctlOutput("is-enabled", serviceName)
	if err != nil {
		enabled = "unknown"
	}

	body, err := client.Get("/v1/status")
	if err != nil {
		if jsonOutput {
			return writeJSONOutput(map[string]any{
				"service": strings.TrimSpace(active),
				"enabled": strings.TrimSpace(enabled),
				"daemon":  "unavailable",
				"error":   err.Error(),
			})
		}
		fmt.Printf("service: %s\n", strings.TrimSpace(active))
		fmt.Printf("enabled: %s\n", strings.TrimSpace(enabled))
		fmt.Printf("daemon: unavailable (%v)\n", err)
		return nil
	}
	var status model.DaemonStatus
	if err := json.Unmarshal(body, &status); err != nil {
		fmt.Println(string(body))
		return nil
	}
	if jsonOutput {
		return writeJSONOutput(map[string]any{
			"service": strings.TrimSpace(active),
			"enabled": strings.TrimSpace(enabled),
			"daemon":  "available",
			"status":  status,
		})
	}
	fmt.Printf("service: %s\n", strings.TrimSpace(active))
	fmt.Printf("enabled: %s\n", strings.TrimSpace(enabled))
	fmt.Println("daemon: available")
	fmt.Printf("system cpu: %.1f\n", status.System.CPUPercent)
	fmt.Printf("cpu temp: %s\n", formatOptionalTemperature(status.System.CPUTempC))
	fmt.Printf("cpu power: %s\n", formatOptionalPower(status.System.CPUPowerW))
	fmt.Printf("load: %.2f %.2f %.2f\n", status.System.Load1, status.System.Load5, status.System.Load15)
	fmt.Printf("subjects: %d\n", status.Summary.Total)
	fmt.Printf("throttled: %d\n", status.Summary.Throttled)
	fmt.Printf("held: %d\n", status.Summary.Held)
	if status.Summary.Total == 0 {
		fmt.Println("note: no subjects matched by current rules")
	}
	return nil
}

func printLimits(client *api.Client, jsonOutput bool) error {
	subjects, err := client.ListLimits()
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSONOutput(subjects)
	}
	if len(subjects) == 0 {
		fmt.Println("no active limits")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATE\tCPU\tLIMIT\tRULE\tSCOPE\tTARGET\tUPDATED\tCOOLDOWN")
	for _, subject := range subjects {
		fmt.Fprintf(w, "%s\t%.1f\t%s\t%s\t%s\t%s\t%s\t%s\n",
			subject.State,
			subject.LastCPUPct,
			formatSubjectLimit(subject),
			subject.RuleID,
			subject.Scope,
			subject.TargetRef,
			formatTime(subject.UpdatedAt),
			formatTime(subject.CooldownUntil),
		)
	}
	return w.Flush()
}

func printLogs(client *api.Client, args []string, jsonOutput bool) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	subjectID := fs.String("subject", "", "filter by subject id")
	eventType := fs.String("type", "", "filter by event type")
	sinceRaw := fs.String("since", "", "filter by duration or RFC3339 time")
	limit := fs.Int("limit", 100, "maximum events")
	if err := fs.Parse(args); err != nil {
		return err
	}
	since, err := parseSince(*sinceRaw)
	if err != nil {
		return err
	}
	events, err := client.QueryEvents(model.EventQuery{SubjectID: *subjectID, Type: *eventType, Since: since, Limit: *limit})
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSONOutput(events)
	}
	if len(events) == 0 {
		fmt.Println("no events")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tTYPE\tCPU\tLIMIT\tSUBJECT\tMESSAGE")
	for _, event := range events {
		fmt.Fprintf(w, "%s\t%s\t%.1f\t%s\t%s\t%s\n",
			formatTime(event.CreatedAt),
			event.Type,
			event.CPUPct,
			formatEventLimit(event),
			event.SubjectID,
			event.Message,
		)
	}
	return w.Flush()
}

func printProtected(client *api.Client, jsonOutput bool) error {
	policy, err := client.ProtectedPolicy()
	if err != nil {
		return err
	}
	if jsonOutput {
		return writeJSONOutput(policy)
	}
	fmt.Printf("pid <= %d\n", policy.PIDMin)
	fmt.Printf("process names: %s\n", strings.Join(policy.ProcessNames, ", "))
	fmt.Printf("process prefixes: %s\n", strings.Join(policy.ProcessPrefixes, ", "))
	fmt.Printf("cmdline fragments: %s\n", strings.Join(policy.CmdlineFragments, ", "))
	fmt.Printf("cgroup prefixes: %s\n", strings.Join(policy.CgroupPrefixes, ", "))
	fmt.Printf("cgroup fragments: %s\n", strings.Join(policy.CgroupFragments, ", "))
	for _, note := range policy.Notes {
		fmt.Printf("note: %s\n", note)
	}
	return nil
}

func printDoctor(client *api.Client, serviceName string, configPath string, socketPath string, jsonOutput bool) error {
	report := buildDoctorReport(client, serviceName, configPath, socketPath)
	if jsonOutput {
		if err := writeJSONOutput(report); err != nil {
			return err
		}
	} else {
		for _, check := range report.Checks {
			fmt.Printf("%s\t%s\t%s\n", check.Status, check.Name, check.Message)
		}
		fmt.Printf("summary: ok=%d warn=%d error=%d\n", report.Summary.OK, report.Summary.Warn, report.Summary.Error)
	}
	if report.Summary.Error > 0 {
		return exitError(1)
	}
	return nil
}

func buildDoctorReport(client *api.Client, serviceName string, configPath string, socketPath string) model.DoctorReport {
	checks := []model.DoctorCheck{}
	var loadedConfig *config.Config
	add := func(id, name string, status model.DoctorStatus, message string) {
		checks = append(checks, model.DoctorCheck{ID: id, Name: name, Status: status, Message: message})
	}
	if cfg, err := config.Load(configPath); err != nil {
		add("config", "configuration", model.DoctorError, err.Error())
	} else {
		loadedConfig = cfg
		add("config", "configuration", model.DoctorOK, configPath)
	}
	if data, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		add("cgroup_v2", "cgroup v2", model.DoctorError, err.Error())
	} else if !strings.Contains(" "+string(data)+" ", " cpu ") {
		add("cgroup_v2", "cgroup v2", model.DoctorError, "cpu controller unavailable")
	} else {
		add("cgroup_v2", "cgroup v2", model.DoctorOK, "cpu controller available")
	}
	if _, err := net.DialTimeout("unix", socketPath, time.Second); err != nil {
		add("socket", "daemon socket", model.DoctorError, err.Error())
	} else {
		add("socket", "daemon socket", model.DoctorOK, socketPath)
	}
	if _, err := client.Status(); err != nil {
		add("api", "daemon api", model.DoctorError, err.Error())
	} else {
		add("api", "daemon api", model.DoctorOK, "status endpoint available")
	}
	if active, err := systemctlOutput("is-active", serviceName); err != nil {
		add("systemd_active", "systemd active", model.DoctorWarn, strings.TrimSpace(active))
	} else {
		add("systemd_active", "systemd active", model.DoctorOK, strings.TrimSpace(active))
	}
	if enabled, err := systemctlOutput("is-enabled", serviceName); err != nil {
		add("systemd_enabled", "systemd enabled", model.DoctorWarn, strings.TrimSpace(enabled))
	} else {
		add("systemd_enabled", "systemd enabled", model.DoctorOK, strings.TrimSpace(enabled))
	}
	dockerNeeded := configUsesDocker(loadedConfig)
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		status := model.DoctorWarn
		if dockerNeeded {
			status = model.DoctorError
		}
		add("docker", "docker socket", status, "not available")
	} else {
		add("docker", "docker socket", model.DoctorOK, "/var/run/docker.sock")
	}
	if temp := system.ReadCPUTemperatureCForDoctor(); temp == nil {
		add("cpu_temp", "cpu temperature", model.DoctorWarn, "no CPU temperature sensor found")
	} else {
		add("cpu_temp", "cpu temperature", model.DoctorOK, formatOptionalTemperature(temp))
	}
	if len(system.ReadRAPLDomainsForDoctor("/sys/class/powercap")) == 0 {
		add("cpu_power", "cpu power", model.DoctorWarn, "no RAPL package power source found")
	} else {
		add("cpu_power", "cpu power", model.DoctorOK, "RAPL package energy available")
	}
	report := model.DoctorReport{Checks: checks}
	for _, check := range checks {
		switch check.Status {
		case model.DoctorOK:
			report.Summary.OK++
		case model.DoctorWarn:
			report.Summary.Warn++
		case model.DoctorError:
			report.Summary.Error++
		}
	}
	return report
}

func configUsesDocker(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, rule := range cfg.Rules {
		if rule.Scope == model.ScopeDockerContainer {
			return true
		}
	}
	return false
}

func systemctlOutput(args ...string) (string, error) {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	return string(out), err
}

func formatOptionalTemperature(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.1fC", *value)
}

func formatOptionalPower(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.1fW", *value)
}

func formatSubjectLimit(subject model.Subject) string {
	if subject.CurrentLimitPct <= 0 {
		return "--"
	}
	return fmt.Sprintf("%.1f", subject.CurrentLimitPct)
}

func formatEventLimit(event model.Event) string {
	if event.LimitPct <= 0 {
		return "--"
	}
	return fmt.Sprintf("%.1f", event.LimitPct)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "--"
	}
	return t.Local().Format(time.RFC3339)
}

func parseSince(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if duration, err := time.ParseDuration(raw); err == nil {
		return time.Now().Add(-duration), nil
	}
	return time.Parse(time.RFC3339Nano, raw)
}

func writeJSONOutput(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

type exitError int

func (e exitError) Error() string {
	return fmt.Sprintf("exit status %d", int(e))
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cpuguardctl [-socket path] [-config path] [-service name] [-json] [start|stop|restart|enable|disable|check-config [path]|status|doctor|limits|logs|protected|rules|tui|reload|throttle <subject-id>|hold <subject-id>|unthrottle <subject-id>]")
	fmt.Fprintln(os.Stderr, "       cpuguardctl without a command opens the interactive TUI when run from a terminal; otherwise it prints status.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "options:")
	flag.PrintDefaults()
}
