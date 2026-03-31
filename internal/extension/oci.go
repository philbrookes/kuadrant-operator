/*
Copyright 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// PullExtensionImages pulls OCI images and extracts extension binaries from them.
// Each image is expected to contain extension binaries under /extensions/<name>/<name>.
// The binaries are extracted to the given destDir preserving that structure.
func PullExtensionImages(imageRefs []string, destDir string, logger logr.Logger) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create extensions directory %s: %w", destDir, err)
	}

	for _, ref := range imageRefs {
		logger.Info("Pulling extension image", "image", ref)

		img, err := crane.Pull(ref)
		if err != nil {
			return fmt.Errorf("failed to pull image %s: %w", ref, err)
		}

		if err := extractExtensions(img, destDir, logger); err != nil {
			return fmt.Errorf("failed to extract extensions from %s: %w", ref, err)
		}

		logger.Info("Successfully extracted extensions", "image", ref, "destDir", destDir)
	}

	return nil
}

// extractExtensions walks the image layers and extracts files under /extensions/ to destDir.
func extractExtensions(img v1.Image, destDir string, logger logr.Logger) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("failed to get image layers: %w", err)
	}

	for _, layer := range layers {
		if err := extractLayer(layer, destDir, logger); err != nil {
			return err
		}
	}

	return nil
}

func extractLayer(layer v1.Layer, destDir string, logger logr.Logger) error {
	rc, err := layer.Uncompressed()
	if err != nil {
		return fmt.Errorf("failed to get uncompressed layer: %w", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Only extract files under extensions/
		name := strings.TrimPrefix(header.Name, "./")
		if !strings.HasPrefix(name, "extensions/") {
			continue
		}

		// Strip the leading "extensions/" to get the relative path
		relPath := strings.TrimPrefix(name, "extensions/")
		if relPath == "" {
			continue
		}

		targetPath := filepath.Join(destDir, relPath)

		// Prevent path traversal
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)) {
			logger.Info("Skipping suspicious path", "path", name)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", targetPath, err)
			}

			mode := os.FileMode(header.Mode)
			f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) // #nosec G115
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}

			if _, err := io.Copy(f, tr); err != nil { // #nosec G110
				f.Close()
				return fmt.Errorf("failed to write file %s: %w", targetPath, err)
			}
			f.Close()

			logger.Info("Extracted extension file", "path", targetPath, "mode", mode)
		}
	}

	return nil
}
