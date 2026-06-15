---
title: What pgit is
description: A Git-like CLI that imports a repo into PostgreSQL so you can run SQL, pre-built analyses, and cross-history search on its commit history.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/overview
  type: explanation
  applies_to:
    - pgit 4
  language: en
  difficulty: beginner
  time_estimate: 5m
  status: stable
  aliases:
    - what is pgit
    - pgit overview
    - git in postgres
    - sql git history
  next: ./installation.md
---

# What pgit is

pgit is a Git-like version control CLI where the repository lives in PostgreSQL instead of on the filesystem. You import a git repo, and every version of every file lands in a handful of database tables. From there you ask questions: which files change together, who owns which subsystem, what the busiest week of the year was. No scripts, no parsing `git log`, no piping things through `awk`. Just answers.

The storage underneath is [pg-xpatch](/imgajeed/pg-xpatch/overview), a PostgreSQL extension that keeps a thousand near-identical versions of a file for about the cost of one. pgit is, in the author's words, primarily a demo for that compression. It also happens to be a genuinely useful way to interrogate a codebase's history.

## What it is good at

!!! cards { cols=2 }
    - **Importing a git repo**{ icon=download }
      Pull a full history (merges, renames, real author dates) into Postgres with one command.

    - **Pre-built analyses**{ icon=chart-bar }
      `churn`, `coupling`, `hotspots`, `authors`, `activity`, `bus-factor`. One command each, no SQL.

    - **Raw SQL**{ icon=database }
      Everything is in tables you can query directly when a built-in analysis is not enough.

    - **Search across history**{ icon=search }
      Grep every version of every file, not just the current checkout.

## What it is not

pgit is not trying to replace git for day-to-day development. Its history is **append-only**: an imported repo is immutable, and the primary loop is `import`, then `analyze` / `sql` / `search`. The `reset` and `resolve` commands that earlier versions had were removed in v4 for exactly this reason.

!!! note "It can still act like git"
    `init`, `add`, `commit`, `log`, `diff`, `checkout`, `blame`, and friends all exist and behave the way you expect. They are a real (if secondary) workflow. But the reason to reach for pgit is the analysis, not the daily commit.

## How it fits together

Three pieces do the work:

- **The CLI** (`pgit`), a single Go binary with no runtime dependencies of its own.
- **A local PostgreSQL container** running the pg-xpatch image, shared across every pgit repo on your machine. Each repo gets its own database inside it. This is why pgit needs Docker or Podman.
- **pg-xpatch**, the table access method that delta-compresses the versioned content. pgit configures it; you rarely touch it directly.

A PostgreSQL connection URL doubles as your "remote". There is no separate auth system: if you can reach a pg-xpatch database, you can `push`, `pull`, and `clone` against it.

## When it pays off

pgit shines when you want to *understand* a repository rather than develop in it:

- finding hidden coupling between files that always change together
- spotting maintenance hotspots and knowledge silos (bus-factor)
- charting commit velocity over years
- running any custom analytics you can express in SQL

It is a poorer fit when you just need a working tree and fast local commits; git already does that better. And the compression is competitive with git, not magic: it wins on source-heavy repos with incremental changes and loses on repos full of large binary or vendored assets. [Compression vs git](./compression.md) has the honest numbers.

## Where to go next

!!! cards { cols=2 }
    - [Install pgit](./installation.md){ icon=download }
      Go, a prebuilt binary, or a system package, plus the one runtime requirement.

    - [Quickstart](./quickstart.md){ icon=rocket }
      Import a real repo and run your first analysis in a few minutes.

    - [How it works](./how-it-works.md){ icon=layers }
      The tables, the delta groups, and why the queries are shaped the way they are.

    - [What pg-xpatch is](/imgajeed/pg-xpatch/overview){ icon=box }
      The compression engine underneath, documented separately.
