package cmd

import (
	"fmt"

	"github.com/anthropics/seek/internal/config"
	"github.com/anthropics/seek/internal/store"
)

type StatusCmd struct{}

func (c *StatusCmd) Run(cfg *config.AppConfig) error {
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

	fmt.Printf("Database: %s\n\n", cfg.DBPath)

	for _, col := range collections {
		docs, _ := db.CountDocuments(col.ID)
		chunks, _ := db.CountChunks(col.ID)

		fmt.Printf("%-25s  type=%-10s  docs=%-5d  chunks=%-5d\n",
			col.Name, col.Type, docs, chunks)
		fmt.Printf("  → %s\n", formatRelPath(col.Path))
	}

	return nil
}
