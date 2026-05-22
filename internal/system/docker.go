package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cpuguard/internal/model"
)

type DockerClient struct {
	http *http.Client
}

func NewDockerClient() *DockerClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
		},
	}
	return &DockerClient{
		http: &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

func (c *DockerClient) available() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

func (c *DockerClient) ListContainers(ctx context.Context) ([]model.DockerContainer, error) {
	if !c.available() {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1.41/containers/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker list containers failed: %s", resp.Status)
	}
	var payload []struct {
		ID     string            `json:"Id"`
		Image  string            `json:"Image"`
		Names  []string          `json:"Names"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]model.DockerContainer, 0, len(payload))
	for _, item := range payload {
		inspect, err := c.InspectContainer(ctx, item.ID)
		if err != nil {
			continue
		}
		name := strings.TrimPrefix(firstName(item.Names), "/")
		out = append(out, model.DockerContainer{
			ID:         item.ID,
			Name:       name,
			Image:      item.Image,
			ComposeSvc: item.Labels["com.docker.compose.service"],
			Labels:     item.Labels,
			PID:        inspect.State.Pid,
			CPUQuota:   inspect.HostConfig.CPUQuota,
			CPUPeriod:  inspect.HostConfig.CPUPeriod,
		})
	}
	return out, nil
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

type containerInspect struct {
	State struct {
		Pid int `json:"Pid"`
	} `json:"State"`
	HostConfig struct {
		CPUQuota  int64 `json:"CpuQuota"`
		CPUPeriod int64 `json:"CpuPeriod"`
	} `json:"HostConfig"`
}

func (c *DockerClient) InspectContainer(ctx context.Context, id string) (*containerInspect, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1.41/containers/"+id+"/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker inspect failed: %s", resp.Status)
	}
	var payload containerInspect
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func DiscoverDockerSubjects(ctx context.Context, client *DockerClient, rule model.Rule) ([]model.DockerSubject, error) {
	containers, err := client.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	var result []model.DockerSubject
	for _, container := range containers {
		if !matchesContainerSelector(container, rule.Selector) {
			continue
		}
		if shouldExcludeContainer(container, rule.Exclude) {
			continue
		}
		result = append(result, model.DockerSubject{Rule: rule, Container: container})
	}
	return result, nil
}

func matchesContainerSelector(c model.DockerContainer, sel model.Selector) bool {
	if sel.MatchAll {
		return true
	}
	if sel.ContainerName != "" && c.Name != sel.ContainerName {
		return false
	}
	if sel.Image != "" && c.Image != sel.Image {
		return false
	}
	if sel.ComposeService != "" && c.ComposeSvc != sel.ComposeService {
		return false
	}
	for key, value := range sel.Label {
		if c.Labels[key] != value {
			return false
		}
	}
	return sel.ContainerName != "" || sel.Image != "" || sel.ComposeService != "" || len(sel.Label) > 0
}

func shouldExcludeContainer(c model.DockerContainer, sel model.Selector) bool {
	if sel.MatchAll {
		return true
	}
	if sel.ContainerName != "" && c.Name == sel.ContainerName {
		return true
	}
	if sel.Image != "" && c.Image == sel.Image {
		return true
	}
	if sel.ComposeService != "" && c.ComposeSvc == sel.ComposeService {
		return true
	}
	for key, value := range sel.Label {
		if c.Labels[key] == value {
			return true
		}
	}
	return false
}

type DockerCPUSampler struct {
	prevUsage map[string]uint64
	prevAt    map[string]time.Time
}

func NewDockerCPUSampler() *DockerCPUSampler {
	return &DockerCPUSampler{
		prevUsage: map[string]uint64{},
		prevAt:    map[string]time.Time{},
	}
}

func (s *DockerCPUSampler) Sample(subject model.DockerSubject) (float64, error) {
	if subject.Container.PID == 0 {
		return 0, nil
	}
	cgroupPath, err := readCgroupPath(subject.Container.PID)
	if err != nil {
		return 0, err
	}
	usage, err := readCgroupCPUUsage(cgroupPath)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	key := subject.SubjectID()
	prevUsage := s.prevUsage[key]
	prevAt := s.prevAt[key]
	s.prevUsage[key] = usage
	s.prevAt[key] = now
	if prevUsage == 0 || prevAt.IsZero() || usage < prevUsage {
		return 0, nil
	}
	deltaUsage := usage - prevUsage
	elapsed := now.Sub(prevAt)
	if elapsed <= 0 {
		return 0, nil
	}
	return (float64(deltaUsage) / float64(elapsed.Microseconds())) * 100, nil
}

func readCgroupCPUUsage(cgroupPath string) (uint64, error) {
	data, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(cgroupPath, "/"), "cpu.stat"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "usage_usec" {
			var v uint64
			_, err := fmt.Sscanf(fields[1], "%d", &v)
			return v, err
		}
	}
	return 0, fmt.Errorf("usage_usec not found in cpu.stat")
}

func (c *DockerClient) UpdateContainerCPU(ctx context.Context, id string, quota, period int64) error {
	payload := map[string]int64{
		"CpuQuota":  quota,
		"CpuPeriod": period,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1.41/containers/"+id+"/update", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker update failed: %s: %s", resp.Status, string(respBody))
	}
	return nil
}
