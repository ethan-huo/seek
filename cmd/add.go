package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/seek/internal/chunk"
	"github.com/anthropics/seek/internal/config"
	"github.com/anthropics/seek/internal/embed"
	"github.com/anthropics/seek/internal/source"
	"github.com/anthropics/seek/internal/store"
)

type AddCmd struct {
	Path   string `arg:"" optional:"" help:"Path to directory"`
	Name   string `short:"n" help:"Collection name (default: directory name)"`
	Claude bool   `help:"Add Claude Code conversations"`
	Codex  bool   `help:"Add Codex conversations"`
	Images bool   `help:"Add image files (png/jpg/webp)"`
}

func (c *AddCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	switch {
	case c.Claude:
		return c.addClaude(cfg, db)
	case c.Codex:
		return c.addCodex(cfg, db)
	case c.Images:
		return c.addImages(cfg, db)
	default:
		return c.addMarkdown(cfg, db)
	}
}

func (c *AddCmd) addMarkdown(cfg *config.AppConfig, db *store.Store) error {
	if c.Path == "" {
		return fmt.Errorf("path is required for markdown collection")
	}

	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	// Check if already exists
	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		return nil
	}

	col, err := db.CreateCollection(name, "markdown", absPath, "**/*.md")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (markdown) → %s\n", col.Name, col.Path)

	// Initial sync
	return syncMarkdownCollection(cfg, db, col)
}

func (c *AddCmd) addClaude(cfg *config.AppConfig, db *store.Store) error {
	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude", "projects")

	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return fmt.Errorf("claude projects directory not found: %s", claudeDir)
	}

	name := "claude-conversations"
	if c.Name != "" {
		name = c.Name
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d)\n", existing.Name, existing.ID)
		return nil
	}

	col, err := db.CreateCollection(name, "claude", claudeDir, "**/*.jsonl")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (claude) → %s\n", col.Name, col.Path)
	return syncClaudeCollection(cfg, db, col)
}

func (c *AddCmd) addCodex(cfg *config.AppConfig, db *store.Store) error {
	home, _ := os.UserHomeDir()
	codexDir := filepath.Join(home, ".codex")

	if _, err := os.Stat(codexDir); os.IsNotExist(err) {
		return fmt.Errorf("codex directory not found: %s", codexDir)
	}

	name := "codex-conversations"
	if c.Name != "" {
		name = c.Name
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d)\n", existing.Name, existing.ID)
		return nil
	}

	col, err := db.CreateCollection(name, "codex", codexDir, "**/*.jsonl")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (codex) → %s\n", col.Name, col.Path)
	return syncCodexCollection(cfg, db, col)
}

func (c *AddCmd) addImages(cfg *config.AppConfig, db *store.Store) error {
	if c.Path == "" {
		return fmt.Errorf("path is required for image collection")
	}

	absPath, err := filepath.Abs(c.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	if existing, err := db.GetCollectionByName(name); err == nil {
		fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		return nil
	}

	col, err := db.CreateCollection(name, "images", absPath, "**/*.{png,jpg,jpeg,webp,gif}")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (images) → %s\n", col.Name, col.Path)
	return syncImageCollection(cfg, db, col)
}

// --- Sync helpers (shared with sync.go) ---

func syncMarkdownCollection(cfg *config.AppConfig, db *store.Store, col *store.Collection) error {
	files, err := source.ScanMarkdown(col.Path, col.Pattern)
	if err != nil {
		return err
	}

	// Build set of current paths on disk
	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}

	// Remove stale documents no longer on disk
	existingPaths, err := db.ListDocumentPaths(col.ID)
	if err == nil {
		var removed int
		for path, docID := range existingPaths {
			if !diskPaths[path] {
				db.DeleteDocument(docID)
				removed++
			}
		}
		if removed > 0 {
			fmt.Printf("  Removed %d stale documents\n", removed)
		}
	}

	var indexed, skipped int

	for _, f := range files {
		existing, err := db.GetDocument(col.ID, f.Path)
		if err == nil {
			// Document exists — check if changed
			if existing.ContentHash == f.ContentHash {
				if existing.Mtime != f.Mtime {
					db.UpdateDocumentMtime(existing.ID, f.Mtime)
				}
				skipped++
				continue
			}
		}

		// New or changed — index it
		docID, err := db.UpsertDocument(col.ID, f.Path, f.Title, f.ContentHash, f.Mtime, f.LineCount)
		if err != nil {
			fmt.Printf("  WARN: upsert doc %s: %v\n", f.Path, err)
			continue
		}

		// FTS
		if err := db.UpsertFTS(docID, f.Title, f.Content); err != nil {
			fmt.Printf("  WARN: fts %s: %v\n", f.Path, err)
		}

		// Chunks + embeddings
		db.DeleteChunksForDocument(docID)
		chunks := chunk.ChunkMarkdown(f.Content, 0, 0)
		if err := indexChunks(db, docID, chunks); err != nil {
			fmt.Printf("  WARN: embed %s: %v\n", f.Path, err)
		}

		indexed++
	}

	fmt.Printf("  Synced: %d indexed, %d unchanged\n", indexed, skipped)
	return nil
}

func syncClaudeCollection(cfg *config.AppConfig, db *store.Store, col *store.Collection) error {
	convos, err := source.ScanClaude()
	if err != nil {
		return err
	}

	var indexed, skipped int
	var totalImages int

	for _, conv := range convos {
		lineCount, _ := source.CountLines(conv.Path)

		existing, err := db.GetDocument(col.ID, conv.Path)
		if err == nil && existing.LineCount >= lineCount {
			skipped++
			continue
		}

		// Get new messages from offset
		fromLine := 0
		if existing != nil {
			fromLine = existing.LineCount
		}

		convID := strings.TrimSuffix(filepath.Base(conv.Path), filepath.Ext(conv.Path))

		var messages []source.ClaudeMessage
		var images []source.ConversationImage
		if fromLine > 0 {
			messages, images, err = source.ParseClaudeFileWithImages(conv.Path, fromLine, convID)
			if err != nil {
				continue
			}
		} else {
			messages = conv.Messages
			images = conv.Images
		}

		if len(messages) == 0 && len(images) == 0 {
			skipped++
			continue
		}

		title := conv.Title
		if title == "" {
			title = filepath.Base(conv.Path)
		}

		docID, err := db.UpsertDocument(col.ID, conv.Path, title, "", 0, lineCount)
		if err != nil {
			continue
		}

		text := source.ClaudeConversationToText(messages)
		if fromLine > 0 {
			// Incremental: append new text to existing FTS entry
			if err := db.AppendFTS(docID, text); err != nil {
				fmt.Printf("  WARN: fts %s: %v\n", conv.Path, err)
			}
		} else {
			if err := db.UpsertFTS(docID, title, text); err != nil {
				fmt.Printf("  WARN: fts %s: %v\n", conv.Path, err)
			}
			db.DeleteChunksForDocument(docID)
		}

		// Text chunks (always append — incremental adds new chunks)
		chunks := chunk.ChunkConversation(text, 0)
		if err := indexChunks(db, docID, chunks); err != nil {
			fmt.Printf("  WARN: embed %s: %v\n", conv.Path, err)
		}

		// Image chunks
		nextSeq := len(chunks)
		for _, img := range images {
			if err := db.InsertImageChunk(docID, nextSeq, img.Context, img.SavedPath, nil); err != nil {
				fmt.Printf("  WARN: image chunk %s: %v\n", img.SavedPath, err)
				continue
			}
			nextSeq++
			totalImages++
		}

		indexed++
	}

	fmt.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if totalImages > 0 {
		fmt.Printf(", %d images", totalImages)
	}
	fmt.Println()
	return nil
}

func syncCodexCollection(cfg *config.AppConfig, db *store.Store, col *store.Collection) error {
	convos, err := source.ScanCodex()
	if err != nil {
		return err
	}

	var indexed, skipped int
	var totalImages int

	for _, conv := range convos {
		lineCount, _ := source.CountLines(conv.Path)

		existing, err := db.GetDocument(col.ID, conv.Path)
		if err == nil && existing.LineCount >= lineCount {
			skipped++
			continue
		}

		fromLine := 0
		if existing != nil {
			fromLine = existing.LineCount
		}

		var messages []source.CodexMessage
		var images []source.ConversationImage
		if fromLine > 0 {
			messages, _, images, err = source.ParseCodexFileWithImages(conv.Path, fromLine)
			if err != nil {
				continue
			}
		} else {
			messages = conv.Messages
			images = conv.Images
		}

		if len(messages) == 0 && len(images) == 0 {
			skipped++
			continue
		}

		title := conv.ThreadName
		if title == "" {
			title = filepath.Base(conv.Path)
		}

		docID, err := db.UpsertDocument(col.ID, conv.Path, title, "", 0, lineCount)
		if err != nil {
			continue
		}

		text := source.ConversationToText(messages)
		if fromLine > 0 {
			if err := db.AppendFTS(docID, text); err != nil {
				fmt.Printf("  WARN: fts %s: %v\n", conv.Path, err)
			}
		} else {
			if err := db.UpsertFTS(docID, title, text); err != nil {
				fmt.Printf("  WARN: fts %s: %v\n", conv.Path, err)
			}
			db.DeleteChunksForDocument(docID)
		}

		// Text chunks
		chunks := chunk.ChunkConversation(text, 0)
		if err := indexChunks(db, docID, chunks); err != nil {
			fmt.Printf("  WARN: embed %s: %v\n", conv.Path, err)
		}

		// Image chunks
		nextSeq := len(chunks)
		for _, img := range images {
			if err := db.InsertImageChunk(docID, nextSeq, img.Context, img.SavedPath, nil); err != nil {
				fmt.Printf("  WARN: image chunk %s: %v\n", img.SavedPath, err)
				continue
			}
			nextSeq++
			totalImages++
		}

		indexed++
	}

	fmt.Printf("  Synced: %d indexed, %d unchanged", indexed, skipped)
	if totalImages > 0 {
		fmt.Printf(", %d images", totalImages)
	}
	fmt.Println()
	return nil
}

func newEmbedClient(cfg *config.AppConfig) *embed.Client {
	key, err := cfg.RequireEmbeddingKey()
	if err != nil {
		return nil
	}
	return embed.NewClient(
		cfg.Config.Embedding.BaseURL,
		key,
		cfg.Config.Embedding.Model,
		cfg.Config.Embedding.Dimensions,
	)
}

func newVLClient(cfg *config.AppConfig) *embed.VLClient {
	key, err := cfg.RequireEmbeddingKey()
	if err != nil {
		return nil
	}
	model := cfg.Config.Embedding.Model
	// Only create VL client for multimodal models
	if !isVLModel(model) {
		return nil
	}
	return embed.NewVLClient(key, model, cfg.Config.Embedding.Dimensions)
}

// isVLModel returns true if the model is a multimodal embedding model.
func isVLModel(model string) bool {
	return strings.Contains(model, "vl-embedding") || strings.Contains(model, "multimodal")
}

// indexChunks stores chunks in DB without embeddings.
// Use `seek embed` to generate embeddings separately (batch or realtime).
func indexChunks(db *store.Store, docID int64, chunks []chunk.Chunk) error {
	for _, c := range chunks {
		if c.Type == chunk.ChunkImage {
			if err := db.InsertImageChunk(docID, c.Seq, c.Content, c.ImagePath, nil); err != nil {
				return err
			}
		} else {
			if err := db.InsertChunk(docID, c.Seq, c.Content, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func syncImageCollection(cfg *config.AppConfig, db *store.Store, col *store.Collection) error {
	files, err := source.ScanImages(col.Path)
	if err != nil {
		return err
	}

	// Remove stale images no longer on disk
	diskPaths := make(map[string]bool, len(files))
	for _, f := range files {
		diskPaths[f.Path] = true
	}
	if existingPaths, err := db.ListDocumentPaths(col.ID); err == nil {
		var removed int
		for path, docID := range existingPaths {
			if !diskPaths[path] {
				db.DeleteDocument(docID)
				removed++
			}
		}
		if removed > 0 {
			fmt.Printf("  Removed %d stale images\n", removed)
		}
	}

	var indexed, skipped int

	for _, f := range files {
		existing, err := db.GetDocument(col.ID, f.Path)
		if err == nil && existing.ContentHash == f.ContentHash {
			skipped++
			continue
		}

		// Each image file = 1 document + 1 image chunk
		docID, err := db.UpsertDocument(col.ID, f.Path, f.Name, f.ContentHash, f.Mtime, 0)
		if err != nil {
			fmt.Printf("  WARN: upsert doc %s: %v\n", f.Path, err)
			continue
		}

		db.DeleteChunksForDocument(docID)

		// Insert as image chunk — content is the filename (as context for VL embedding)
		if err := db.InsertImageChunk(docID, 0, f.Name, f.Path, nil); err != nil {
			fmt.Printf("  WARN: image chunk %s: %v\n", f.Path, err)
			continue
		}

		indexed++
	}

	fmt.Printf("  Synced: %d indexed, %d unchanged\n", indexed, skipped)
	return nil
}

// formatRelPath returns a shorter display path.
func formatRelPath(path string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
