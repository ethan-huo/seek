# seek

Personal hybrid search engine for markdown notes, Claude Code conversations, and Codex conversations. BM25 full-text + vector semantic search with multimodal embedding.

## Why

AI agents lose context between sessions. `seek` indexes everything — your notes, every Claude Code conversation, every Codex session, including screenshots — so agents can recall what you discussed weeks ago.

## Install

```bash
# requires: go 1.24+, CGO
make build
ln -sf $(pwd)/seek /usr/local/bin/seek
```

As a [skill](https://skills.sh) (Claude Code, Codex, Cursor, etc.):

```bash
bunx skills add ethan-huo/seek
```

## Setup

```bash
# Configure embedding API (DashScope / OpenAI / custom)
seek auth login

# Add your collections
seek add /path/to/notes --name mynotes    # markdown
seek add --claude                          # Claude Code conversations
seek add --codex                           # Codex conversations
seek add --images /path/to/images -n pics  # image files

# Generate embeddings
seek embed
```

## Usage

```bash
# Hybrid search (BM25 + vector, recommended)
seek search "how to deploy the gateway"

# BM25 keyword search (fast, no API call)
seek search "ECONNREFUSED port 3000" --lex

# Vector semantic search (meaning-based)
seek search "函数式编程架构" --vec

# Incremental sync + embed new content
seek sync && seek embed
```

## How It Works

**Indexing** — `seek sync` scans collections incrementally. Markdown files are tracked by content hash. Claude/Codex JSONL files are append-only, tracked by line count. Base64 images in conversations are extracted to `~/.cache/seek/images/`.

**Embedding** — `seek embed` generates vectors via [qwen3-vl-embedding](https://help.aliyun.com/zh/model-studio/developer-reference/multimodal-embedding) (multimodal). Text and images share the same vector space. Supports DashScope Batch API (50% cheaper) for bulk indexing.

**Search** — Three modes:
- `--lex`: SQLite FTS5 BM25 ranking
- `--vec`: Cosine similarity against stored embeddings
- Default (hybrid): [RRF fusion](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf) combining both

**Storage** — SQLite database at `~/.cache/seek/index.db`. Config at `~/.config/seek/config.yaml`.

## Collections

| Type | Source | What's indexed |
|---|---|---|
| `markdown` | Any directory | `.md` files, FTS + chunks + embeddings |
| `claude` | `~/.claude/projects/` | All Claude Code conversations + screenshots |
| `codex` | `~/.codex/` | All Codex sessions + screenshots |
| `images` | Any directory | Image files (png/jpg/webp) with VL embedding |

## Built With

- [Kong](https://github.com/alecthomas/kong) — struct-tag CLI framework
- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) — SQLite with FTS5
- [qwen3-vl-embedding](https://help.aliyun.com/zh/model-studio/developer-reference/multimodal-embedding) — multimodal embedding via DashScope

## License

MIT
