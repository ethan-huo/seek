package source

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ImageFile represents a single image file found on disk.
type ImageFile struct {
	Path        string
	Name        string // filename without extension
	ContentHash string
	Mtime       float64
}

// imageExtensions are the file extensions we index.
var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true,
	".webp": true, ".gif": true, ".bmp": true,
	".tiff": true, ".tif": true, ".svg": true,
}

// ScanImages walks a directory and returns all image files.
func ScanImages(dir string) ([]ImageFile, error) {
	var files []ImageFile

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !imageExtensions[ext] {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		hash := sha256.Sum256(data)

		base := filepath.Base(path)
		name := strings.TrimSuffix(base, filepath.Ext(base))

		files = append(files, ImageFile{
			Path:        path,
			Name:        name,
			ContentHash: fmt.Sprintf("%x", hash),
			Mtime:       float64(info.ModTime().Unix()),
		})
		return nil
	})

	return files, err
}
