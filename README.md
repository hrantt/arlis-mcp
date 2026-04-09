# arlis-mcp

MCP server for the [Armenian Legal Information System](https://www.arlis.am) (ARLIS) — the official legal acts database of Armenia's Ministry of Justice, containing ~206,000 laws, codes, government decisions, and other regulatory acts.

## Installation

**Download the binary for your platform from [Releases](../../releases/latest):**

| Platform | File |
|---|---|
| Windows (64-bit) | `arlis-mcp-windows-amd64.exe` |
| macOS (Apple Silicon) | `arlis-mcp-macos-arm64` |
| macOS (Intel) | `arlis-mcp-macos-amd64` |
| Linux (64-bit) | `arlis-mcp-linux-amd64` |

On macOS/Linux, make it executable after downloading:
```bash
chmod +x arlis-mcp-*
```

## Usage with Claude Desktop

Add to your `claude_desktop_config.json`:

**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`  
**Windows:** `%APPDATA%\Claude\claude_desktop_config.json`  
**Linux:** `~/.config/claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "arlis": {
      "command": "/path/to/arlis-mcp"
    }
  }
}
```

Replace `/path/to/arlis-mcp` with the actual path to the downloaded binary (e.g. `C:\Users\you\Downloads\arlis-mcp-windows-amd64.exe` on Windows).

## Usage with Claude Code

```bash
claude mcp add arlis -- /path/to/arlis-mcp
```

## Tools

### `search_acts`
Search the ARLIS database by keyword, act number, year, and more.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `query` | string | — | Keyword(s) to search for |
| `number` | string | — | Act number (e.g. `HO-239`) |
| `year` | integer | — | Enactment year |
| `lang` | `en`/`hy`/`ru` | `en` | Language |
| `page` | integer | `1` | Results page |
| `catalog` | `all`/`EEU`/`ECHR`/`cassation`/`yerevan` | `all` | Document collection |
| `text_filter` | `all`/`title`/`body` | `all` | Where to search |
| `exact_match` | boolean | `false` | Require exact phrase |
| `exclude_amending` | boolean | `false` | Exclude amending acts |
| `order_by` | `title`/`enactment_date`/`effective_date` | — | Sort field |
| `order_dir` | `ASC`/`DESC` | `ASC` | Sort direction |

### `get_act`
Fetch metadata and body text of a specific act by its numeric ARLIS ID.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `act_id` | integer | **required** | Numeric ARLIS ID (e.g. `205622` for the Civil Code) |
| `lang` | `en`/`hy`/`ru` | `en` | Language version |
| `body_offset` | integer | `0` | Character offset for pagination |
| `body_chunk_size` | integer | `20000` | Characters to return (max 40000) |

Large documents (codes, etc.) are paginated. Check `body_has_more` and advance `body_offset` by `body_chunk_size` to read the full text.

### `get_recent_acts`
Return the most recently published acts.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `page` | integer | `1` | Page number |
| `lang` | `en`/`hy`/`ru` | `en` | Language |

## Example queries

- *"What are the latest acts published in Armenia?"*
- *"Find Armenian laws about intellectual property"*
- *"Get the full text of the Labor Code"*
- *"Search for ECHR judgments mentioning freedom of expression"*
- *"What does Armenian law say about LLC formation?"*

## Building from source

Requires [Go 1.22+](https://go.dev/dl/).

```bash
git clone https://github.com/hrantt/arlis-mcp
cd arlis-mcp
go build -o arlis-mcp .
```

## Data source

All data is fetched live from [arlis.am](https://www.arlis.am). Supports Armenian (`hy`), English (`en`), and Russian (`ru`) language versions where available. The English translation covers major codes and laws but not all acts.
