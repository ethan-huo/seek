package chunk

import (
	"strings"
)

const (
	DefaultMaxChunkSize = 1000 // chars
	DefaultOverlap      = 100  // chars
)

// ChunkType distinguishes text chunks from image chunks.
type ChunkType int

const (
	ChunkText  ChunkType = iota // 0
	ChunkImage                  // 1
)

type Chunk struct {
	Seq       int
	Content   string    // text content or context for image
	Type      ChunkType // ChunkText or ChunkImage
	ImagePath string    // path to image file on disk (for image chunks)
}

// ChunkMarkdown splits markdown content into chunks by headers, then by size.
func ChunkMarkdown(content string, maxSize, overlap int) []Chunk {
	if maxSize <= 0 {
		maxSize = DefaultMaxChunkSize
	}
	if overlap <= 0 {
		overlap = DefaultOverlap
	}

	// Split by headers first
	sections := splitByHeaders(content)

	var chunks []Chunk
	seq := 0

	for _, section := range sections {
		text := strings.TrimSpace(section)
		if text == "" {
			continue
		}

		if len(text) <= maxSize {
			chunks = append(chunks, Chunk{Seq: seq, Content: text, Type: ChunkText})
			seq++
			continue
		}

		// Split large sections by paragraphs, then by size
		parts := splitBySize(text, maxSize, overlap)
		for _, p := range parts {
			chunks = append(chunks, Chunk{Seq: seq, Content: p, Type: ChunkText})
			seq++
		}
	}

	return chunks
}

// ChunkConversation splits conversation text into chunks.
// Each chunk is a sequence of messages up to maxSize.
func ChunkConversation(content string, maxSize int) []Chunk {
	if maxSize <= 0 {
		maxSize = DefaultMaxChunkSize
	}

	lines := strings.Split(content, "\n")
	var chunks []Chunk
	seq := 0
	var current strings.Builder

	for _, line := range lines {
		if current.Len()+len(line)+1 > maxSize && current.Len() > 0 {
			chunks = append(chunks, Chunk{Seq: seq, Content: strings.TrimSpace(current.String()), Type: ChunkText})
			seq++
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	if current.Len() > 0 {
		text := strings.TrimSpace(current.String())
		if text != "" {
			chunks = append(chunks, Chunk{Seq: seq, Content: text, Type: ChunkText})
		}
	}

	return chunks
}

func splitByHeaders(content string) []string {
	lines := strings.Split(content, "\n")
	var sections []string
	var current strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && current.Len() > 0 {
			sections = append(sections, current.String())
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if current.Len() > 0 {
		sections = append(sections, current.String())
	}

	return sections
}

func splitBySize(text string, maxSize, overlap int) []string {
	// Split by paragraphs first
	paragraphs := strings.Split(text, "\n\n")

	var parts []string
	var current strings.Builder

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if current.Len()+len(p)+2 > maxSize && current.Len() > 0 {
			parts = append(parts, strings.TrimSpace(current.String()))
			// Keep overlap from end of current
			tail := current.String()
			current.Reset()
			if overlap > 0 && len(tail) > overlap {
				current.WriteString(tail[len(tail)-overlap:])
				current.WriteString("\n\n")
			}
		}
		current.WriteString(p)
		current.WriteString("\n\n")
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}
