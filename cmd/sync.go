package cmd

import (
	"fmt"

	"github.com/anthropics/seek/internal/config"
	"github.com/anthropics/seek/internal/store"
)

type SyncCmd struct {
	Collection string `arg:"" optional:"" help:"Sync a specific collection (default: all)"`
}

func (c *SyncCmd) Run(cfg *config.AppConfig) error {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	collections, err := db.ListCollections()
	if err != nil {
		return err
	}

	if len(collections) == 0 {
		fmt.Println("No collections. Use 'seek add' to add one.")
		return nil
	}

	for i := range collections {
		col := &collections[i]
		if c.Collection != "" && col.Name != c.Collection {
			continue
		}

		fmt.Printf("Syncing %q (%s)...\n", col.Name, col.Type)

		switch col.Type {
		case "markdown":
			if err := syncMarkdownCollection(cfg, db, col); err != nil {
				fmt.Printf("  ERROR: %v\n", err)
			}
		case "claude":
			if err := syncClaudeCollection(cfg, db, col); err != nil {
				fmt.Printf("  ERROR: %v\n", err)
			}
		case "codex":
			if err := syncCodexCollection(cfg, db, col); err != nil {
				fmt.Printf("  ERROR: %v\n", err)
			}
		case "images":
			if err := syncImageCollection(cfg, db, col); err != nil {
				fmt.Printf("  ERROR: %v\n", err)
			}
		default:
			fmt.Printf("  Unknown type: %s\n", col.Type)
		}
	}

	return nil
}
