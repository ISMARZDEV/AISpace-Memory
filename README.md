# aispace-men

**Persistent memory for AI coding agents with cloud sync**

aispace-men is a persistent memory system for AI coding agents like OpenCode and Claude Code. It stores observations (decisions, bugs, discoveries, conventions) in SQLite with FTS5 full-text search and syncs to AIspace cloud for multi-device and team sharing.

## Features

- **SQLite + FTS5** - Fast local storage with full-text search
- **15 MCP Tools** - Complete API for memory management
- **Project Detection** - Automatic project name from git
- **Scope Management** - Personal vs project-scoped observations
- **Topic Keys** - Evolving topics with upsert support
- **Cloud Sync** - Multi-device and team sharing (coming soon)

## Installation

### From Source

```bash
git clone https://github.com/aispace/men.git
cd men
make build
sudo make install  # or cp aispace-men ~/.local/bin/
```

### Binary

```bash
# macOS
brew install aispace/tap/aispace-men

# Linux
brew install aispace/tap/aispace-men
# or
go install github.com/aispace/men/cmd/aispace-men@latest
```

## Quick Start

### 1. Start the MCP Server

```bash
aispace-men mcp
```

The MCP server runs on stdio and can be integrated with AI agents.

### 2. Configure Your Agent

#### OpenCode

```bash
aispace-men setup opencode
```

This installs a TypeScript plugin in `~/.config/opencode/plugins/aispace-men.ts` and registers the MCP server.

#### Claude Code

```bash
aispace-men setup claude-code
```

This writes `~/.claude/mcp/aispace-men.json` and updates permissions.

### 3. Use Memory Tools

From your AI agent, call:

```
mem_save("Fixed N+1 query", "bugfix", "Users table was missing index")
mem_search("N+1 query")
mem_context()  # Get recent context
```

## Commands

| Command | Description |
|---------|-------------|
| `mcp` | Start MCP server (stdio transport) |
| `version` | Show version |
| `stats` | Show memory statistics |
| `search <query>` | Search memories from CLI |
| `setup [agent]` | Install agent integration |

## MCP Tools

| Tool | Description |
|------|-------------|
| `mem_save` | Save an observation |
| `mem_search` | Search memories (FTS5) |
| `mem_context` | Get recent context |
| `mem_session_summary` | Save end-of-session summary |
| `mem_session_start` | Start a session |
| `mem_session_end` | End a session |
| `mem_get_observation` | Get observation by ID |
| `mem_suggest_topic_key` | Suggest topic key |
| `mem_update` | Update observation |
| `mem_delete` | Delete observation |
| `mem_stats` | Show statistics |
| `mem_timeline` | Timeline around observation |
| `mem_capture_passive` | Extract learnings from text |
| `mem_save_prompt` | Save user prompt |

## Data Storage

Data is stored in `~/.aispace-men/`:

```
~/.aispace-men/
├── aispace-men.db       # SQLite database
├── aispace-men.db-wal   # Write-ahead log
└── aispace-men.db-shm  # Shared memory
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `AISPACE_MEN_DATA_DIR` | Override data directory |
| `AISPACE_MEN_PROJECT` | Override detected project name |

## Development

```bash
# Build
make build

# Test
make test

# Run MCP server
make run-mcp

# Watch for changes (requires reflex)
make watch
```

## License

MIT License - see [LICENSE](LICENSE) for details.

## Credits

Based on [Engram](https://github.com/Gentleman-Programming/engram) by Gentleman Programming.