package model

import "time"

type Scope string

const (
	ScopeHostProcess     Scope = "host_process"
	ScopeDockerContainer       = "docker_container"
)

type SubjectState string

const (
	StateObserving  SubjectState = "observing"
	StateThrottled  SubjectState = "throttled"
	StateManualHold              = "manual_hold"
)

type Rule struct {
	ID        string
	Scope     Scope
	Selector  Selector
	Exclude   Selector
	Threshold Threshold
	Action    Action
	Recovery  Recovery
	Cooldown  time.Duration
}

type Selector struct {
	MatchAll       bool
	ProcessName    string
	CmdlineRegex   string
	User           string
	UID            *int
	MinPID         int
	ContainerName  string
	Image          string
	ComposeService string
	Label          map[string]string
}

type Threshold struct {
	CPUPercentTotalGT float64
	Window            time.Duration
	Ratio             float64
}

type Action struct {
	Type        string
	Factor      float64
	MinLimitPct float64
}

type Recovery struct {
	CPUPercentTotalLT float64
	Window            time.Duration
	Ratio             float64
}

type Subject struct {
	ID                      string       `json:"id"`
	RuleID                  string       `json:"rule_id"`
	Scope                   Scope        `json:"scope"`
	TargetRef               string       `json:"target_ref"`
	DisplayName             string       `json:"display_name"`
	State                   SubjectState `json:"state"`
	LastCPUPct              float64      `json:"last_cpu_pct"`
	CurrentLimitPct         float64      `json:"current_limit_pct"`
	OriginalCgroupPath      string       `json:"original_cgroup_path,omitempty"`
	ManagedCgroupPath       string       `json:"managed_cgroup_path,omitempty"`
	HostOriginalCgroups     string       `json:"host_original_cgroups,omitempty"`
	DockerContainerID       string       `json:"docker_container_id,omitempty"`
	DockerOriginalCPUQuota  int64        `json:"docker_original_cpu_quota,omitempty"`
	DockerOriginalCPUPeriod int64        `json:"docker_original_cpu_period,omitempty"`
	CooldownUntil           time.Time    `json:"cooldown_until,omitempty"`
	UpdatedAt               time.Time    `json:"updated_at"`
}

type SystemMetrics struct {
	CPUPercent float64   `json:"cpu_pct"`
	CPUCount   int       `json:"cpu_count"`
	Load1      float64   `json:"load1"`
	Load5      float64   `json:"load5"`
	Load15     float64   `json:"load15"`
	CPUTempC   *float64  `json:"cpu_temp_c,omitempty"`
	CPUPowerW  *float64  `json:"cpu_power_w,omitempty"`
	SampledAt  time.Time `json:"sampled_at"`
}

type DaemonStatus struct {
	System  SystemMetrics  `json:"system"`
	Summary SubjectSummary `json:"summary"`
}

type SubjectQuery struct {
	Offset     int
	Limit      int
	Sort       string
	Dir        string
	SelectedID string
}

type SubjectPage struct {
	Total         int       `json:"total"`
	Offset        int       `json:"offset"`
	Limit         int       `json:"limit"`
	Items         []Subject `json:"items"`
	SelectedIndex int       `json:"selected_index"`
	Selected      *Subject  `json:"selected,omitempty"`
}

type SubjectSummary struct {
	Total     int     `json:"total"`
	Throttled int     `json:"throttled"`
	Held      int     `json:"held"`
	MaxCPUPct float64 `json:"max_cpu_pct"`
	AvgCPUPct float64 `json:"avg_cpu_pct"`
}

type EventQuery struct {
	SubjectID string
	Type      string
	Since     time.Time
	Limit     int
}

type Event struct {
	ID        int64     `json:"id"`
	SubjectID string    `json:"subject_id"`
	RuleID    string    `json:"rule_id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	CPUPct    float64   `json:"cpu_pct"`
	LimitPct  float64   `json:"limit_pct"`
	CreatedAt time.Time `json:"created_at"`
}

type ProtectedPolicy struct {
	PIDMin           int      `json:"pid_min"`
	ProcessNames     []string `json:"process_names"`
	ProcessPrefixes  []string `json:"process_prefixes"`
	CmdlineFragments []string `json:"cmdline_fragments"`
	CgroupPrefixes   []string `json:"cgroup_prefixes"`
	CgroupFragments  []string `json:"cgroup_fragments"`
	Notes            []string `json:"notes"`
}

type DoctorStatus string

const (
	DoctorOK    DoctorStatus = "ok"
	DoctorWarn  DoctorStatus = "warn"
	DoctorError DoctorStatus = "error"
)

type DoctorCheck struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Status  DoctorStatus `json:"status"`
	Message string       `json:"message"`
}

type DoctorSummary struct {
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
}

type DoctorReport struct {
	Checks  []DoctorCheck `json:"checks"`
	Summary DoctorSummary `json:"summary"`
}

type HostProcess struct {
	PID        int
	PPID       int
	Name       string
	Cmdline    string
	UID        int
	User       string
	CgroupPath string
	Children   []int
}

type HostSubject struct {
	Rule        Rule
	Root        HostProcess
	Descendants []int
}

func (s HostSubject) SubjectID() string {
	return "host/" + s.Rule.ID + "/" + itoa(s.Root.PID)
}

func (s HostSubject) TargetReference() string {
	return s.Root.Name + "#" + itoa(s.Root.PID)
}

type DockerContainer struct {
	ID         string
	Name       string
	Image      string
	ComposeSvc string
	Labels     map[string]string
	PID        int
	CPUQuota   int64
	CPUPeriod  int64
}

type DockerSubject struct {
	Rule      Rule
	Container DockerContainer
}

func (s DockerSubject) SubjectID() string {
	return "docker/" + s.Rule.ID + "/" + s.Container.ID
}

func (s DockerSubject) TargetReference() string {
	return s.Container.Name
}

// Keep the helper local to avoid importing strconv in every caller.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
