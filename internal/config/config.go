package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"cpuguard/internal/model"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Global GlobalConfig `yaml:"global"`
	Rules  []RuleConfig `yaml:"rules"`
}

type GlobalConfig struct {
	SampleInterval Duration        `yaml:"sample_interval"`
	StateDB        string          `yaml:"state_db"`
	SocketPath     string          `yaml:"socket_path"`
	LogLevel       string          `yaml:"log_level"`
	EventRetention *EventRetention `yaml:"event_retention"`
}

type EventRetention struct {
	MaxEvents int      `yaml:"max_events"`
	MaxAge    Duration `yaml:"max_age"`
}

type RuleConfig struct {
	ID        string         `yaml:"id"`
	Scope     model.Scope    `yaml:"scope"`
	Selector  SelectorConfig `yaml:"selector"`
	Exclude   SelectorConfig `yaml:"exclude"`
	Threshold Threshold      `yaml:"threshold"`
	Action    Action         `yaml:"action"`
	Recovery  Recovery       `yaml:"recovery"`
	Cooldown  Duration       `yaml:"cooldown"`
}

type SelectorConfig struct {
	MatchAll       bool              `yaml:"match_all"`
	ProcessName    string            `yaml:"process_name"`
	CmdlineRegex   string            `yaml:"cmdline_regex"`
	User           string            `yaml:"user"`
	UID            *int              `yaml:"uid"`
	MinPID         int               `yaml:"min_pid"`
	ContainerName  string            `yaml:"container_name"`
	Image          string            `yaml:"image"`
	ComposeService string            `yaml:"compose_service"`
	Label          map[string]string `yaml:"label"`
}

type Threshold struct {
	CPUPercentTotalGT float64  `yaml:"cpu_percent_total_gt"`
	Window            Duration `yaml:"window"`
	Ratio             float64  `yaml:"ratio"`
}

type Action struct {
	Type        string  `yaml:"type"`
	Factor      float64 `yaml:"factor"`
	MinLimitPct float64 `yaml:"min_limit_pct"`
}

type Recovery struct {
	CPUPercentTotalLT float64  `yaml:"cpu_percent_total_lt"`
	Window            Duration `yaml:"window"`
	Ratio             float64  `yaml:"ratio"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be scalar")
	}
	var raw string
	if err := node.Decode(&raw); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Global.SampleInterval.Duration == 0 {
		c.Global.SampleInterval.Duration = 5 * time.Second
	}
	if c.Global.StateDB == "" {
		c.Global.StateDB = "/var/lib/cpuguard/state.db"
	}
	if c.Global.SocketPath == "" {
		c.Global.SocketPath = "/run/cpuguardd.sock"
	}
	if c.Global.LogLevel == "" {
		c.Global.LogLevel = "info"
	}
	if c.Global.EventRetention == nil {
		c.Global.EventRetention = &EventRetention{
			MaxEvents: 10000,
			MaxAge:    Duration{Duration: 30 * 24 * time.Hour},
		}
	}
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.Threshold.CPUPercentTotalGT == 0 {
			r.Threshold.CPUPercentTotalGT = 90
		}
		if r.Threshold.Window.Duration == 0 {
			r.Threshold.Window.Duration = 5 * time.Minute
		}
		if r.Threshold.Ratio == 0 {
			r.Threshold.Ratio = 0.9
		}
		if r.Action.Type == "" {
			r.Action.Type = "throttle_cpu"
		}
		if r.Action.Factor == 0 {
			r.Action.Factor = 0.5
		}
		if r.Action.MinLimitPct == 0 {
			r.Action.MinLimitPct = 20
		}
		if r.Recovery.CPUPercentTotalLT == 0 {
			r.Recovery.CPUPercentTotalLT = 70
		}
		if r.Recovery.Window.Duration == 0 {
			r.Recovery.Window.Duration = 10 * time.Minute
		}
		if r.Recovery.Ratio == 0 {
			r.Recovery.Ratio = 0.9
		}
		if r.Cooldown.Duration == 0 {
			r.Cooldown.Duration = 15 * time.Minute
		}
	}
}

func (c *Config) validate() error {
	if len(c.Rules) == 0 {
		return fmt.Errorf("at least one rule is required")
	}
	seen := map[string]struct{}{}
	for _, r := range c.Rules {
		if r.ID == "" {
			return fmt.Errorf("rule id is required")
		}
		if _, ok := seen[r.ID]; ok {
			return fmt.Errorf("duplicate rule id %q", r.ID)
		}
		seen[r.ID] = struct{}{}
		if r.Scope != model.ScopeHostProcess && r.Scope != model.ScopeDockerContainer {
			return fmt.Errorf("rule %q has invalid scope %q", r.ID, r.Scope)
		}
		if r.Action.Type != "throttle_cpu" {
			return fmt.Errorf("rule %q has unsupported action type %q", r.ID, r.Action.Type)
		}
		if r.Action.Factor <= 0 || r.Action.Factor >= 1 {
			return fmt.Errorf("rule %q action.factor must be > 0 and < 1", r.ID)
		}
		if r.Action.MinLimitPct <= 0 {
			return fmt.Errorf("rule %q action.min_limit_pct must be > 0", r.ID)
		}
		if r.Threshold.CPUPercentTotalGT <= 0 {
			return fmt.Errorf("rule %q threshold cpu_percent_total_gt must be > 0", r.ID)
		}
		if r.Recovery.CPUPercentTotalLT <= 0 || r.Recovery.CPUPercentTotalLT >= r.Threshold.CPUPercentTotalGT {
			return fmt.Errorf("rule %q recovery cpu_percent_total_lt must be > 0 and below trigger threshold", r.ID)
		}
		if r.Threshold.Ratio <= 0 || r.Threshold.Ratio > 1 || r.Recovery.Ratio <= 0 || r.Recovery.Ratio > 1 {
			return fmt.Errorf("rule %q ratios must be > 0 and <= 1", r.ID)
		}
		if r.Threshold.Window.Duration < c.Global.SampleInterval.Duration || r.Recovery.Window.Duration < c.Global.SampleInterval.Duration {
			return fmt.Errorf("rule %q windows must be >= sample_interval", r.ID)
		}
		for label, expr := range map[string]string{
			"cmdline_regex":         r.Selector.CmdlineRegex,
			"exclude.cmdline_regex": r.Exclude.CmdlineRegex,
		} {
			if expr == "" {
				continue
			}
			if _, err := regexp.Compile(expr); err != nil {
				return fmt.Errorf("rule %q has invalid %s: %w", r.ID, label, err)
			}
		}
		if !selectorPresent(r) {
			return fmt.Errorf("rule %q selector must not be empty; use selector.match_all: true for global monitoring", r.ID)
		}
		if r.Scope == model.ScopeHostProcess && !hostSelectorPresent(r) {
			return fmt.Errorf("rule %q host_process selector needs process_name, cmdline_regex, or user", r.ID)
		}
		if r.Scope == model.ScopeDockerContainer && !dockerSelectorPresent(r) {
			return fmt.Errorf("rule %q docker_container selector needs container_name, image, compose_service, or label", r.ID)
		}
	}
	if c.Global.EventRetention.MaxEvents < 0 {
		return fmt.Errorf("global.event_retention.max_events must be >= 0")
	}
	if c.Global.EventRetention.MaxAge.Duration < 0 {
		return fmt.Errorf("global.event_retention.max_age must be >= 0")
	}
	return nil
}

func selectorPresent(r RuleConfig) bool {
	return hostSelectorPresent(r) || dockerSelectorPresent(r)
}

func hostSelectorPresent(r RuleConfig) bool {
	return r.Selector.MatchAll || r.Selector.ProcessName != "" || r.Selector.CmdlineRegex != "" || r.Selector.User != "" || r.Selector.UID != nil || r.Selector.MinPID > 0
}

func dockerSelectorPresent(r RuleConfig) bool {
	return r.Selector.MatchAll || r.Selector.ContainerName != "" || r.Selector.Image != "" || r.Selector.ComposeService != "" || len(r.Selector.Label) > 0
}

func (c *Config) RulesAsModel() []model.Rule {
	out := make([]model.Rule, 0, len(c.Rules))
	for _, rule := range c.Rules {
		out = append(out, model.Rule{
			ID:    rule.ID,
			Scope: rule.Scope,
			Selector: model.Selector{
				MatchAll:       rule.Selector.MatchAll,
				ProcessName:    rule.Selector.ProcessName,
				CmdlineRegex:   rule.Selector.CmdlineRegex,
				User:           rule.Selector.User,
				UID:            rule.Selector.UID,
				MinPID:         rule.Selector.MinPID,
				ContainerName:  rule.Selector.ContainerName,
				Image:          rule.Selector.Image,
				ComposeService: rule.Selector.ComposeService,
				Label:          rule.Selector.Label,
			},
			Exclude: model.Selector{
				MatchAll:       rule.Exclude.MatchAll,
				ProcessName:    rule.Exclude.ProcessName,
				CmdlineRegex:   rule.Exclude.CmdlineRegex,
				User:           rule.Exclude.User,
				UID:            rule.Exclude.UID,
				MinPID:         rule.Exclude.MinPID,
				ContainerName:  rule.Exclude.ContainerName,
				Image:          rule.Exclude.Image,
				ComposeService: rule.Exclude.ComposeService,
				Label:          rule.Exclude.Label,
			},
			Threshold: model.Threshold{
				CPUPercentTotalGT: rule.Threshold.CPUPercentTotalGT,
				Window:            rule.Threshold.Window.Duration,
				Ratio:             rule.Threshold.Ratio,
			},
			Action: model.Action{
				Type:        rule.Action.Type,
				Factor:      rule.Action.Factor,
				MinLimitPct: rule.Action.MinLimitPct,
			},
			Recovery: model.Recovery{
				CPUPercentTotalLT: rule.Recovery.CPUPercentTotalLT,
				Window:            rule.Recovery.Window.Duration,
				Ratio:             rule.Recovery.Ratio,
			},
			Cooldown: rule.Cooldown.Duration,
		})
	}
	return out
}
