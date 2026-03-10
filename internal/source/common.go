package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConversationImage represents an image extracted from a conversation.
type ConversationImage struct {
	Data      []byte // decoded base64
	MediaType string // "image/png", "image/jpeg"
	Context   string // surrounding text from the conversation
	Index     int    // position in conversation
	SavedPath string // path where image was saved to disk
}

// SaveImage writes image data to the given path, creating directories as needed.
func SaveImage(data []byte, mediaType string, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create image dir: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ExtractExtension returns the file extension for a given media type.
func ExtractExtension(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	default:
		return "bin"
	}
}

// ImageCacheDir returns the path to the image cache directory.
func ImageCacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "seek", "images")
}
