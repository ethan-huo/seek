package cmd

import (
	"fmt"
	"strings"

	"github.com/anthropics/seek/internal/config"
	"github.com/anthropics/seek/internal/search"
	"github.com/anthropics/seek/internal/store"
)

type SearchCmd struct {
	Query string `arg:"" help:"Search query"`
	Lex   bool   `help:"BM25 full-text search only"`
	Vec   bool   `help:"Vector semantic search only"`
	Limit int    `short:"l" default:"10" help:"Max results"`
}

func (c *SearchCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	embedClient := newEmbedClient(cfg)
	vlClient := newVLClient(cfg)

	var engine *search.Engine
	if vlClient != nil {
		engine = search.NewEngineWithVL(db, embedClient, vlClient)
	} else {
		engine = search.NewEngine(db, embedClient)
	}

	var results []store.SearchResult

	switch {
	case c.Lex:
		results, err = engine.SearchBM25(c.Query, c.Limit)
	case c.Vec:
		if embedClient == nil && vlClient == nil {
			return fmt.Errorf("vector search requires embedding API key")
		}
		results, err = engine.SearchVector(c.Query, c.Limit)
	default:
		results, err = engine.SearchHybrid(c.Query, c.Limit)
	}

	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	for i, r := range results {
		fmt.Printf("\n%s %s\n", fmt.Sprintf("[%d]", i+1), r.Title)
		fmt.Printf("    %s  (%s)  score=%.4f\n", formatRelPath(r.Path), r.Collection, r.Score)
		if r.ChunkType == 1 && r.ImagePath != "" {
			fmt.Printf("    📷 %s\n", formatRelPath(r.ImagePath))
			if r.Content != "" {
				snippet := formatSnippet(r.Content, 150)
				fmt.Printf("    context: %s\n", snippet)
			}
		} else if r.Content != "" {
			snippet := formatSnippet(r.Content, 200)
			fmt.Printf("    %s\n", snippet)
		}
	}
	fmt.Println()

	return nil
}

func formatSnippet(content string, maxLen int) string {
	// Clean up whitespace
	s := strings.ReplaceAll(content, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}
