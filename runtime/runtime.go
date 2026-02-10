//go:build linux

package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/cgroups"
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

// Run creates and starts a new container. It:
// 1. Generates a container ID
// 2. Re-executes itself with _child command inside new namespaces
// 3. Applies cgroup limits to the child process
// 4. Waits for the child to exit
func (r *Runtime) Run(req RunRequest) (int, error) {
	// Verify image exists
	img, err := r.imageStore.Get(req.ImageName, req.ImageTag)
	if err != nil {
		return 1, fmt.Errorf("image %s:%s not found — run 'mini-docker pull %s:%s' first: %w",
			req.ImageName, req.ImageTag, req.ImageName, req.ImageTag, err)
	}
	log.Infof("using image %s:%s (%d layers)", img.Name, img.Tag, img.LayerCount)

	// Generate container ID
	containerID := strings.ReplaceAll(uuid.New().String(), "-", "")
	if req.ContainerName == "" {
		req.ContainerName = containerID[:12]
	}

	// Re-exec ourselves with _child inside new namespaces
	// This is the key trick: /proc/self/exe points to the current binary
	self, err := os.Executable()
	if err != nil {
		return 1, fmt.Errorf("cannot find self executable: %w", err)
	}

	argsStr := strings.Join(req.Args, "\x00")
	childArgs := []string{
		"_child",
		"--id", containerID,
		"--image", req.ImageName,
		"--tag", req.ImageTag,
		"--cmd", req.Command,
		"--args", argsStr,
		"--name", req.ContainerName,
		"--mem", fmt.Sprintf("%d", req.MemoryLimit),
		"--pids", fmt.Sprintf("%d", req.PidLimit),
		"--cpus", fmt.Sprintf("%d", req.CPULimit),
	}

	cmd := exec.Command(self, childArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Create new namespaces for the child process
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | // new hostname
			syscall.CLONE_NEWPID | // new PID namespace
			syscall.CLONE_NEWNS | // new mount namespace
			syscall.CLONE_NEWIPC | // new IPC namespace
			syscall.CLONE_NEWNET, // new network namespace
		Unshareflags: syscall.CLONE_NEWNS,
	}

	log.Infof("starting container %s", containerID[:12])

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("failed to start container process: %w", err)
	}

	pid := cmd.Process.Pid
	log.Debugf("container process PID: %d", pid)

	// Apply cgroup resource limits
	cg, err := cgroups.New(containerID)
	if err != nil {
		log.Warnf("failed to create cgroup: %v", err)
	} else {
		defer cg.Cleanup()
		if err := cg.Apply(pid, req.MemoryLimit, req.PidLimit, req.CPULimit); err != nil {
			log.Warnf("failed to apply cgroup limits: %v", err)
		}
	}

	// TODO: Phase 6 — set up networking (veth pair, bridge, IP assignment)

	// Wait for the container process to finish
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("container process error: %w", err)
	}

	return 0, nil
}

// Exec joins a running container's namespaces and runs a command.
// This will be fully implemented in Phase 7.
func (r *Runtime) Exec(containerID, command string, args []string) error {
	return fmt.Errorf("exec not yet implemented — coming in Phase 7")
}
