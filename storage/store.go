package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/config"
)

// --- Image types and store ---

type ImageMeta struct {
	Name       string    `json:"name"`
	Tag        string    `json:"tag"`
	LayerCount int       `json:"layer_count"`
	Layers     []string  `json:"layers"` // layer directory names in order
	CreatedAt  time.Time `json:"created_at"`
}

type ImageStore struct {
	root string
}

func NewImageStore() *ImageStore {
	return &ImageStore{root: config.ImageStorePath}
}

// Save writes image metadata to disk.
func (s *ImageStore) Save(meta ImageMeta) error {
	dir := filepath.Join(s.root, meta.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, meta.Tag+".json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Get retrieves image metadata by name and tag.
func (s *ImageStore) Get(name, tag string) (*ImageMeta, error) {
	path := filepath.Join(s.root, name, tag+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("image %s:%s not found", name, tag)
	}
	var meta ImageMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// List returns all images in the store.
func (s *ImageStore) List() ([]ImageMeta, error) {
	var images []ImageMeta
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if filepath.Ext(path) == ".json" && !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			var meta ImageMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				return nil
			}
			images = append(images, meta)
		}
		return nil
	})
	return images, err
}

// LayerDir returns the path where a specific layer is extracted.
// We replace "sha256:" prefix with "sha256-" to avoid colons in paths
// (OverlayFS uses ":" as separator in lowerdir option).
func (s *ImageStore) LayerDir(digest string) string {
	safe := strings.ReplaceAll(digest, ":", "-")
	return filepath.Join(s.root, "_layers", safe)
}

// EnsureDir creates a directory if it doesn't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// --- Container types and store ---

type ContainerConfig struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ImageName string    `json:"image_name"`
	ImageTag  string    `json:"image_tag"`
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	CreatedAt time.Time `json:"created_at"`
	Pid       int       `json:"pid"`
	ExitCode  *int      `json:"exit_code,omitempty"`
}

type ContainerStore struct {
	root string
}

func NewContainerStore() *ContainerStore {
	return &ContainerStore{root: config.ContainerStorePath}
}

// Create sets up the container directory structure and mounts OverlayFS.
// Returns the container directory path.
func (cs *ContainerStore) Create(containerID string, img *ImageMeta) (string, error) {
	containerDir := filepath.Join(cs.root, containerID)

	// Create container directory structure:
	// containers/{id}/upper/   — container write layer
	// containers/{id}/work/    — overlayfs workdir
	// containers/{id}/merged/  — union mount point (container sees this)
	for _, sub := range []string{"upper", "work", "merged"} {
		if err := os.MkdirAll(filepath.Join(containerDir, sub), 0755); err != nil {
			return "", fmt.Errorf("create %s dir: %w", sub, err)
		}
	}

	// Mount OverlayFS
	overlay := NewOverlay(
		NewImageStore(),
		img.Layers,
		filepath.Join(containerDir, "upper"),
		filepath.Join(containerDir, "work"),
		filepath.Join(containerDir, "merged"),
	)
	if err := overlay.Mount(); err != nil {
		return "", fmt.Errorf("overlayfs mount: %w", err)
	}

	log.Infof("container %s created with overlayfs", containerID[:12])
	return containerDir, nil
}

// SaveConfig writes container metadata to disk.
func (cs *ContainerStore) SaveConfig(containerID string, cfg ContainerConfig) error {
	dir := filepath.Join(cs.root, containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
}

// GetConfig reads container metadata.
func (cs *ContainerStore) GetConfig(containerID string) (*ContainerConfig, error) {
	path := filepath.Join(cs.root, containerID, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg ContainerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// List returns all containers.
func (cs *ContainerStore) List() ([]ContainerConfig, error) {
	entries, err := os.ReadDir(cs.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var containers []ContainerConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cfg, err := cs.GetConfig(entry.Name())
		if err != nil {
			continue
		}
		containers = append(containers, *cfg)
	}
	return containers, nil
}

// Remove deletes a container and unmounts its overlayfs.
func (cs *ContainerStore) Remove(containerID string) error {
	containerDir := filepath.Join(cs.root, containerID)

	// Try to find it by prefix if full ID not found
	if _, err := os.Stat(containerDir); os.IsNotExist(err) {
		containerDir, err = cs.findByPrefix(containerID)
		if err != nil {
			return fmt.Errorf("container %s not found", containerID)
		}
	}

	// Unmount overlayfs
	mergedDir := filepath.Join(containerDir, "merged")
	UnmountOverlay(mergedDir)

	return os.RemoveAll(containerDir)
}

// findByPrefix finds a container directory by ID prefix.
func (cs *ContainerStore) findByPrefix(prefix string) (string, error) {
	entries, err := os.ReadDir(cs.root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if len(entry.Name()) >= len(prefix) && entry.Name()[:len(prefix)] == prefix {
			return filepath.Join(cs.root, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("not found")
}
