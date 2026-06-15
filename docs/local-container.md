---
title: Managing the local container
description: Start, stop, update, and reset the shared pgit database container, and manage the per-repository databases inside it.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/local-container
  type: how-to
  applies_to:
    - pgit 4
    - docker
    - podman
  language: en
  difficulty: beginner
  time_estimate: 8m
  status: stable
  aliases:
    - pgit local
    - pgit repos
    - local database
    - container management
    - pgit doctor
  prev: ./remotes.md
  next: ./configuration-and-tuning.md
---

# Managing the local container

pgit keeps your local data in a single PostgreSQL container, running the pg-xpatch image. One container is shared across every pgit repository on your machine, and each repository gets its own database inside it. The `pgit local` and `pgit repos` commands manage that container and the databases within it.

## The lifecycle

`pgit local` groups the container operations:

```bash
pgit local status    # runtime, container state, port, image, volume size
pgit local start     # start it (or create it on first run)
pgit local stop      # stop without removing
pgit local logs      # container logs (-n / --tail to limit lines)
pgit local destroy   # remove the container (data is kept by default)
```

Most of the time you do not run these by hand: an `import`, `commit`, or query starts the container automatically. You reach for `pgit local` to inspect state, free the port, or recover from a wedged container.

`pgit local start` uses port 5433 by default and picks the next free port if that one is taken. Pass `--port` to choose explicitly.

## Your data is in a named volume

The container stores PostgreSQL data in a named volume called `pgit-data`, separate from the container itself. This matters:

!!! check "Destroy keeps your data"
    `pgit local destroy` removes the container but leaves the volume intact, so you can recreate the container later and your repositories are still there. To delete the data too, add `--purge`. That one is irreversible.

```bash
pgit local destroy            # safe: container gone, data kept
pgit local destroy --purge    # destroys all pgit data permanently
```

This is also how you apply container configuration changes: settings are baked in at startup, so `pgit local destroy && pgit local start` recreates the container with new settings without losing data. See [Configuration and tuning](./configuration-and-tuning.md).

## Updating the database engine

`pgit local update` pulls the latest pg-xpatch image and recreates the container, preserving your data in the volume:

```bash
pgit local update --check   # see if a newer image exists
pgit local update           # pull and recreate
```

`pgit local status` also tells you, in passing, when an update is available.

!!! note "Legacy containers: migrate first"
    Very old pgit containers used an anonymous volume that would not survive removal. If `pgit local status` warns about a legacy container, run `pgit local migrate` once to move the data into the persistent named volume. Update and destroy then become safe.

## Managing repositories in the container

Because all repos share one container, you can list and tidy them from anywhere with `pgit repos`:

```bash
pgit repos                # list every pgit database (name, path, commits, size)
pgit repos cleanup        # drop databases whose directories no longer exist
pgit repos delete --force # delete the repo in the current directory
```

Each listed repo has a status:

- **ok**: the working directory still exists.
- **orphaned**: the database is here but its directory is gone. `pgit repos cleanup` removes these.
- **unknown**: no path was recorded yet. Run any pgit command inside that repo to register it, or use `--search` to locate it on disk.

`pgit repos delete` is destructive but narrow: it drops the database and removes the `.pgit` folder. Your actual project files are never touched. It requires `--force`, and accepts a path or a `pgit_...` database name as an argument.

!!! warning "cleanup and delete drop databases"
    `pgit repos cleanup` and `pgit repos delete` permanently remove database content. Use `pgit repos cleanup --dry-run` first to see what would go.

## Checking health

`pgit doctor` is the catch-all diagnostic. It verifies the container runtime, the container state, the database connection, and (inside a repo) your commit identity and remotes:

```bash
pgit doctor
```

Run it first whenever something is not behaving; it usually points straight at the problem.

## Updating the CLI itself

Separately from the database image, `pgit update` checks whether a newer pgit binary is available on GitHub and prints how to upgrade:

```bash
pgit update           # check and show install instructions
pgit update --check   # just check
```

It does not update in place; follow the printed instructions (for example, re-run `go install`).

## Where to go next

!!! cards { cols=2 }
    - [Configuration and tuning](./configuration-and-tuning.md){ icon=sliders }
      The settings baked into the container, and how to size them.

    - [Command reference](./commands.md){ icon=terminal }
      Every command and flag, including all of `local` and `repos`.
