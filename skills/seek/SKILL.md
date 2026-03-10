---
name: seek
description: Search user's personal notes, markdown docs, Claude Code and Codex conversation history. Use when user asks about past conversations, notes, or "do I have notes about X".
---

# seek — Personal Knowledge Search

`seek` is the user's local search engine. It indexes markdown notes, Claude Code conversations, and Codex conversations with BM25 + vector hybrid search. Uses **qwen3-vl-embedding** (multimodal) via Alibaba Bailian API — text and images share the same vector space.

Binary location: `seek`

## When to Use

- User asks "我之前有没有讨论过 X" / "找一下关于 Y 的笔记"
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
2. **Use default (hybrid)** for conceptual questions like "how to deploy" or "AI 编程经验"
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
    context: dialog 不对 内容溢出了...
```

- `collection-name` tells you the source: `hub` (markdown notes), `claude-conversations`, `codex-conversations`
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

## Current Collections

| Collection | Type | Content |
|---|---|---|
| `hub` | markdown | personal-hub 笔记、项目、日志 |
| `claude-conversations` | claude | Claude Code 所有项目的聊天历史（含截图） |
| `codex-conversations` | codex | Codex 所有会话历史（含截图） |

## Multimodal

- Images from Claude/Codex conversations are extracted and cached at `~/.cache/seek/images/`
- Both text and image chunks use `qwen3-vl-embedding` (unified vector space)
- Vector search naturally finds relevant images alongside text results

## Important Notes

- **Do NOT run `sync` or `embed` proactively.** Only when user asks to update.
- **Always use absolute path** to the binary: `seek`
- Hybrid/vec search requires API key (already configured in `~/.config/seek/config.yaml`)
- If search returns no results, try rephrasing or switching between `--lex` and hybrid
- Chinese and English queries both work — the index contains mixed-language content
