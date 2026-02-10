//go:build !linux

package cgroups

import "fmt"

type CGroup struct {
	containerID string
}

func New(containerID string) (*CGroup, error) {
	return &CGroup{containerID: containerID}, nil
}

func (cg *CGroup) Apply(pid, memLimit, pidLimit, cpuLimit int) error {
	return fmt.Errorf("cgroups only available on Linux")
}

func (cg *CGroup) Cleanup() {}
