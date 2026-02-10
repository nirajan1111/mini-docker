//go:build !linux

package runtime

import (
	"fmt"

	"github.com/nirajansah/mini-docker/storage"
)

type Runtime struct {
	imageStore     *storage.ImageStore
	containerStore *storage.ContainerStore
}

type RunRequest struct {
	ImageName     string
	ImageTag      string
	Command       string
	Args          []string
	ContainerName string
	MemoryLimit   int
	PidLimit      int
	CPULimit      int
}

type InitRequest struct {
	ContainerID   string
	ContainerName string
	ImageName     string
	ImageTag      string
	Command       string
	Args          []string
	MemoryLimit   int
	PidLimit      int
	CPULimit      int
}

func New() *Runtime {
	return &Runtime{
		imageStore:     storage.NewImageStore(),
		containerStore: storage.NewContainerStore(),
	}
}

func (r *Runtime) Run(req RunRequest) (int, error) {
	return 1, fmt.Errorf("mini-docker only runs on Linux — use 'make build-linux' and run in a Linux VM")
}

func (r *Runtime) InitContainer(req InitRequest) int {
	fmt.Println("mini-docker only runs on Linux")
	return 1
}

func (r *Runtime) Exec(containerID, command string, args []string) error {
	return fmt.Errorf("mini-docker only runs on Linux")
}
