//go:build linux

package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/storage"
)

// InitContainer is called inside the new namespaces (by the _child command).
// It sets up the container filesystem, performs pivot_root, and exec's the user command.
func (r *Runtime) InitContainer(req InitRequest) int {
	log.Debugf("initializing container %s", req.ContainerID[:12])

	// 1. Find the image
	img, err := r.imageStore.Get(req.ImageName, req.ImageTag)
	if err != nil {
		log.Errorf("image not found: %v", err)
		return 1
	}

	// 2. Create container from image using OverlayFS
	contStore := storage.NewContainerStore()
	containerDir, err := contStore.Create(req.ContainerID, img)
	if err != nil {
		log.Errorf("failed to create container: %v", err)
		return 1
	}

	// 3. Save container metadata
	contStore.SaveConfig(req.ContainerID, storage.ContainerConfig{
		ID:        req.ContainerID,
		Name:      req.ContainerName,
		ImageName: req.ImageName,
		ImageTag:  req.ImageTag,
		Command:   req.Command,
		Args:      req.Args,
		CreatedAt: time.Now(),
		Pid:       os.Getpid(),
	})

	mergedDir := filepath.Join(containerDir, "merged")

	// 4. Set up device bind mounts inside the merged rootfs
	if err := setupDevices(mergedDir); err != nil {
		log.Errorf("failed to setup devices: %v", err)
		return 1
	}

	// 5. Perform pivot_root (more secure than chroot)
	if err := pivotRoot(mergedDir); err != nil {
		log.Errorf("pivot_root failed: %v", err)
		return 1
	}

	// 6. Set hostname to container ID (short form)
	hostname := req.ContainerID[:12]
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		log.Warnf("failed to set hostname: %v", err)
	}

	// 7. Mount /proc so ps and /proc work inside container
	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		log.Warnf("failed to mount /proc: %v", err)
	}

	// 8. Mount /sys as read-only
	if err := syscall.Mount("sysfs", "/sys", "sysfs", syscall.MS_RDONLY, ""); err != nil {
		log.Warnf("failed to mount /sys: %v", err)
	}

	// 9. Mount tmpfs on /dev so we have a writable /dev
	// (the devices were bind-mounted before pivot_root)
	if err := mountDev(); err != nil {
		log.Warnf("failed to mount /dev: %v", err)
	}

	// 10. Execute the requested command
	log.Infof("executing: %s %v", req.Command, req.Args)
	argv := append([]string{req.Command}, req.Args...)
	if err := syscall.Exec(req.Command, argv, os.Environ()); err != nil {
		log.Errorf("exec failed: %v", err)
		return 1
	}

	// syscall.Exec replaces the process, so we should never reach here
	return 0
}

// pivotRoot switches the root filesystem to newRoot using pivot_root(2).
// This is more secure than chroot because it actually moves the old root away.
func pivotRoot(newRoot string) error {
	// pivot_root requires the new root to be a mount point
	if err := syscall.Mount(newRoot, newRoot, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount new root: %w", err)
	}

	// Create directory for the old root
	putOld := filepath.Join(newRoot, ".pivot_old")
	if err := os.MkdirAll(putOld, 0700); err != nil {
		return fmt.Errorf("create pivot_old dir: %w", err)
	}

	// pivot_root moves the current root to putOld and makes newRoot the new root
	if err := syscall.PivotRoot(newRoot, putOld); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	// Change to the new root
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// Unmount the old root (it's now at /.pivot_old)
	if err := syscall.Unmount("/.pivot_old", syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root: %w", err)
	}

	// Remove the old root mount point
	return os.RemoveAll("/.pivot_old")
}

// setupDevices creates essential device nodes inside the container rootfs.
func setupDevices(rootfs string) error {
	devDir := filepath.Join(rootfs, "dev")
	if err := os.MkdirAll(devDir, 0755); err != nil {
		return err
	}

	devices := []struct {
		path string
		src  string
	}{
		{filepath.Join(devDir, "null"), "/dev/null"},
		{filepath.Join(devDir, "zero"), "/dev/zero"},
		{filepath.Join(devDir, "random"), "/dev/random"},
		{filepath.Join(devDir, "urandom"), "/dev/urandom"},
	}

	for _, dev := range devices {
		// Create the mount point file
		f, err := os.Create(dev.path)
		if err != nil {
			return fmt.Errorf("create %s: %w", dev.path, err)
		}
		f.Close()

		// Bind mount the host device
		if err := syscall.Mount(dev.src, dev.path, "bind", syscall.MS_BIND, ""); err != nil {
			log.Warnf("bind mount %s: %v", dev.path, err)
		}
	}
	return nil
}

// mountDev sets up a minimal /dev with essential device nodes
func mountDev() error {
	// Mount devpts for terminal support
	ptsDir := "/dev/pts"
	if err := os.MkdirAll(ptsDir, 0755); err != nil {
		return err
	}
	if err := syscall.Mount("devpts", ptsDir, "devpts", 0, ""); err != nil {
		log.Warnf("failed to mount devpts: %v", err)
	}
	return nil
}
