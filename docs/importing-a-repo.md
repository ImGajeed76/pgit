---
title: Importing a git repository
description: Import a git history into pgit in depth, covering branches, parallel workers, resuming an interrupted run, and importing straight into a remote.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/importing-a-repo
  type: how-to
  applies_to:
    - pgit 4
    - docker
    - podman
    - git
  language: en
  difficulty: intermediate
  time_estimate: 10m
  status: stable
  aliases:
    - pgit import
    - import git history
    - import a repository
    - resume import
  prev: ./compression.md
  next: ./analyzing-history.md
---

# Importing a git repository

`pgit import` is the command you will reach for most. It reads a git repository's full history and writes it into pgit's tables with delta compression. This guide covers the options that matter once you move past the basics.

## The basics

Run `import` from inside a pgit workspace (`pgit init` first) and point it at a git repo:

```bash
pgit init
pgit import /path/to/repo --branch main
```

The path argument defaults to the current directory, so `pgit import` with no path imports the repo you are standing in. The target must contain a `.git` directory.

Under the hood, pgit runs `git fast-export` (with `--reencode=yes --show-original-ids`) so it gets correct handling of merges, renames, and full commit messages, then streams the content into PostgreSQL through a pool of workers. Locally, the database container starts automatically if it is not already running.

## Choosing a branch

```bash
pgit import /path/to/repo --branch develop
```

Leave `--branch` off and pgit imports the current branch, or shows an interactive picker if the repo has several branches. Each import brings in one branch's history.

## Parallel workers

Blob import is parallelized. Control the worker count with `--workers` (`-w`):

```bash
pgit import /path/to/repo --workers 8
```

The default comes from `import.workers` in your global config (a conservative 3 on a typical laptop), and pgit caps the effective count at the number of CPU cores, since more workers than cores only adds contention. On a big machine, raising both the config default and this flag is the single biggest lever on import speed.

!!! tip "Tuning for a large import"
    Worker count is only half the story. For a multi-million-commit import, the container's memory and xpatch cache settings matter just as much. See [Configuration and tuning](./configuration-and-tuning.md) for a worked profile.

## Resuming an interrupted import

Large imports can take a while, and pgit tracks its progress so you do not have to start over. If a run is interrupted, re-running `import` tells you what state the database is in. To continue from where it stopped:

```bash
pgit import /path/to/repo --resume
```

Resume picks up the blob phase when commits are already in, or skips already-inserted commits otherwise. The states pgit distinguishes:

| Situation | What pgit does |
| --------- | -------------- |
| Database empty | Normal import |
| Commits inserted, blobs incomplete | `--resume` continues the blob phase |
| Import already complete | Refuses, unless you pass `--force` |

## Re-importing and `--force`

Imported history is immutable, so pgit will not quietly overwrite a finished import. To wipe the database and start fresh, pass `--force`:

```bash
pgit import /path/to/repo --force
```

!!! warning "Schema upgrades require a re-import"
    pgit's storage schema is versioned. When you upgrade pgit to a version with a newer schema, an existing database is rejected with a message telling you to re-import (`pgit import --force`). This is by design: the layout is append-only and optimized per version, so there is no in-place migration.

## Importing straight into a remote

By default `import` writes to your local container. To import directly into a remote pg-xpatch database (skipping the local container entirely), name a configured remote:

```bash
pgit remote add origin postgres://user:pass@host:5432/mydb
pgit import /path/to/repo --remote origin
```

pgit initializes the schema on the remote if needed. This is useful for populating a shared database without a local copy. See [Remotes](./remotes.md) for the connection model.

## Reusing a fast-export file

If you already have a `git fast-export` stream (or want to export once and import several times), skip the re-export with `--fastexport`:

```bash
git fast-export --reencode=yes --show-original-ids master > repo.stream
pgit import /path/to/repo --fastexport repo.stream
```

## Other flags

- `--dry-run` (`-n`) reports what would be imported without writing anything.
- `--timeout` bounds the whole operation (default 24h); raise it for very large repos, for example `--timeout 48h`.

## What gets stored

After import you have the full history in queryable tables: commits, every file version, the path-to-group mapping, and the commit graph. From here the repository is read-only and ready for analysis.

## Where to go next

!!! cards { cols=2 }
    - [Analyzing history](./analyzing-history.md){ icon=chart-bar }
      Run the pre-built analyses on what you just imported.

    - [Querying with SQL](./querying-with-sql.md){ icon=database }
      Ask anything the built-ins do not cover.

    - [Configuration and tuning](./configuration-and-tuning.md){ icon=sliders }
      Make a large import faster.
