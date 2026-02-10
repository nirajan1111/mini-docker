package image

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// manifestV2 represents an OCI image manifest.
type manifestV2 struct {
	SchemaVersion int `json:"schemaVersion"`
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

	// Step 2: Fetch the image manifest
	manifest, err := r.getManifest(repoName, tag, token)
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

		// Check if layer already exists
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

// getManifest fetches the image manifest which lists all layers.
func (r *Registry) getManifest(repo, tag, token string) (*manifestV2, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", r.baseURL, repo, tag)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// Request V2 manifest format
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("blob download failed (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}
