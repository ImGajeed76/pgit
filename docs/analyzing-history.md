---
title: Analyzing history
description: The six pre-built pgit analyses (churn, coupling, hotspots, authors, activity, bus-factor) with their filters, sorting, and output formats.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/analyzing-history
  type: how-to
  applies_to:
    - pgit 4
  language: en
  difficulty: beginner
  time_estimate: 10m
  status: stable
  aliases:
    - pgit analyze
    - churn coupling hotspots
    - bus factor
    - repository analysis
  prev: ./importing-a-repo.md
  next: ./querying-with-sql.md
---

# Analyzing history

`pgit analyze` wraps optimized SQL behind one-word subcommands. Each one answers a common question about a codebase's history, and each is tuned for pgit's storage so you do not have to think about delta chains. There are six.

!!! cards { cols=3 }
    - **churn**{ icon=flame }
      Files changed in the most commits.

    - **coupling**{ icon=link }
      File pairs that change together.

    - **hotspots**{ icon=folder-tree }
      Churn rolled up by directory.

    - **authors**{ icon=users }
      Commits per contributor.

    - **activity**{ icon=trending-up }
      Commit volume over time.

    - **bus-factor**{ icon=alert-triangle }
      Files only one person touches.

## Shared options

Every subcommand accepts the same core flags:

| Flag | Default | Effect |
| ---- | ------- | ------ |
| `--limit`, `-n` | 25 | Maximum rows to show |
| `--path`, `-p` | (none) | Glob filter on file paths, for example `src/**/*.go` |
| `--sort` | per-analysis | Sort by a named column |
| `--reverse` | off | Flip the sort order |
| `--json` | off | Emit a JSON array |
| `--raw` | off | Tab-separated output, for piping |
| `--no-pager` | off | Plain table instead of the interactive viewer |
| `--remote` | (local) | Run against a configured remote database |
| `--timeout` | 5m | Abort if the analysis runs longer |

By default results open in an interactive table you can search, sort, expand columns, and copy from. `--json` and `--raw` switch to non-interactive output for scripts.

!!! tip "Path globs understand `**`"
    `--path "src/**/*.go"` matches `.go` files at any depth under `src/`. A plain `*` matches within a single path segment.

!!! tip "Analyze a remote without cloning"
    Every analysis takes `--remote <name>` to run against a configured remote database instead of your local one, for example `pgit analyze churn --remote origin`. The query runs where the data lives, so you can interrogate a shared database without pulling it down. See [Remotes](./remotes.md#reading-a-remote-without-cloning).

## churn

Rank files by how many commits touched them. Maintenance hotspots tend to surface here.

```bash
pgit analyze churn --limit 20
```

Columns: `path`, `versions`. This query reads only heap tables, so it stays fast on large repos. Default sort is `versions` descending.

## coupling

Find file pairs most often modified in the same commit. High coupling between unrelated files hints at a missing abstraction or a hidden dependency.

```bash
pgit analyze coupling --min 5
```

Columns: `file_a`, `file_b`, `commits_together`. Two extra flags shape it:

- `--min` (default 2) sets the minimum co-change count to include a pair.
- `--max-files` (default 100) skips commits that touch more than that many files, because a tree-wide reformat couples everything to everything and drowns out real signal.

The pair counting runs in Go rather than as a SQL self-join, which keeps it tractable on millions of file references.

## hotspots

Aggregate churn by directory, so you see which subsystems accumulate change rather than which individual files do.

```bash
pgit analyze hotspots --depth 2
```

Columns: `directory`, `files`, `total_versions`, `avg_versions`. `--depth` (default 1) controls how many directory levels to roll up to; depth 1 is top-level, depth 2 splits one level deeper. Files at the repository root are grouped under `(root)`.

## authors

Rank contributors by commit count, with the span of their activity.

```bash
pgit analyze authors
```

Columns: `author`, `email`, `commits`, `first_commit`, `last_commit`. This one reads commit metadata, so it streams `pgit_commits` once front-to-back (the cheapest way to read a delta chain). On a very large history it takes seconds rather than milliseconds.

## activity

Commit counts bucketed by time period, with empty periods included so the timeline has no gaps.

```bash
pgit analyze activity --period month --chart
```

Columns: `period`, `commits`, and optionally `bar`. Options:

- `--period` is `week`, `month` (default), `quarter`, or `year`.
- `--chart` adds an ASCII bar column for a quick visual.

Output is chronological, so `--sort` does not apply here; use `--reverse` to flip oldest-first to newest-first.

## bus-factor

For each file, count the distinct authors who have ever touched it. Files with a single author are knowledge silos: a bus-factor risk.

```bash
pgit analyze bus-factor --max-authors 1
```

Columns: `path`, `authors`, `author_list`. `--max-authors` (default 0, meaning no cap) shows only files with at most that many authors, so `--max-authors 1` lists the pure silos. Results sort by author count ascending, most vulnerable first.

## Output for scripts and agents

Anything you can see, you can pipe. `--json` gives structured rows; `--raw` gives tab-separated values:

```bash
pgit analyze churn --json | jq '.[0]'
pgit analyze coupling --raw | awk -F'\t' '$3 > 50'
```

When the built-in analyses do not answer your exact question, drop to SQL.

## Where to go next

!!! cards { cols=2 }
    - [Querying with SQL](./querying-with-sql.md){ icon=database }
      Write your own queries and search across history.

    - [Database schema reference](./database-schema.md){ icon=table }
      The tables and columns these analyses read.
