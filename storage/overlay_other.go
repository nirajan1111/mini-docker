//go:build !linux

package storage

import "fmt"

type Overlay struct {
	imageStore *ImageStore
	layers     []string
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

func (o *Overlay) Mount() error {
	return fmt.Errorf("overlayfs only available on Linux")
}

func UnmountOverlay(mergedDir string) {}
