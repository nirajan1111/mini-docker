package image

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/nirajansah/mini-docker/config"
	"github.com/nirajansah/mini-docker/storage"
)

// Registry handles pulling images from Docker Hub using the V2 registry API.
// Unlike gocker (which uses a third-party library), we implement the protocol directly.
type Registry struct {
	client   *http.Client
	baseURL  string
	imgStore *storage.ImageStore
}

// tokenResponse from Docker Hub auth service
type tokenResponse struct {
	Token string `json:"token"`
}

// manifestList represents a Docker manifest list (fat manifest) for multi-arch images.
// When you pull "alpine", Docker Hub returns this — a list of manifests for each architecture.
type manifestList struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Manifests     []struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
		Platform  struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		} `json:"platform"`
	} `json:"manifests"`
}

// manifestV2 represents an OCI image manifest with layers.
type manifestV2 struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Layers        []struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

func NewRegistry() *Registry {
	return &Registry{
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		baseURL:  config.DefaultRegistry,
		imgStore: storage.NewImageStore(),
	}
}

// Pull downloads an image and all its layers from Docker Hub.
func (r *Registry) Pull(name, tag string) error {
	// Docker Hub requires "library/" prefix for official images
	repoName := name
	if !strings.Contains(name, "/") {
		repoName = "library/" + name
	}

	log.Infof("pulling %s:%s from %s", repoName, tag, r.baseURL)

	// Step 1: Get auth token for this repository
	token, err := r.getToken(repoName)
	if err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}
	log.Debug("obtained auth token")

	// Step 2: Fetch the image manifest (handling multi-arch manifest lists)
	manifest, err := r.resolveManifest(repoName, tag, token)
	if err != nil {
		return fmt.Errorf("manifest fetch failed: %w", err)
	}
	log.Infof("manifest has %d layers", len(manifest.Layers))

	// Step 3: Download and extract ALL layers (gocker only does 1)
	layerDigests := make([]string, 0, len(manifest.Layers))
	for i, layer := range manifest.Layers {
		digest := layer.Digest
		shortDigest := digest
		if len(digest) > 19 {
			shortDigest = digest[:19]
		}

		// Check if layer already exists (layer caching)
		layerDir := r.imgStore.LayerDir(digest)
		if _, err := os.Stat(layerDir); err == nil {
			log.Infof("layer %d/%d: %s (cached)", i+1, len(manifest.Layers), shortDigest)
			layerDigests = append(layerDigests, digest)
			continue
		}

		log.Infof("layer %d/%d: %s (%.2f MB)", i+1, len(manifest.Layers), shortDigest, float64(layer.Size)/1024/1024)

		// Download the layer blob
		reader, err := r.getBlob(repoName, digest, token)
		if err != nil {
			return fmt.Errorf("download layer %s: %w", shortDigest, err)
		}

		// Extract layer to storage
		if err := storage.EnsureDir(layerDir); err != nil {
			reader.Close()
			return fmt.Errorf("create layer dir: %w", err)
		}

		if err := storage.Untar(reader, layerDir); err != nil {
			reader.Close()
			return fmt.Errorf("extract layer %s: %w", shortDigest, err)
		}
		reader.Close()

		layerDigests = append(layerDigests, digest)
	}

	// Step 4: Save image metadata
	meta := storage.ImageMeta{
		Name:       name,
		Tag:        tag,
		LayerCount: len(layerDigests),
		Layers:     layerDigests,
		CreatedAt:  time.Now(),
	}
	if err := r.imgStore.Save(meta); err != nil {
		return fmt.Errorf("save image metadata: %w", err)
	}

	log.Infof("image %s:%s pulled successfully (%d layers)", name, tag, len(layerDigests))
	return nil
}

// resolveManifest handles both single-arch and multi-arch images.
// Multi-arch images return a manifest list — we pick the right architecture and fetch its manifest.
func (r *Registry) resolveManifest(repo, tag, token string) (*manifestV2, error) {
	// Request with both manifest list and v2 manifest accept types
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", r.baseURL, repo, tag)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
	}, ", "))

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest fetch failed (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	log.Debugf("manifest content-type: %s", contentType)

	// Check if this is a manifest list (multi-arch)
	if strings.Contains(contentType, "manifest.list") || strings.Contains(contentType, "image.index") {
		var ml manifestList
		if err := json.Unmarshal(body, &ml); err != nil {
			return nil, fmt.Errorf("decode manifest list: %w", err)
		}

		// Find manifest for our architecture
		arch := runtime.GOARCH
		// Map Go arch names to Docker arch names
		if arch == "amd64" {
			arch = "amd64"
		}

		log.Infof("multi-arch image detected, selecting %s/linux", arch)

		var digest string
		for _, m := range ml.Manifests {
			if m.Platform.Architecture == arch && m.Platform.OS == "linux" {
				digest = m.Digest
				break
			}
		}
		if digest == "" {
			// List available architectures for helpful error
			var available []string
			for _, m := range ml.Manifests {
				available = append(available, fmt.Sprintf("%s/%s", m.Platform.OS, m.Platform.Architecture))
			}
			return nil, fmt.Errorf("no manifest for linux/%s — available: %s", arch, strings.Join(available, ", "))
		}

		log.Debugf("resolved architecture manifest: %s", digest[:19])

		// Fetch the actual manifest for this architecture
		return r.getManifest(repo, digest, token)
	}

	// It's already a direct manifest
	var manifest manifestV2
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	return &manifest, nil
}

// getManifest fetches a specific image manifest by digest or tag.
func (r *Registry) getManifest(repo, ref, token string) (*manifestV2, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", r.baseURL, repo, ref)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest fetch failed (status %d): %s", resp.StatusCode, string(body))
	}

	var manifest manifestV2
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	return &manifest, nil
}

// getToken obtains a Bearer token from Docker Hub's auth service.
// Docker Hub uses token-based auth — you first request a token, then use it for all API calls.
func (r *Registry) getToken(repo string) (string, error) {
	url := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", repo)

	resp, err := r.client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}

	return tokenResp.Token, nil
}

// getBlob downloads a layer blob (gzipped tar) from the registry.
func (r *Registry) getBlob(repo, digest, token string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", r.baseURL, repo, digest)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("blob download failed (status %d)", resp.StatusCode)
	}

	return resp.Body, nil
}
