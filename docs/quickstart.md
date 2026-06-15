---
title: Quickstart
description: Import a real git repository into pgit and run your first analyses and SQL queries, start to finish, in about ten minutes.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/quickstart
  type: tutorial
  applies_to:
    - pgit 4
    - docker
    - podman
  language: en
  difficulty: beginner
  time_estimate: 10m
  status: stable
  aliases:
    - pgit quickstart
    - getting started with pgit
    - import a repo
    - first analysis
  prev: ./installation.md
  next: ./how-it-works.md
---

# Quickstart

The fastest way to understand pgit is to point it at a repository you already know and start asking questions. In this walkthrough you import a real git repo, run a couple of built-in analyses, drop into SQL, and search across history. By the end you will have the whole loop in your hands.

You need pgit installed and a working Docker or Podman. If `pgit doctor` is happy, you are set. If not, start at [Installation](./installation.md).

## 1. Get a repository to play with

Any git repo works. If you do not have one handy, clone a small public project:

```bash
git clone https://github.com/junegunn/fzf
```

## 2. Initialize a pgit workspace

Make a separate directory for the analysis and initialize pgit there. Keeping it apart from the source repo is tidy, not required.

```bash
mkdir fzf-analysis && cd fzf-analysis
pgit init
```

`pgit init` creates a `.pgit/` directory for local config and detects your container runtime. It does not start the database yet; the next command does that for you.

## 3. Import the history

Point `import` at the git repo and pick a branch:

```bash
pgit import ../fzf --branch master
```

A few things happen on this first run:

- the local pg-xpatch container starts (and the image is pulled if needed),
- pgit runs `git fast-export` to read the full history, including merges, renames, and real author dates,
- a worker pool streams the content into PostgreSQL with delta compression.

!!! tip "Not sure which branch?"
    Leave off `--branch` and pgit imports the current branch, or shows a picker when there is more than one. Pass `--branch <name>` to skip straight to one.

When it finishes, every version of every file is in the database, ready to query.

## 4. Run your first analyses

The `analyze` commands wrap optimized queries behind one-word subcommands. Start with churn, the files that changed the most:

```bash
pgit analyze churn
```

Then coupling, the files that tend to change together:

```bash
pgit analyze coupling
```

```
file_a           file_b          commits_together
───────────────  ──────────────  ────────────────
CHANGELOG.md     man/man1/fzf.1  311
src/terminal.go  src/options.go  288
man/man1/fzf.1   src/options.go  276
```

!!! note "Your numbers will differ"
    That is the real top of fzf's coupling at the time of writing. Your output depends on the repo you imported and when, and results open in an interactive viewer you can search, sort, and copy from. Add `--json` or `--raw` to pipe the data somewhere else.

Every analysis takes `--limit`, a `--path` glob filter, and sort flags. [Analyzing history](./analyzing-history.md) covers all six.

## 5. Drop into SQL

When a built-in analysis is not exactly what you want, the data is all in plain tables. Ask for the ten most recent commits:

```bash
pgit sql "SELECT id, author_name, message FROM pgit_commits ORDER BY seq DESC LIMIT 10"
```

Not sure what is in there? pgit documents its own schema:

```bash
pgit sql schema          # all tables
pgit sql schema pgit_commits   # one table's columns
pgit sql examples        # ready-to-run example queries
```

SQL is read-only by default, so you cannot corrupt the import by exploring. [Querying with SQL](./querying-with-sql.md) goes deeper.

## 6. Search across every version

Unlike `git grep`, pgit can search history, not just the current checkout:

```bash
pgit search "TODO" --path "*.go"            # latest version of each file
pgit search --all "panic\(" --ignore-case   # every version ever (the pattern is a regex, so escape the paren)
```

## What you just did

!!! steps
    1. Initialized a pgit workspace with `pgit init`.
    2. Imported a full git history with `pgit import`.
    3. Ran `analyze churn` and `analyze coupling`.
    4. Queried the raw tables with `pgit sql`.
    5. Searched across all of history with `pgit search`.

That is the core loop: **import, then ask**.

## Optional: the native git-like workflow

pgit can also record commits itself, no import required. This is the secondary use case, but it is there when you want it:

```bash
pgit config user.name "Your Name"
pgit config user.email "you@example.com"

pgit add .                       # stage everything (-A includes untracked)
pgit commit -m "Initial commit"  # or omit -m to open your editor
pgit log
```

Remember that imported history is append-only, so this workflow is best on a repo you started in pgit rather than one you imported.

## Where to go next

!!! cards { cols=2 }
    - [How it works](./how-it-works.md){ icon=layers }
      The tables behind these commands, and why the queries are shaped this way.

    - [Importing a repository](./importing-a-repo.md){ icon=download }
      Branches, workers, resuming, and importing straight into a remote.

    - [Analyzing history](./analyzing-history.md){ icon=chart-bar }
      All six analyses, with filters and output formats.

    - [Querying with SQL](./querying-with-sql.md){ icon=database }
      Custom queries, the schema, and search.
