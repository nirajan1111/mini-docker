//go:build linux

package cgroups

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/config"
)

// CGroup manages Linux cgroups for a container.
// Supports both cgroup v1 (per-controller dirs) and cgroup v2 (unified hierarchy).
//
// cgroup v1: /sys/fs/cgroup/{memory,cpu,pids}/mini-docker/{id}/
// cgroup v2: /sys/fs/cgroup/mini-docker/{id}/   (all controllers unified)
type CGroup struct {
	containerID string
	root        string
	v2          bool // true if using cgroup v2 (unified hierarchy)
}

func New(containerID string) (*CGroup, error) {
	root := config.CgroupRoot
	// Detect cgroup version: v2 has a "cgroup.controllers" file at the root
	v2 := false
	if _, err := os.Stat(filepath.Join(root, "cgroup.controllers")); err == nil {
		v2 = true
		log.Debug("detected cgroup v2 (unified hierarchy)")
	} else {
		log.Debug("detected cgroup v1")
	}

	return &CGroup{
		containerID: containerID,
		root:        root,
		v2:          v2,
	}, nil
}

// Apply sets resource limits and adds the container process to the cgroup.
func (cg *CGroup) Apply(pid, memLimit, pidLimit, cpuLimit int) error {
	if cg.v2 {
		return cg.applyV2(pid, memLimit, pidLimit, cpuLimit)
	}
	return cg.applyV1(pid, memLimit, pidLimit, cpuLimit)
}

// --- cgroup v2 (unified hierarchy) ---

func (cg *CGroup) applyV2(pid, memLimit, pidLimit, cpuLimit int) error {
	dir := filepath.Join(cg.root, "mini-docker.slice", "mini-docker-"+cg.containerID+".scope")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create cgroup dir: %w", err)
	}

	// Enable controllers by writing to parent's cgroup.subtree_control
	parentDir := filepath.Join(cg.root, "mini-docker.slice")
	writeFile(filepath.Join(parentDir, "cgroup.subtree_control"), "+memory +pids +cpu")

	// Memory limit: memory.max (v2) instead of memory.limit_in_bytes (v1)
	if err := writeFile(filepath.Join(dir, "memory.max"), fmt.Sprintf("%d", memLimit)); err != nil {
		log.Warnf("memory limit: %v", err)
	}

	// PID limit: pids.max
	if err := writeFile(filepath.Join(dir, "pids.max"), fmt.Sprintf("%d", pidLimit)); err != nil {
		log.Warnf("pids limit: %v", err)
	}

	// CPU limit: cpu.max = "$QUOTA $PERIOD"
	hostCPUs := runtime.NumCPU()
	if cpuLimit > hostCPUs {
		cpuLimit = hostCPUs
	}
	period := 100000 // 100ms
	quota := cpuLimit * period
	if err := writeFile(filepath.Join(dir, "cpu.max"), fmt.Sprintf("%d %d", quota, period)); err != nil {
		log.Warnf("cpu limit: %v", err)
	}

	// Add process to the cgroup
	if err := writeFile(filepath.Join(dir, "cgroup.procs"), fmt.Sprintf("%d", pid)); err != nil {
		return fmt.Errorf("add process to cgroup: %w", err)
	}

	log.Infof("cgroup v2: memory=%dMB pids=%d cpus=%d", memLimit/1024/1024, pidLimit, cpuLimit)
	return nil
}

// --- cgroup v1 (per-controller hierarchy) ---

func (cg *CGroup) applyV1(pid, memLimit, pidLimit, cpuLimit int) error {
	if err := cg.setMemoryLimit(memLimit); err != nil {
		log.Warnf("memory cgroup: %v", err)
	}
	if err := cg.setPidLimit(pidLimit); err != nil {
		log.Warnf("pids cgroup: %v", err)
	}
	if err := cg.setCPULimit(cpuLimit); err != nil {
		log.Warnf("cpu cgroup: %v", err)
	}
	if err := cg.addProcessV1(pid); err != nil {
		return fmt.Errorf("add process to cgroup: %w", err)
	}
	log.Infof("cgroup v1: memory=%dMB pids=%d cpus=%d", memLimit/1024/1024, pidLimit, cpuLimit)
	return nil
}

func (cg *CGroup) setMemoryLimit(bytes int) error {
	dir := filepath.Join(cg.root, "memory", "mini-docker", cg.containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "memory.limit_in_bytes"), fmt.Sprintf("%d", bytes))
}

func (cg *CGroup) setPidLimit(max int) error {
	dir := filepath.Join(cg.root, "pids", "mini-docker", cg.containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "pids.max"), fmt.Sprintf("%d", max)); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "notify_on_release"), "1")
}

func (cg *CGroup) setCPULimit(cpus int) error {
	dir := filepath.Join(cg.root, "cpu", "mini-docker", cg.containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	hostCPUs := runtime.NumCPU()
	if cpus > hostCPUs {
		cpus = hostCPUs
	}
	period := 1000000
	quota := cpus * period
	if err := writeFile(filepath.Join(dir, "cpu.cfs_period_us"), fmt.Sprintf("%d", period)); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "cpu.cfs_quota_us"), fmt.Sprintf("%d", quota))
}

func (cg *CGroup) addProcessV1(pid int) error {
	pidStr := fmt.Sprintf("%d", pid)
	for _, ctrl := range []string{"memory", "pids", "cpu"} {
		procsFile := filepath.Join(cg.root, ctrl, "mini-docker", cg.containerID, "cgroup.procs")
		if err := writeFile(procsFile, pidStr); err != nil {
			log.Debugf("add pid to %s cgroup: %v", ctrl, err)
		}
	}
	return nil
}

// Cleanup removes the cgroup directories for this container.
func (cg *CGroup) Cleanup() {
	if cg.v2 {
		dir := filepath.Join(cg.root, "mini-docker.slice", "mini-docker-"+cg.containerID+".scope")
		os.RemoveAll(dir)
		return
	}
	for _, ctrl := range []string{"memory", "pids", "cpu"} {
		dir := filepath.Join(cg.root, ctrl, "mini-docker", cg.containerID)
		os.RemoveAll(dir)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
