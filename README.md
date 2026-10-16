# planviewer

Browse the plans Claude Code writes in plan mode (`~/.claude/plans/*.md`) and search your past sessions to find the one to resume, from the terminal or a browser.

- Full-text search across titles, filenames, projects and plan bodies, with highlighted snippets
- Sort by updated, created, title, project or size
- Rendered markdown, plus raw view
- If `~/.claude/projects/` is present, session transcripts are used to recover each plan's exact creation time and originating project; otherwise creation time falls back to file mtime (shown as `~`)
- Sessions: search transcripts by title (`/rename` or the generated one), project, git branch, your prompts and Claude's replies, then read the conversation and get the command to resume it. Tool calls, tool output and thinking are not indexed.

## Install

```sh
go install github.com/nbr23/planviewer@latest
```

Or build from source: `go build -o planviewer .`

Both commands read from `$CLAUDE_CONFIG_DIR`, defaulting to `~/.claude`. Override with `-d <dir>`.

The parsed transcript index is cached in your user cache directory (`~/.cache/planviewer` on Linux), so later launches only read what was appended since. Change the location with `-cache-dir <dir>`, or disable caching with `-cache-dir=`.

## CLI

```sh
planviewer          # same as: planviewer tui
```

List view: type to search, `↑/↓` move, `enter` open, `tab` cycle sort key, `shift+tab` reverse, `ctrl+t` switch between plans and sessions, `ctrl+r` reload, `esc` clear search / quit.

Plan and session views use `less`-style keys: `j/k`, `space/b`, `g/G`, `/` and `?` to search, `n/N` next/previous match, `%` / `J` go to percent / line, `h` help, `q` back. In a session, the header shows the command to resume it (`cd ~/<project> && claude --resume <id>`), and `r` quits and prints it for you to run.

Markdown style follows the terminal background; set `GLAMOUR_STYLE` (e.g. `dark`, `light`, `dracula`) to override.

## Web

```sh
planviewer serve                 # http://localhost:8080
planviewer serve -addr :9000     # or ADDR=:9000
```

`/` focuses search, arrows + `enter` open a plan, column headers sort. On a plan page, `r` toggles raw view and `esc` goes back. `/sessions` lists sessions the same way; a session page shows the conversation and a copyable resume command.

The sessions pages serve your prompts and Claude's replies, so keep the listener on localhost unless you mean to share them.

### Docker

```sh
docker compose up -d
```

Mounts `${CLAUDE_CONFIG_DIR:-~/.claude}/{plans,projects}` read-only and binds to `127.0.0.1:${PORT:-8080}`. Transcripts are mode `0600`, so on Linux set `HOST_UID`/`HOST_GID` to their owner if it isn't `1000`.
