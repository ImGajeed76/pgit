# Performance Tuning

pgit runs PostgreSQL in a container. Default settings are conservative — tuning for your hardware makes imports faster and queries more responsive.

## Current Settings

```bash
pgit config --global --list
```

## Profiles

Pick the profile closest to your machine and paste the commands. Restart the container afterward. See the [setting reference](#setting-reference) for what each value does.

### Small (8-16 GB RAM, 4 cores)

```bash
pgit config --global container.shared_buffers 2GB
pgit config --global container.effective_cache_size 6GB
pgit config --global container.shm_size 3g
pgit config --global container.work_mem 32MB
pgit config --global container.wal_buffers 64MB
pgit config --global container.max_wal_size 4GB
pgit config --global container.checkpoint_timeout 30min
pgit config --global container.max_connections 50
pgit config --global container.max_worker_processes 8
pgit config --global container.max_parallel_workers 4
pgit config --global container.max_parallel_per_gather 2
pgit config --global container.xpatch_cache_size_mb 1024
pgit config --global container.xpatch_cache_max_entries 65536
pgit config --global container.xpatch_cache_max_entry_kb 1024
pgit config --global container.xpatch_encode_threads 4
pgit config --global import.workers 4
```

### Medium (32-64 GB RAM, 8 cores)

```bash
pgit config --global container.shared_buffers 16GB
pgit config --global container.effective_cache_size 48GB
pgit config --global container.shm_size 20g
pgit config --global container.work_mem 128MB
pgit config --global container.wal_buffers 256MB
pgit config --global container.max_wal_size 8GB
pgit config --global container.checkpoint_timeout 60min
pgit config --global container.max_connections 50
pgit config --global container.max_worker_processes 12
pgit config --global container.max_parallel_workers 8
pgit config --global container.max_parallel_per_gather 4
pgit config --global container.xpatch_cache_size_mb 8192
pgit config --global container.xpatch_cache_max_entries 524288
pgit config --global container.xpatch_cache_max_entry_kb 4096
pgit config --global container.xpatch_cache_partitions 32
pgit config --global container.xpatch_encode_threads 8
pgit config --global container.xpatch_group_cache_size_mb 128
pgit config --global container.xpatch_tid_cache_size_mb 256
pgit config --global container.xpatch_seq_tid_cache_size_mb 256
pgit config --global container.xpatch_insert_cache_slots 64
pgit config --global import.workers 8
```

### Large (128+ GB RAM, 16+ cores)

```bash
pgit config --global container.shared_buffers 64GB
pgit config --global container.effective_cache_size 200GB
pgit config --global container.shm_size 80g
pgit config --global container.work_mem 256MB
pgit config --global container.wal_buffers 512MB
pgit config --global container.max_wal_size 32GB
pgit config --global container.checkpoint_timeout 60min
pgit config --global container.max_connections 50
pgit config --global container.max_worker_processes 20
pgit config --global container.max_parallel_workers 16
pgit config --global container.max_parallel_per_gather 8
pgit config --global container.xpatch_cache_size_mb 32768
pgit config --global container.xpatch_cache_max_entries 2097152
pgit config --global container.xpatch_cache_max_entry_kb 16384
pgit config --global container.xpatch_cache_partitions 64
pgit config --global container.xpatch_encode_threads 16
pgit config --global container.xpatch_group_cache_size_mb 512
pgit config --global container.xpatch_tid_cache_size_mb 1024
pgit config --global container.xpatch_seq_tid_cache_size_mb 1024
pgit config --global container.xpatch_insert_cache_slots 256
pgit config --global import.workers 16
```

## Applying Changes

Settings take effect after a container restart:

```bash
pgit local destroy && pgit local start
```

> `pgit local destroy` removes the container but **preserves your data**. Use `--purge` only to delete everything.

## Monitoring

```bash
# Cache hit rate (should be >90%)
pgit sql "SELECT * FROM xpatch.cache_stats()"

# Compression statistics
pgit stats --xpatch
```

Key cache metrics:
- **hit_count / miss_count** — below 90% hit rate means the cache is too small
- **skip_count** — entries rejected for being too large, increase `xpatch_cache_max_entry_kb`
- **evict_count** — growing rapidly during queries means increase `xpatch_cache_size_mb`

## Troubleshooting

**Container won't start** (shared memory error): increase `shm_size` above `shared_buffers`.

**Import is slow**: increase `xpatch_encode_threads` and `import.workers` to match your core count. Increase `max_wal_size` and `checkpoint_timeout` to reduce checkpoint stalls.

**Queries are slow**: increase `xpatch_cache_size_mb`. Check `xpatch.cache_stats()` for high miss rate.

**Out of memory during import**: reduce `import.workers`, `xpatch_encode_threads`, and `work_mem`.

## Setting Reference

### PostgreSQL settings

| Setting | Default | Rule of thumb |
|---------|---------|---------------|
| `shared_buffers` | 256MB | 25% of RAM (cap at 64GB) |
| `effective_cache_size` | 1GB | 75% of RAM |
| `work_mem` | 16MB | RAM / max_connections / 4 |
| `wal_buffers` | 64MB | 64-512MB depending on import size |
| `shm_size` | 256m | shared_buffers * 1.25 |
| `max_wal_size` | 4GB | 8-32GB for large imports |
| `checkpoint_timeout` | 30min | 60min for large imports |
| `max_connections` | 100 | 50 for local single-user |
| `max_worker_processes` | 4 | cores + 4 |
| `max_parallel_workers` | 4 | cores |
| `max_parallel_per_gather` | 2 | cores / 2 |

### pg-xpatch settings

| Setting | Default | Rule of thumb |
|---------|---------|---------------|
| `xpatch_cache_size_mb` | 256 | 10-15% of RAM |
| `xpatch_cache_max_entries` | 65536 | cache_size_mb * 64 |
| `xpatch_cache_max_entry_kb` | 256 | 1024-16384 (allow large files) |
| `xpatch_cache_partitions` | 32 | cores |
| `xpatch_encode_threads` | 0 | cores (for imports) |
| `xpatch_insert_cache_slots` | 16 | 16 + cache_size_mb / 128 |
| `xpatch_group_cache_size_mb` | 16 | cache_size_mb / 64 |
| `xpatch_tid_cache_size_mb` | 16 | cache_size_mb / 32 |
| `xpatch_seq_tid_cache_size_mb` | 16 | cache_size_mb / 32 |

### Import settings

| Setting | Default | Rule of thumb |
|---------|---------|---------------|
| `import.workers` | 3 | cores (max 16) |
