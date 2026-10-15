# planviewer

Browse the plans Claude Code writes in plan mode (`~/.claude/plans/*.md`), from the terminal or a browser.

- Full-text search across titles, filenames, projects and plan bodies, with highlighted snippets
- Sort by updated, created, title, project or size
- Rendered markdown, plus raw view
- If `~/.claude/projects/` is present, session transcripts are used to recover each plan's exact creation time and originating project; otherwise creation time falls back to file mtime (shown as `~`)

## Install

```sh
go install github.com/nbr23/planviewer@latest
```

Or build from source: `go build -o planviewer .`

Both commands read from `$CLAUDE_CONFIG_DIR`, defaulting to `~/.claude`. Override with `-d <dir>`.

## CLI

```sh
planviewer          # same as: planviewer tui
```

List view: type to search, `↑/↓` move, `enter` open, `tab` cycle sort key, `shift+tab` reverse, `ctrl+r` reload, `esc` clear search / quit.

Plan view uses `less`-style keys: `j/k`, `space/b`, `g/G`, `/` and `?` to search, `n/N` next/previous match, `%` / `J` go to percent / line, `h` help, `q` back.

Markdown style follows the terminal background; set `GLAMOUR_STYLE` (e.g. `dark`, `light`, `dracula`) to override.

## Web

```sh
planviewer serve                 # http://localhost:8080
planviewer serve -addr :9000     # or ADDR=:9000
```

`/` focuses search, arrows + `enter` open a plan, column headers sort. On a plan page, `r` toggles raw view and `esc` goes back.

### Docker

```sh
docker compose up -d
```

Mounts `${CLAUDE_CONFIG_DIR:-~/.claude}/{plans,projects}` read-only and binds to `127.0.0.1:${PORT:-8080}`. Transcripts are mode `0600`, so on Linux set `HOST_UID`/`HOST_GID` to their owner if it isn't `1000`.
