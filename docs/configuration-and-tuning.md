---
title: Configuration and tuning
description: Configure pgit at the repository and global level, set environment variables, and tune the database container for your hardware.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/configuration-and-tuning
  type: how-to
  applies_to:
    - pgit 4
    - docker
    - podman
  language: en
  difficulty: intermediate
  time_estimate: 12m
  status: stable
  aliases:
    - pgit config
    - performance tuning
    - container tuning
    - pgit settings
    - environment variables
  prev: ./local-container.md
  next: ./commands.md
---

# Configuration and tuning

pgit has two layers of configuration: per-repository settings (who you are, your remotes) and global settings (the database container and import defaults, shared by every repo). The default container settings are deliberately conservative, sized for an 8 GB laptop, so the main reason to tune is to make large imports and queries faster on a bigger machine.

## The two scopes

- **Repository config** lives in `.pgit/config.toml` inside each repo. It holds your commit identity and remotes.
- **Global config** lives in your user config directory and applies everywhere: `~/.config/pgit/config.toml` on Linux (or `$XDG_CONFIG_HOME/pgit`), `~/Library/Application Support/pgit/config.toml` on macOS, and `%APPDATA%\pgit\config.toml` on Windows.

You rarely edit these files by hand. Use the `config` command.

## The config command

```bash
pgit config user.name "Ada Lovelace"   # set a repo value
pgit config user.name                  # get a repo value
pgit config --list                     # list repo config

pgit config --global --list                       # list global config
pgit config --global container.shared_buffers 2GB # set a global value
pgit config --global container.shared_buffers     # get a global value
```

Values are validated when you set them, so an out-of-range port or a malformed size is rejected rather than silently breaking the container.

## Repository settings

| Key | Meaning |
| --- | ------- |
| `user.name` | Author name recorded on commits |
| `user.email` | Author email recorded on commits |
| `remote.<name>.url` | A remote's connection URL (see [Remotes](./remotes.md)) |
| `core.local_db` | The repo's database name (read-only, derived from the path) |

If `user.name` or `user.email` is unset, pgit falls back to the `PGIT_AUTHOR_NAME` / `PGIT_AUTHOR_EMAIL` environment variables, then to your git config. You can also set a global default identity with `pgit config --global user.name "..."`.

## Global settings and the restart rule

Global config covers the PostgreSQL container (`container.*`) and the import default (`import.workers`). The container reads these once, at startup.

!!! warning "Container settings need a restart"
    Changing any `container.*` value does not affect a running container. Apply it by recreating the container, which preserves your data:

    ```bash
    pgit local destroy && pgit local start
    ```

## Environment variables

A handful of variables tweak behaviour without touching config files:

| Variable | Effect |
| -------- | ------ |
| `PGIT_CONTAINER_RUNTIME` | Force `docker` or `podman` instead of auto-detect |
| `PGIT_ACCESSIBLE` | Set to `1` for accessibility mode (no animations) |
| `NO_COLOR` | Disable colored output |
| `PGIT_AUTHOR_NAME` / `PGIT_AUTHOR_EMAIL` | Commit identity fallback |
| `PGIT_EDITOR`, `VISUAL`, `EDITOR` | Editor for commit messages, tried in that order |

## Tuning for your hardware

The defaults suit a small machine. If you are importing or querying a large repository, pick the profile closest to your hardware and paste it, then restart the container. These are starting points, not gospel; measure and adjust.

=== "Small (8-16 GB, 4 cores)"
    ```bash
    pgit config --global container.shared_buffers 2GB
    pgit config --global container.effective_cache_size 6GB
    pgit config --global container.shm_size 3g
    pgit config --global container.work_mem 32MB
    pgit config --global container.max_wal_size 4GB
    pgit config --global container.max_worker_processes 8
    pgit config --global container.max_parallel_workers 4
    pgit config --global container.xpatch_cache_size_mb 1024
    pgit config --global container.xpatch_encode_threads 4
    pgit config --global import.workers 4
    ```

=== "Medium (32-64 GB, 8 cores)"
    ```bash
    pgit config --global container.shared_buffers 16GB
    pgit config --global container.effective_cache_size 48GB
    pgit config --global container.shm_size 20g
    pgit config --global container.work_mem 128MB
    pgit config --global container.max_wal_size 8GB
    pgit config --global container.max_worker_processes 12
    pgit config --global container.max_parallel_workers 8
    pgit config --global container.xpatch_cache_size_mb 8192
    pgit config --global container.xpatch_encode_threads 8
    pgit config --global import.workers 8
    ```

=== "Large (128+ GB, 16+ cores)"
    ```bash
    pgit config --global container.shared_buffers 64GB
    pgit config --global container.effective_cache_size 200GB
    pgit config --global container.shm_size 80g
    pgit config --global container.work_mem 256MB
    pgit config --global container.max_wal_size 32GB
    pgit config --global container.max_worker_processes 20
    pgit config --global container.max_parallel_workers 16
    pgit config --global container.xpatch_cache_size_mb 32768
    pgit config --global container.xpatch_encode_threads 16
    pgit config --global import.workers 16
    ```

Then apply:

```bash
pgit local destroy && pgit local start
```

### Monitoring what you changed

Check whether the cache is doing its job:

```bash
pgit sql "SELECT * FROM xpatch.cache_stats()"   # cache hit rate (aim above 90%)
pgit stats --xpatch                             # per-table compression and cache detail
```

- A high `miss_count` versus `hit_count` means the cache is too small. Raise `xpatch_cache_size_mb`.
- A high `skip_count` means entries are rejected for being too large. Raise `xpatch_cache_max_entry_kb`.

### When something is slow or breaks

- **Container will not start (shared memory error):** raise `shm_size` above `shared_buffers`.
- **Import is slow:** raise `xpatch_encode_threads` and `import.workers` toward your core count, and raise `max_wal_size` / `checkpoint_timeout` to cut checkpoint stalls.
- **Queries are slow:** raise `xpatch_cache_size_mb`, then re-check `cache_stats()`.
- **Out of memory during import:** lower `import.workers`, `xpatch_encode_threads`, and `work_mem`.

### Setting reference

PostgreSQL settings:

| Setting | Default | Rule of thumb |
| ------- | ------- | ------------- |
| `shared_buffers` | 256MB | 25% of RAM (cap around 64GB) |
| `effective_cache_size` | 1GB | 75% of RAM |
| `work_mem` | 16MB | RAM / max_connections / 4 |
| `wal_buffers` | 64MB | 64MB to 512MB by import size |
| `shm_size` | 256m | shared_buffers times 1.25 |
| `max_wal_size` | 4GB | 8GB to 32GB for large imports |
| `checkpoint_timeout` | 30min | 60min for large imports |
| `max_connections` | 100 | 50 for local single-user |
| `max_worker_processes` | 4 | cores plus 4 |
| `max_parallel_workers` | 4 | cores |
| `max_parallel_per_gather` | 2 | cores / 2 |

pg-xpatch settings:

| Setting | Default | Rule of thumb |
| ------- | ------- | ------------- |
| `xpatch_cache_size_mb` | 256 | 10% to 15% of RAM |
| `xpatch_cache_max_entries` | 65536 | cache_size_mb times 64 |
| `xpatch_cache_max_entry_kb` | 256 | 1024 to 16384 to allow large files |
| `xpatch_cache_partitions` | 32 | cores |
| `xpatch_encode_threads` | 0 | cores, for imports |
| `xpatch_insert_cache_slots` | 16 | 16 plus cache_size_mb / 128 |
| `xpatch_warm_cache_workers` | 4 | cores, for cache warming |

Import setting:

| Setting | Default | Rule of thumb |
| ------- | ------- | ------------- |
| `import.workers` | 3 | cores |

For the meaning of each xpatch knob and the engine-side detail, see [pg-xpatch server parameters](/imgajeed/pg-xpatch/server-parameters) and [tuning compression](/imgajeed/pg-xpatch/tuning-compression).

## Where to go next

!!! cards { cols=2 }
    - [Command reference](./commands.md){ icon=terminal }
      Every command and flag.

    - [Database schema reference](./database-schema.md){ icon=table }
      The tables your queries and stats run against.
