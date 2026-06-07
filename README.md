<p align="center"><img src="logo.jpg" width="128" alt="mim"></p>

# mim

A portable, dependency-free TUI code viewer with vim keybindings. Named after
Mím, the petty-dwarf of Tolkien mythology.

**Read-only.** No LSP, no code execution, no plugins. Safe to open untrusted
repositories. Copy text to clipboard and read diffs — that's it.

## Features

- **Vim navigation** — `hjkl`, `gg`, `G`, `0`, `$`, `Ctrl‑D/U/B`, visual
  selection (`v`/`V`), yank to system clipboard (OSC 52)
- **File tree** — respects `.gitignore` by default, toggle with `Ctrl‑A`
- **Syntax highlighting** — Go, Python, TypeScript (zero-regex scanner)
- **Find in files** — `Ctrl‑F`, shell to `grep`, navigate results with `j`/`k`
- **Git diff** — `Ctrl‑G` opens commit log, `v` selects range, `Enter` shows
  `git diff` with ANSI colors piped through
- **Zero dependencies** — single static binary, nothing fetched at build or runtime

## Install

```bash
go install github.com/MQ37/mim@latest
```

Requires Go 1.24+. The binary is ~2 MB, statically linked, no glibc dependency.
After install, `mim` is available as `~/go/bin/mim` — make sure `~/go/bin` is
in your `$PATH`.

## Usage

```
mim .           # open current directory
mim main.go     # open a single file
```

| Key | Action |
|---|---|
| `h j k l` | Move cursor |
| `gg` / `G` | Top / bottom of file |
| `0` / `$` | Start / end of line |
| `Ctrl‑D` / `Ctrl‑U` | Half page down / up |
| `Ctrl‑E` | Hide / show file tree |
| `Ctrl‑T` | Toggle focus (tree ↔ viewer) |
| `Ctrl‑F` | Find in files |
| `Ctrl‑G` | Git diff view |
| `v` / `V` | Visual selection (charwise / linewise) |
| `y` | Yank selection to clipboard |
| `q` | Quit |

## Philosophy

Dependencies are liabilities. Every library you link is code you didn't read,
written by people you don't know, executing in your process. mim runs nothing
but the Go standard library — no `npm`, no `pip`, no `cargo`, no `apt`.
One binary, one directory, zero trust required.
