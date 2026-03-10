---
name: seek
description: Search user's personal notes, markdown docs, Claude Code and Codex conversation history. Use when user asks about past conversations, notes, or "do I have notes about X".
---

# seek — Personal Knowledge Search

`seek` is the user's local search engine. It indexes markdown notes, Claude Code conversations, and Codex conversations with BM25 + vector hybrid search. Uses **qwen3-vl-embedding** (multimodal) via Alibaba Bailian API — text and images share the same vector space.

Binary location: `seek`

## When to Use

- User asks "have I discussed X before" / "find my notes about Y"
- User references past conversations or notes
- You need context from previous work sessions
- User asks you to search their knowledge base

## Search Commands

```bash
# Hybrid search (BM25 + vector, best quality, RECOMMENDED)
seek search "your query" -l 10

# BM25 keyword search only (fast, no API call)
seek search "exact keyword" --lex -l 10

# Vector semantic search only (meaning-based)
seek search "conceptual question" --vec -l 10
```

### Search Strategy

1. **Start with `--lex`** for exact terms, names, error messages, file paths
2. **Use default (hybrid)** for conceptual questions like "how to deploy" or "best practices for X"
3. **Use `--vec`** only when hybrid results are poor and you need pure semantic matching
4. **Increase `-l 20`** if the first 10 results aren't enough

### Reading Output

```
# Text result
[1] Document Title
    ~/path/to/file.md  (collection-name)  score=0.5775
    Snippet of matching content...

# Image result
[2] Conversation Title
    ~/.claude/projects/.../xxx.jsonl  (claude-conversations)  score=0.6037
    📷 ~/.cache/seek/images/claude-0222d48a-0.png
    context: the dialog layout is broken, content overflows...
```

- `collection-name` tells you which collection the result came from (run `seek status` to see all)
- For conversation results, the title is the first user message
- The path is the actual file — use `Read` tool to get full content if needed
- 📷 indicates an image result — use `Read` tool on the image path to view it

## Maintenance Commands

Only run these when user explicitly asks to update the index:

```bash
# Sync new/changed files (incremental, fast)
# Also extracts base64 images from Claude/Codex conversations → ~/.cache/seek/images/
seek sync

# Generate embeddings for new chunks (VL multimodal API, text + images)
seek embed

# Force re-embed all chunks (e.g. after model change)
seek embed -f

# Check index status
seek status

# Add a new markdown collection
seek add /path/to/dir --name myname

# Add an image directory (png/jpg/webp — VL embedding for visual search)
seek add --images /path/to/images -n myimages
```

## Collection Types

| Type | Source | What's indexed |
|---|---|---|
| `markdown` | Any directory | `.md` files — FTS + chunks + embeddings |
| `claude` | `~/.claude/projects/` | Claude Code conversations + screenshots |
| `codex` | `~/.codex/` | Codex sessions + screenshots |
| `images` | Any directory | Image files (png/jpg/webp) with VL embedding |

Run `seek status` to see which collections the user has configured.

## Multimodal

- Images from conversations are extracted and cached at `~/.cache/seek/images/`
- Text and image chunks use the same embedding model (unified vector space)
- Vector search naturally finds relevant images alongside text results

## Important Notes

- **Do NOT run `sync` or `embed` proactively.** Only when user asks to update.
- **Always use absolute path** to the binary: `seek`
- Hybrid/vec search requires API key (already configured in `~/.config/seek/config.yaml`)
- If search returns no results, try rephrasing or switching between `--lex` and hybrid
- Multilingual queries work — the index supports mixed-language content
