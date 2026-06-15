---
title: Remotes, push, pull, and clone
description: Use a PostgreSQL connection URL as a pgit remote to push, pull, and clone repositories, and how to run your own remote.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/remotes
  type: how-to
  applies_to:
    - pgit 4
    - postgres 16
    - docker
    - podman
  language: en
  difficulty: intermediate
  time_estimate: 9m
  status: stable
  aliases:
    - pgit push
    - pgit pull
    - pgit clone
    - pgit remote
    - postgres remote
  prev: ./querying-with-sql.md
  next: ./local-container.md
---

# Remotes, push, pull, and clone

A pgit remote is just a PostgreSQL database running pg-xpatch. There is no separate account system or transport: the connection URL is the remote, and whatever credentials it carries are the auth. If you can reach the database, you can sync with it.

!!! info "What a remote is"
    Any pg-xpatch PostgreSQL database can be a remote. That includes a managed Postgres with the extension, a container you run yourself, or even another machine's pgit container. The URL is a standard connection string, for example `postgres://user:pass@host:5432/dbname`.

## Managing remotes

Remotes are named, like git's. Add one, list them, change a URL, or remove one:

```bash
pgit remote add origin postgres://user:pass@host:5432/mydb
pgit remote                # list remote names
pgit remote -v             # list with URLs
pgit remote set-url origin postgres://user:pass@newhost:5432/mydb
pgit remote remove origin  # 'rm' also works
```

Remote definitions live in your repository config (`remote.<name>.url`), so they travel with the `.pgit` directory, not globally.

## Pushing

`pgit push` sends local commits (and their content) to a remote. With no argument it targets `origin`:

```bash
pgit push            # push to origin
pgit push myremote   # push to a named remote
```

The first push to an empty remote sends everything and initializes the schema there. Later pushes are incremental, sending only the commits the remote does not have yet.

!!! warning "Push refuses to overwrite divergent history"
    If the remote has commits you do not have locally, push is rejected as a non-fast-forward, the same idea as git. Pull first to reconcile, or force the overwrite with `pgit push --force` if you are certain you want the remote to match your local history.

## Pulling

`pgit pull` fetches from a remote (default `origin`) and integrates:

```bash
pgit pull            # fetch from origin and merge
pgit pull --rebase   # replay your local commits on top of the remote
```

When local and remote have diverged, the default is a three-way merge: pgit finds the common ancestor, pulls the remote commits, and writes conflict markers into any file changed on both sides. Fix the conflicts, then `pgit add <file>` and `pgit commit` to finish, exactly the git muscle memory. With `--rebase`, pgit instead resets to the remote head and replays your local commits on top, giving them new commit IDs.

## Cloning

`pgit clone` creates a fresh local repository from a remote URL:

```bash
pgit clone postgres://user:pass@host:5432/mydb
pgit clone postgres://user:pass@host:5432/mydb my-dir
```

Cloning verifies the remote is a pgit database, sets it up as `origin`, copies the full history into your local container, and checks out the working tree at the remote head.

!!! note "Default directory"
    If you do not pass a directory, pgit clones into a directory named `pgit-clone` in the current folder. Pass a second argument to choose the name. Use `--force` to overwrite an existing local database without the confirmation prompt.

## Reading a remote without cloning

Cloning copies a whole history onto your machine. Often you only want to ask a question, and for that you do not need a local copy at all. The read-only commands accept `--remote <name>` and run against the remote database in place:

```bash
pgit analyze churn --remote origin
pgit sql "SELECT COUNT(*) FROM pgit_paths" --remote origin
pgit search "TODO" --remote origin
pgit stats --remote origin
```

`--remote` is available on `analyze`, `sql`, `search`, `stats`, `log`, `show`, `blame`, and `diff` (the last needs a commit range). It is the quickest way to inspect a shared database, and the analyses run the same optimized queries they would locally.

## Running your own remote

The same pg-xpatch image that powers the local container makes a perfectly good remote. Start one on a spare port and point a remote at it:

```bash
docker run -d --name pgit-remote \
  -e POSTGRES_USER=pgit \
  -e POSTGRES_PASSWORD=pgit \
  -e POSTGRES_DB=myproject \
  -p 5444:5432 \
  ghcr.io/imgajeed76/pg-xpatch:latest

pgit remote add origin postgres://pgit:pgit@localhost:5444/myproject
pgit push origin
```

The database name is yours to choose; it just has to match in the container and the URL. pgit creates the schema on first push. (The example uses host port 5444 so it does not collide with the local container's default 5433.)

## Importing straight into a remote

If you want to populate a remote without keeping a local copy, `import` can target one directly:

```bash
pgit import /path/to/repo --remote origin
```

This skips the local container and writes the imported history into the remote database. See [Importing a repository](./importing-a-repo.md) for the rest of the import options.

## Where to go next

!!! cards { cols=2 }
    - [Managing the local container](./local-container.md){ icon=container }
      The container that backs your local database (and can act as a remote).

    - [Importing a repository](./importing-a-repo.md){ icon=download }
      Including importing directly into a remote.
