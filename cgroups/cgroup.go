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
// Cgroups (control groups) limit, account for, and isolate resource usage:
//   - Memory: prevents container from consuming all host RAM
//   - CPU: limits CPU time the container can use
//   - PIDs: prevents fork bombs by limiting process count
type CGroup struct {
	containerID string
	root        string
}

func New(containerID string) (*CGroup, error) {
	return &CGroup{
		containerID: containerID,
		root:        config.CgroupRoot,
	}, nil
}

// Apply sets resource limits and adds the container process to the cgroup.
func (cg *CGroup) Apply(pid, memLimit, pidLimit, cpuLimit int) error {
	if err := cg.setMemoryLimit(memLimit); err != nil {
		log.Warnf("memory cgroup: %v", err)
	}
	if err := cg.setPidLimit(pidLimit); err != nil {
		log.Warnf("pids cgroup: %v", err)
	}
	if err := cg.setCPULimit(cpuLimit); err != nil {
		log.Warnf("cpu cgroup: %v", err)
	}
	if err := cg.addProcess(pid); err != nil {
		return fmt.Errorf("add process to cgroup: %w", err)
	}
	return nil
}

// setMemoryLimit writes the memory limit to the cgroup filesystem.
// Path: /sys/fs/cgroup/memory/mini-docker/{id}/memory.limit_in_bytes
func (cg *CGroup) setMemoryLimit(bytes int) error {
	dir := filepath.Join(cg.root, "memory", "mini-docker", cg.containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "memory.limit_in_bytes"), fmt.Sprintf("%d", bytes))
}

// setPidLimit limits the number of processes in the container.
// Path: /sys/fs/cgroup/pids/mini-docker/{id}/pids.max
func (cg *CGroup) setPidLimit(max int) error {
	dir := filepath.Join(cg.root, "pids", "mini-docker", cg.containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "pids.max"), fmt.Sprintf("%d", max)); err != nil {
		return err
	}
	// Enable auto-cleanup when the cgroup is empty
	return writeFile(filepath.Join(dir, "notify_on_release"), "1")
}

// setCPULimit controls how much CPU time the container gets.
// Uses CFS (Completely Fair Scheduler) bandwidth control:
//   - cpu.cfs_period_us: scheduling period (default 1 second = 1,000,000 us)
//   - cpu.cfs_quota_us: max CPU time per period (e.g., 2 CPUs = 2,000,000 us)
func (cg *CGroup) setCPULimit(cpus int) error {
	dir := filepath.Join(cg.root, "cpu", "mini-docker", cg.containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Cap at host CPU count
	hostCPUs := runtime.NumCPU()
	if cpus > hostCPUs {
		cpus = hostCPUs
	}

	period := 1000000 // 1 second in microseconds
	quota := cpus * period

	if err := writeFile(filepath.Join(dir, "cpu.cfs_period_us"), fmt.Sprintf("%d", period)); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "cpu.cfs_quota_us"), fmt.Sprintf("%d", quota))
}

// addProcess adds a process to all cgroup controllers.
func (cg *CGroup) addProcess(pid int) error {
	pidStr := fmt.Sprintf("%d", pid)
	controllers := []string{"memory", "pids", "cpu"}

	for _, ctrl := range controllers {
		procsFile := filepath.Join(cg.root, ctrl, "mini-docker", cg.containerID, "cgroup.procs")
		if err := writeFile(procsFile, pidStr); err != nil {
			log.Debugf("add pid to %s cgroup: %v", ctrl, err)
		}
	}
	return nil
}

// Cleanup removes the cgroup directories for this container.
func (cg *CGroup) Cleanup() {
	controllers := []string{"memory", "pids", "cpu"}
	for _, ctrl := range controllers {
		dir := filepath.Join(cg.root, ctrl, "mini-docker", cg.containerID)
		if err := os.RemoveAll(dir); err != nil {
			log.Debugf("cleanup cgroup %s: %v", dir, err)
		}
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
