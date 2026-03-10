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

// ClaudeMessage represents a parsed message from a Claude Code conversation.
type ClaudeMessage struct {
	Role    string
	Content string
}

// ClaudeConversation represents a parsed Claude Code conversation.
type ClaudeConversation struct {
	Path     string
	Project  string
	Title    string
	Messages []ClaudeMessage
	Images   []ConversationImage
}

// ConversationFile represents a discovered JSONL file (lightweight, no parsing).
type ConversationFile struct {
	Path  string
	Mtime float64
}

// ScanClaudeFiles scans ~/.claude/projects/ for conversation JSONL file paths without parsing them.
func ScanClaudeFiles() ([]ConversationFile, error) {
	home, _ := os.UserHomeDir()
	projectsDir := filepath.Join(home, ".claude", "projects")

	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("claude projects directory not found: %s", projectsDir)
	}

	var files []ConversationFile

	err := filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		files = append(files, ConversationFile{
			Path:  path,
			Mtime: float64(info.ModTime().Unix()),
		})
		return nil
	})

	return files, err
}

// ParseClaudeFile parses a single Claude JSONL file starting from a line offset.
func ParseClaudeFile(path string, fromLine int) ([]ClaudeMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []ClaudeMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer
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

		msg, ok := parseClaudeLine(line)
		if ok {
			messages = append(messages, msg)
		}
	}

	return messages, scanner.Err()
}

// ParseClaudeFileWithImages parses a Claude JSONL file and extracts both messages and images.
func ParseClaudeFileWithImages(path string, fromLine int, convID string) ([]ClaudeMessage, []ConversationImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var messages []ClaudeMessage
	var images []ConversationImage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10MB buffer for images
	lineNum := 0
	imgIdx := 0

	// Collect all text messages for context lookup
	var allTexts []string

	for scanner.Scan() {
		lineNum++
		if lineNum <= fromLine {
			continue
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		msg, ok := parseClaudeLine(line)
		if ok {
			messages = append(messages, msg)
			allTexts = append(allTexts, msg.Content)
		}

		// Extract images from this line
		lineImages := extractClaudeImages(line, convID, &imgIdx)
		for i := range lineImages {
			// Attach context: nearest text before or after
			if len(allTexts) > 0 {
				lineImages[i].Context = Truncate(allTexts[len(allTexts)-1], 500)
			}
			images = append(images, lineImages[i])
		}
	}

	return messages, images, scanner.Err()
}

// extractClaudeImages extracts images from a single JSONL line.
// Claude images appear as:
//   - User messages: {type: "user", message: {role: "user", content: [{type: "image", source: {type: "base64", media_type: "...", data: "..."}}]}}
//   - Assistant messages: {type: "assistant", message: [{type: "image", source: {type: "base64", media_type: "...", data: "..."}}]}
func extractClaudeImages(line string, convID string, imgIdx *int) []ConversationImage {
	var raw struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}

	if raw.Type != "user" && raw.Type != "assistant" {
		return nil
	}

	var contentBlocks []json.RawMessage

	if raw.Type == "user" {
		var userMsg struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw.Message, &userMsg); err != nil {
			return nil
		}
		// content can be a string or array
		var arr []json.RawMessage
		if err := json.Unmarshal(userMsg.Content, &arr); err != nil {
			return nil // string content, no images
		}
		contentBlocks = arr
	} else {
		// Assistant: message is array directly or has content field
		var arr []json.RawMessage
		if err := json.Unmarshal(raw.Message, &arr); err != nil {
			return nil
		}
		contentBlocks = arr
	}

	var images []ConversationImage
	prefix := convID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}

	for _, block := range contentBlocks {
		var b struct {
			Type   string `json:"type"`
			Source struct {
				Type      string `json:"type"`
				MediaType string `json:"media_type"`
				Data      string `json:"data"`
			} `json:"source"`
		}
		if err := json.Unmarshal(block, &b); err != nil {
			continue
		}
		if b.Type != "image" || b.Source.Type != "base64" || b.Source.Data == "" {
			continue
		}

		decoded, err := base64.StdEncoding.DecodeString(b.Source.Data)
		if err != nil {
			continue
		}

		ext := ExtractExtension(b.Source.MediaType)
		savePath := filepath.Join(ImageCacheDir(), fmt.Sprintf("claude-%s-%d.%s", prefix, *imgIdx, ext))

		if err := SaveImage(decoded, b.Source.MediaType, savePath); err != nil {
			continue
		}

		images = append(images, ConversationImage{
			Data:      decoded,
			MediaType: b.Source.MediaType,
			Index:     *imgIdx,
			SavedPath: savePath,
		})
		*imgIdx++
	}

	return images
}

func parseClaudeConversation(path, projectsDir string) (*ClaudeConversation, error) {
	// Extract project name from path
	rel, _ := filepath.Rel(projectsDir, path)
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	project := parts[0]

	// Use convID from filename (without extension)
	convID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	messages, images, err := ParseClaudeFileWithImages(path, 0, convID)
	if err != nil {
		return nil, err
	}

	title := ""
	if len(messages) > 0 {
		// First user message becomes title
		for _, m := range messages {
			if m.Role == "user" {
				title = Truncate(m.Content, 100)
				break
			}
		}
	}

	return &ClaudeConversation{
		Path:     path,
		Project:  project,
		Title:    title,
		Messages: messages,
		Images:   images,
	}, nil
}

func parseClaudeLine(line string) (ClaudeMessage, bool) {
	var raw struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return ClaudeMessage{}, false
	}

	if raw.Type != "user" && raw.Type != "assistant" {
		return ClaudeMessage{}, false
	}

	role := raw.Type

	if raw.Type == "user" {
		// User message: {message: {role: "user", content: "text" | [{type: "text", text: "..."}]}}
		var userMsg struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw.Message, &userMsg); err != nil {
			return ClaudeMessage{}, false
		}
		text := extractTextContent(userMsg.Content)
		if text == "" {
			return ClaudeMessage{}, false
		}
		return ClaudeMessage{Role: role, Content: text}, true
	}

	// Assistant message: {message: [{type: "text", text: "..."}] | "string"}
	text := extractTextContent(raw.Message)
	if text == "" {
		return ClaudeMessage{}, false
	}
	return ClaudeMessage{Role: role, Content: text}, true
}

func extractTextContent(raw json.RawMessage) string {
	// Try as string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try as array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

func Truncate(s string, maxLen int) string {
	// Take first line or first maxLen chars
	if idx := strings.IndexByte(s, '\n'); idx >= 0 && idx < maxLen {
		s = s[:idx]
	}
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

// CountLines counts the number of lines in a file.
func CountLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}
