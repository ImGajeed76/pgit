---
title: Command reference
description: Every pgit command and its flags, grouped by what you use it for.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/commands
  type: reference
  applies_to:
    - pgit 4
  language: en
  difficulty: intermediate
  time_estimate: 10m
  status: stable
  aliases:
    - pgit commands
    - pgit cli reference
    - pgit flags
    - command list
  prev: ./configuration-and-tuning.md
  next: ./database-schema.md
---

# Command reference

Every pgit command, grouped by task. Run `pgit <command> --help` for the authoritative, version-specific details; this page is the map.

## Global flags

These apply to every command:

| Flag | Effect |
| ---- | ------ |
| `--verbose`, `-v` | More output |
| `--no-color` | Disable colored output |

`pgit version` prints the build version, commit, and date.

## Setup

| Command | Description |
| ------- | ----------- |
| `pgit init [path]` | Create a `.pgit` repository and detect the container runtime |
| `pgit doctor` | Diagnose runtime, container, database, and identity |
| `pgit config <key> [value]` | Get or set configuration |

`pgit config` flags: `--list` (`-l`) lists everything, `--global` (`-g`) targets global instead of repository config. See [Configuration and tuning](./configuration-and-tuning.md).

## Working with files

The native, git-like workflow:

| Command | Description |
| ------- | ----------- |
| `pgit add [path]...` | Stage file contents |
| `pgit rm <file>...` | Remove files and stage the deletion |
| `pgit mv <src> <dst>` | Move or rename a file and stage it |
| `pgit status` | Show the working tree status |
| `pgit commit` | Record staged changes |
| `pgit checkout [commit] [--] [path...]` | Restore working tree files |
| `pgit clean` | Remove untracked files |

Flags:

- `add`: `--all` (`-A`) includes untracked files, `--verbose` (`-v`).
- `rm`: `--cached` removes from tracking but keeps the file, `--recursive` (`-r`), `--force` (`-f`).
- `mv`: `--force` (`-f`) overwrites an existing destination.
- `status`: `--short` (`-s`), `--json`.
- `commit`: `--message` (`-m`), `--author` (`-a`) in `"Name <email>"` form. Without `-m`, your editor opens (`$PGIT_EDITOR`, `$VISUAL`, `$EDITOR`, then vi/vim/nano/notepad).
- `checkout`: `--force` (`-f`) discards local changes.
- `clean`: `--force` (`-f`, required to actually delete), `--dry-run` (`-n`), `--directories` (`-d`).

## Inspecting history

| Command | Description |
| ------- | ----------- |
| `pgit log [commit]` | Show commit history |
| `pgit show [commit] \| [commit:path]` | Show a commit or a file at a commit |
| `pgit diff [<commit>] [<commit>..<commit>] [--] [path...]` | Show changes |
| `pgit blame <file>` | Line-by-line last-change attribution |
| `pgit search <pattern>` | Search file content across history (alias: `pgit grep`) |

Flags:

- `log`: `--max-count` (`-n`) or `--limit`, `--oneline`, `--graph`, `--no-pager`, `--json`, `--remote`.
- `show`: `--stat`, `--no-patch`, `--unified` (`-U`, default 3), `--remote`.
- `diff`: `--staged` (or `--cached`), `--name-only`, `--name-status`, `--stat`, `--no-color`, `--unified` (`-U`, default 3), `--remote`.
- `blame`: `--remote`.
- `search` / `grep`: `--ignore-case` (`-i`), `--path` (`-p`) glob, `--limit` (`-n`, default 50), `--all` (every version), `--commit` (at one commit), `--no-group` (only with `--all`), `--remote`. See [Querying with SQL and search](./querying-with-sql.md).

## Analysis and queries

| Command | Description |
| ------- | ----------- |
| `pgit analyze <name>` | Run a pre-built analysis |
| `pgit sql [query]` | Run SQL on the repository database |
| `pgit stats` | Repository and compression statistics |

`analyze` subcommands are `churn`, `coupling`, `hotspots`, `authors`, `activity`, `bus-factor`. They share `--limit` (`-n`, 25), `--path` (`-p`), `--json`, `--raw`, `--no-pager`, `--remote`, `--sort`, `--reverse`, `--timeout` (5m), plus a few of their own (`coupling --min/--max-files`, `hotspots --depth`, `activity --period/--chart`, `bus-factor --max-authors`). See [Analyzing history](./analyzing-history.md).

`sql` flags: `--write` (allow INSERT/UPDATE/DELETE), `--raw`, `--json`, `--no-pager`, `--timeout` (seconds, 60), `--remote`. Subcommands: `sql schema [table]`, `sql tables`, `sql examples`.

`stats` flags: `--xpatch` (detailed compression stats), `--json`, `--remote`.

## Importing

| Command | Description |
| ------- | ----------- |
| `pgit import [git-repo-path]` | Import a git repository |

Flags: `--workers` (`-w`), `--branch` (`-b`), `--dry-run` (`-n`), `--force` (`-f`), `--resume`, `--fastexport <file>`, `--remote <name>`, `--timeout` (default 24h). See [Importing a repository](./importing-a-repo.md).

## Remotes

| Command | Description |
| ------- | ----------- |
| `pgit remote` | List remotes (`-v` shows URLs) |
| `pgit remote add <name> <url>` | Add a remote |
| `pgit remote remove <name>` | Remove a remote (alias `rm`) |
| `pgit remote set-url <name> <url>` | Change a remote's URL |
| `pgit push [remote]` | Push to a remote (default `origin`) |
| `pgit pull [remote]` | Pull from a remote (default `origin`) |
| `pgit clone <url> [directory]` | Clone from a remote URL |

Flags: `push --force` (`-f`), `pull --rebase`, `clone --force` (`-f`). See [Remotes, push, pull, and clone](./remotes.md).

## Local container

| Command | Description |
| ------- | ----------- |
| `pgit local status` | Container status, port, image, volume |
| `pgit local start` | Start the container (`--port`, `-p`) |
| `pgit local stop` | Stop the container |
| `pgit local logs` | Container logs (`--tail`, `-n`, default 50) |
| `pgit local destroy` | Remove the container (`--purge` also deletes data) |
| `pgit local migrate` | Migrate a legacy container to a named volume |
| `pgit local update` | Update the pg-xpatch image (`--check`) |
| `pgit repos` | List databases in the container |
| `pgit repos cleanup` | Drop orphaned databases (`--dry-run`, `--unknown`) |
| `pgit repos delete [path\|database]` | Delete a repository (`--force`, `--search`) |

`pgit repos` and `pgit repos list` also accept `--json`, `--search`, `--path`, and `--depth`. See [Managing the local container](./local-container.md).

## Maintenance

| Command | Description |
| ------- | ----------- |
| `pgit update` | Check for a newer pgit release (`--check`) |
| `pgit completion <shell>` | Generate shell completions (bash, zsh, fish, powershell) |
| `pgit version` | Print version information |

## Where to go next

!!! cards { cols=2 }
    - [Database schema reference](./database-schema.md){ icon=table }
      The tables behind `sql`, `stats`, and the analyses.

    - [Overview](./overview.md){ icon=book-open }
      Back to the start, if you landed here first.
