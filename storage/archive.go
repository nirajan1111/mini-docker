package storage

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Untar extracts a gzipped tar archive to the destination directory.
// It handles directories, regular files, symlinks, and hardlinks.
func Untar(reader io.Reader, dst string) error {
	gzr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	// Track symlinks to create after all files are extracted
	type symlink struct {
		path   string
		target string
	}
	var symlinks []symlink

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		target := filepath.Join(dst, header.Name)

		// Prevent path traversal attacks
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dst)) {
			log.Warnf("skipping potentially dangerous path: %s", header.Name)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}

		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", dir, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			f.Close()

		case tar.TypeSymlink:
			symlinks = append(symlinks, symlink{path: target, target: header.Linkname})

		case tar.TypeLink:
			// Hard link
			linkTarget := filepath.Join(dst, header.Linkname)
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", dir, err)
			}
			if err := os.Link(linkTarget, target); err != nil {
				log.Debugf("hardlink %s -> %s: %v", target, linkTarget, err)
			}

		default:
			log.Debugf("skipping unsupported tar entry type %d: %s", header.Typeflag, header.Name)
		}
	}

	// Create symlinks after all files are extracted
	for _, sl := range symlinks {
		// Remove existing file/link if it exists
		os.Remove(sl.path)
		dir := filepath.Dir(sl.path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir parent %s: %w", dir, err)
		}
		if err := os.Symlink(sl.target, sl.path); err != nil {
			log.Debugf("symlink %s -> %s: %v", sl.path, sl.target, err)
		}
	}

	return nil
}
