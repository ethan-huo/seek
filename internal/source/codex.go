package source

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CodexMessage struct {
	Role    string
	Content string
}

type CodexConversation struct {
	Path       string
	SessionID  string
	ThreadName string
	CWD        string
	Messages   []CodexMessage
	Images     []ConversationImage
}

// ScanCodexFiles scans ~/.codex/ for conversation JSONL file paths without parsing them.
func ScanCodexFiles() ([]ConversationFile, error) {
	home, _ := os.UserHomeDir()

	dirs := []string{
		filepath.Join(home, ".codex", "sessions"),
		filepath.Join(home, ".codex", "archived_sessions"),
	}

	var files []ConversationFile

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			if filepath.Base(path) == "session_index.jsonl" {
				return nil
			}
			files = append(files, ConversationFile{
				Path:  path,
				Mtime: float64(info.ModTime().Unix()),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return files, nil
}

// LoadCodexThreadNames loads session index for title lookup.
func LoadCodexThreadNames() map[string]string {
	home, _ := os.UserHomeDir()
	return loadSessionIndex(home)
}

// ParseCodexFile parses a single Codex JSONL file starting from a line offset.
func ParseCodexFile(path string, fromLine int) ([]CodexMessage, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	var messages []CodexMessage
	var sessionID string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum <= fromLine {
			continue
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var raw struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		switch raw.Type {
		case "session_meta":
			var meta struct {
				ID string `json:"id"`
			}
			json.Unmarshal(raw.Payload, &meta)
			sessionID = meta.ID

		case "response_item":
			var item struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(raw.Payload, &item); err != nil {
				continue
			}
			if item.Role != "user" && item.Role != "assistant" {
				continue
			}
			var parts []string
			for _, c := range item.Content {
				if (c.Type == "input_text" || c.Type == "output_text") && c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
			if len(parts) > 0 {
				messages = append(messages, CodexMessage{
					Role:    item.Role,
					Content: strings.Join(parts, "\n"),
				})
			}
		}
	}

	return messages, sessionID, scanner.Err()
}

// ParseCodexFileWithImages parses a Codex JSONL file and extracts both messages and images.
func ParseCodexFileWithImages(path string, fromLine int) ([]CodexMessage, string, []ConversationImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", nil, err
	}
	defer f.Close()

	var messages []CodexMessage
	var images []ConversationImage
	var sessionID string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10MB buffer for images
	lineNum := 0
	imgIdx := 0

	for scanner.Scan() {
		lineNum++
		if lineNum <= fromLine {
			continue
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var raw struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		switch raw.Type {
		case "session_meta":
			var meta struct {
				ID string `json:"id"`
			}
			json.Unmarshal(raw.Payload, &meta)
			sessionID = meta.ID

		case "response_item":
			var item struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(raw.Payload, &item); err != nil {
				continue
			}

			// Extract text messages
			var contentBlocks []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL string `json:"image_url"`
			}
			if err := json.Unmarshal(item.Content, &contentBlocks); err != nil {
				continue
			}

			if item.Role != "user" && item.Role != "assistant" {
				continue
			}

			var textParts []string
			for _, c := range contentBlocks {
				if (c.Type == "input_text" || c.Type == "output_text") && c.Text != "" {
					textParts = append(textParts, c.Text)
				}
			}
			if len(textParts) > 0 {
				messages = append(messages, CodexMessage{
					Role:    item.Role,
					Content: strings.Join(textParts, "\n"),
				})
			}

			// Extract images from user messages
			if item.Role == "user" {
				sid := sessionID
				if len(sid) > 8 {
					sid = sid[:8]
				}

				for _, c := range contentBlocks {
					if c.Type != "input_image" || c.ImageURL == "" {
						continue
					}

					// Parse data URI: "data:image/png;base64,iVBORw0K..."
					mediaType, decoded, ok := parseDataURI(c.ImageURL)
					if !ok {
						continue
					}

					ext := ExtractExtension(mediaType)
					savePath := filepath.Join(ImageCacheDir(), fmt.Sprintf("codex-%s-%d.%s", sid, imgIdx, ext))

					if err := SaveImage(decoded, mediaType, savePath); err != nil {
						continue
					}

					// Context: nearest text from this message
					context := ""
					if len(textParts) > 0 {
						context = Truncate(strings.Join(textParts, " "), 500)
					} else if len(messages) > 0 {
						context = Truncate(messages[len(messages)-1].Content, 500)
					}

					images = append(images, ConversationImage{
						Data:      decoded,
						MediaType: mediaType,
						Context:   context,
						Index:     imgIdx,
						SavedPath: savePath,
					})
					imgIdx++
				}
			}
		}
	}

	return messages, sessionID, images, scanner.Err()
}

// parseDataURI parses "data:image/png;base64,..." and returns mediaType, decoded bytes, and ok.
func parseDataURI(uri string) (string, []byte, bool) {
	if !strings.HasPrefix(uri, "data:") {
		return "", nil, false
	}
	// data:image/png;base64,iVBORw0K...
	rest := uri[5:]
	semicolonIdx := strings.Index(rest, ";")
	if semicolonIdx < 0 {
		return "", nil, false
	}
	mediaType := rest[:semicolonIdx]
	rest = rest[semicolonIdx+1:]

	if !strings.HasPrefix(rest, "base64,") {
		return "", nil, false
	}
	b64Data := rest[7:]

	decoded, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		// Try RawStdEncoding (no padding)
		decoded, err = base64.RawStdEncoding.DecodeString(b64Data)
		if err != nil {
			return "", nil, false
		}
	}

	return mediaType, decoded, true
}

func parseCodexConversation(path string, threadNames map[string]string) (*CodexConversation, error) {
	messages, sessionID, images, err := ParseCodexFileWithImages(path, 0)
	if err != nil {
		return nil, err
	}

	title := ""
	if name, ok := threadNames[sessionID]; ok {
		title = name
	} else if len(messages) > 0 {
		for _, m := range messages {
			if m.Role == "user" {
				title = Truncate(m.Content, 100)
				break
			}
		}
	}

	return &CodexConversation{
		Path:       path,
		SessionID:  sessionID,
		ThreadName: title,
		Messages:   messages,
		Images:     images,
	}, nil
}

func loadSessionIndex(home string) map[string]string {
	indexPath := filepath.Join(home, ".codex", "sessions", "session_index.jsonl")
	f, err := os.Open(indexPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	names := make(map[string]string)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil && entry.ID != "" {
			names[entry.ID] = entry.ThreadName
		}
	}
	return names
}

// ConversationToText formats messages into a readable text.
func ConversationToText(messages []CodexMessage) string {
	var b strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&b, "[%s]: %s\n\n", m.Role, m.Content)
	}
	return b.String()
}

// ClaudeConversationToText formats Claude messages into readable text.
func ClaudeConversationToText(messages []ClaudeMessage) string {
	var b strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&b, "[%s]: %s\n\n", m.Role, m.Content)
	}
	return b.String()
}
