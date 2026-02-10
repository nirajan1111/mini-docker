//go:build linux

package storage

import (
	"fmt"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
)

// Overlay represents an OverlayFS mount for a container.
// OverlayFS is a union filesystem that layers multiple directories:
//   - lowerdir: read-only image layers (stacked, rightmost is bottom)
//   - upperdir: writable container layer (copy-on-write)
//   - workdir:  scratch space for overlayfs internals
//   - merged:   the unified view the container sees
type Overlay struct {
	imageStore *ImageStore
	layers     []string // layer digests, bottom to top
	upperDir   string
	workDir    string
	mergedDir  string
}

func NewOverlay(imageStore *ImageStore, layers []string, upper, work, merged string) *Overlay {
	return &Overlay{
		imageStore: imageStore,
		layers:     layers,
		upperDir:   upper,
		workDir:    work,
		mergedDir:  merged,
	}
}

// Mount mounts the overlay filesystem.
// The lowerdir option stacks layers with ":" separator — leftmost is topmost layer.
func (o *Overlay) Mount() error {
	if len(o.layers) == 0 {
		return fmt.Errorf("no image layers to mount")
	}

	// Build lowerdir string: layers are listed top-to-bottom (reverse order)
	// OverlayFS lowerdir format: topmost:...:bottommost
	lowerDirs := make([]string, len(o.layers))
	for i, digest := range o.layers {
		// Reverse: last layer in our list is the topmost
		lowerDirs[len(o.layers)-1-i] = o.imageStore.LayerDir(digest)
	}
	lowerDir := strings.Join(lowerDirs, ":")

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		lowerDir, o.upperDir, o.workDir)

	log.Debugf("mounting overlayfs: %s", opts)

	if err := syscall.Mount("overlay", o.mergedDir, "overlay", 0, opts); err != nil {
		return fmt.Errorf("mount overlay on %s: %w", o.mergedDir, err)
	}

	log.Infof("overlayfs mounted at %s (%d layers)", o.mergedDir, len(o.layers))
	return nil
}

// UnmountOverlay unmounts an overlay filesystem at the given path.
func UnmountOverlay(mergedDir string) {
	if err := syscall.Unmount(mergedDir, syscall.MNT_DETACH); err != nil {
		log.Debugf("unmount %s: %v", mergedDir, err)
	}
}
