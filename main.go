// Build with: make build (or: CGO_ENABLED=1 go build -tags "fts5" -o seek .)
// FTS5 tag is required for SQLite full-text search.
package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/anthropics/seek/cmd"
	"github.com/anthropics/seek/internal/config"
)

var cli struct {
	Add    cmd.AddCmd    `cmd:"" help:"Add a collection to index"`
	Rm     cmd.RmCmd     `cmd:"" help:"Remove a collection"`
	Sync   cmd.SyncCmd   `cmd:"" help:"Sync all collections (incremental)"`
	Embed  cmd.EmbedCmd  `cmd:"" help:"Generate embeddings (batch or realtime)"`
	Search cmd.SearchCmd `cmd:"" help:"Search across collections"`
	Status cmd.StatusCmd `cmd:"" help:"Show index status"`
	Service cmd.ServiceCmd `cmd:"" help:"Manage periodic sync+embed service"`
	Auth    cmd.AuthCmd    `cmd:"" help:"Manage API authentication"`
	Config  cmd.ConfigCmd  `cmd:"" help:"Show or edit config"`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("seek"),
		kong.Description("Personal document search engine — BM25 + vector hybrid search"),
		kong.UsageOnError(),
	)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	err = ctx.Run(cfg)
	ctx.FatalIfErrorf(err)
}
