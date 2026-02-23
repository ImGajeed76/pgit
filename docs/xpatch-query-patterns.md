# pg-xpatch Query Patterns

> **Tip:** If you just want to run common analyses (churn, coupling, authors,
> etc.), use `pgit analyze` — it wraps optimized queries so you don't need
> to think about any of this. This doc is for writing your own custom SQL.

Guide for writing performant SQL queries against pg-xpatch tables in pgit.

## How xpatch stores data

xpatch compresses tables using **delta chains** within groups. Each group has
periodic **keyframes** (full snapshots) with deltas in between. To reconstruct
any row, xpatch walks backward from that row to the nearest keyframe, applying
deltas forward.

Reconstructed results are stored in a **shared LRU cache**. Cache hits are
sub-millisecond. The cache is shared across all connections.

## pgit's tables

### xpatch tables (delta-compressed)

| Table | Grouping | Description |
|-------|----------|-------------|
| `pgit_commits` | Single group | All commits in one delta chain |
| `pgit_text_content` | One group per content identity (paths sharing identical content share a group) | Text file content versions |
| `pgit_binary_content` | One group per content identity | Binary file content versions |

Querying these tables has decompression cost. Every rule below exists to
minimize how much of the delta chain gets decompressed.

### Normal (heap) tables

| Table | Description |
|-------|-------------|
| `pgit_file_refs` | File version metadata. PK: `(path_id, commit_id)`. Also: version_id, content_hash, mode, is_symlink, symlink_target, is_binary |
| `pgit_paths` | path_id (PK) + group_id (shared compression group) -> file path |
| `pgit_refs` | Ref name -> commit_id (e.g., HEAD) |
| `pgit_metadata` | Repository key-value store |
| `pgit_sync_state` | Remote sync tracking (remote_name, last_commit_id, synced_at) |

Normal tables have no decompression cost. **Always prefer querying these first**
and only touch xpatch tables when you need actual content or commit messages.

## Rules

### 1. Always provide `group_id` in WHERE

Without `group_id`, the planner may scan the entire table, decompressing every
group's delta chain.

```sql
-- Good: scoped to one group
SELECT content FROM pgit_text_content
WHERE group_id = $1 AND version_id = $2

-- Bad: scans all groups
SELECT content FROM pgit_text_content
WHERE version_id = $2
```

### 2. Never JOIN onto xpatch tables

The planner may choose a sequential scan or bitmap scan on the xpatch side of
the JOIN, decompressing everything.

```sql
-- Bad: JOIN drives scan of pgit_commits
SELECT c.* FROM pgit_commits c
JOIN pgit_refs r ON r.commit_id = c.id
WHERE r.name = 'HEAD'

-- Good: two-step lookup
-- Step 1: read from normal table
SELECT commit_id FROM pgit_refs WHERE name = 'HEAD'
-- Step 2: PK lookup on xpatch table
SELECT * FROM pgit_commits WHERE id = $1
```

### 3. PK lookups are fine

`WHERE group_id = $1 AND version_id = $2` uses an Index Scan and reconstructs
only that single row. Cost depends on distance from the nearest keyframe.

```sql
-- Content tables: PK is (group_id, version_id)
SELECT content FROM pgit_text_content
WHERE group_id = $1 AND version_id = $2

-- Commits table: PK is just id
SELECT * FROM pgit_commits WHERE id = $1
```

### 4. Front-to-back sequential access is fastest

`ORDER BY version_id ASC` with the PK index gives an **Index Scan** that
decompresses the delta chain in its natural order. Each row reuses the
previous row's cached result. This is the optimal access pattern.

```sql
-- Optimal: front-to-back Index Scan
SELECT version_id, content FROM pgit_text_content
WHERE group_id = $1
ORDER BY version_id ASC
LIMIT $2
```

Use `LIMIT` to fetch only what you need. Without it, you get the entire group.

### 5. Back-to-front works with warm cache

`ORDER BY version_id DESC` uses an **Index Scan Backward**. On cold cache this
is slow because xpatch must decompress from the front to reach the back. After
a prior access warms the cache, it's fast.

```sql
-- Fast with warm cache, slow cold
SELECT version_id, content FROM pgit_text_content
WHERE group_id = $1
ORDER BY version_id DESC
LIMIT $2
```

If you need newest-first on cold cache, consider fetching front-to-back and
reversing in application code.

### 6. Avoid version_id range filters

Adding `AND version_id > $2` or `AND version_id < $2` causes the planner to
switch from an efficient Index Scan to a **Bitmap Heap Scan**, which
decompresses the entire group and then filters.

```sql
-- Bad: Bitmap Heap Scan, decompresses everything
SELECT version_id, content FROM pgit_text_content
WHERE group_id = $1 AND version_id > $2
ORDER BY version_id ASC LIMIT $3

-- Good: no version_id filter, just LIMIT
SELECT version_id, content FROM pgit_text_content
WHERE group_id = $1
ORDER BY version_id ASC
LIMIT $2
```

### 7. Avoid ANY() on xpatch tables

`WHERE version_id = ANY($2)` also triggers a Bitmap Heap Scan.

```sql
-- Bad: Bitmap Heap Scan
SELECT content FROM pgit_text_content
WHERE group_id = $1 AND version_id = ANY($2)

-- Good: individual PK lookups
SELECT content FROM pgit_text_content
WHERE group_id = $1 AND version_id = $2
```

Exception: `ANY()` is fast with warm cache since the bitmap scan finds
everything already decompressed.

### 8. Never use COUNT(*), MIN(), MAX() on xpatch tables

These aggregates require scanning every row, decompressing the entire table.

```sql
-- Bad: full table decompress
SELECT COUNT(*) FROM pgit_commits

-- Good: use xpatch.stats() for counts
SELECT * FROM xpatch.stats('pgit_commits')

-- Good: use normal tables for MIN/MAX
SELECT MIN(commit_id), MAX(commit_id) FROM pgit_file_refs
```

### 9. Avoid OFFSET

`OFFSET N` forces decompression and discard of N rows before returning results.

```sql
-- Bad: decompresses and discards N rows
SELECT ... ORDER BY version_id ASC LIMIT $1 OFFSET $2

-- Workaround: fetch a larger LIMIT and skip in application code
```

### 10. Prefer metadata from normal tables

For file paths, commit IDs, version IDs, or content hashes, query
`pgit_file_refs` and `pgit_paths` first. Only touch xpatch tables when you
need actual file content or commit messages.

```sql
-- Good: metadata from normal tables, zero xpatch cost
SELECT p.path, r.commit_id, r.version_id, r.content_hash
FROM pgit_file_refs r
JOIN pgit_paths p ON p.path_id = r.path_id
WHERE r.commit_id > $1 AND r.commit_id <= $2

-- Then fetch content only for the specific rows you need
-- (resolve group_id from pgit_paths.group_id via path_id)
SELECT content FROM pgit_text_content
WHERE group_id = $1 AND version_id = $2
```

## Cache configuration

| GUC | Default | Description |
|-----|---------|-------------|
| `pg_xpatch.cache_size_mb` | 256 | Total shared cache size (requires restart) |
| `pg_xpatch.cache_max_entries` | 65536 | Max entries in cache |
| `pg_xpatch.cache_max_entry_kb` | 256 | Max size per cached entry |
| `pg_xpatch.cache_partitions` | 32 | Cache lock partitions (concurrency) |
| `pg_xpatch.encode_threads` | 0 | Parallel delta encoding threads (0 = disabled) |
| `pg_xpatch.insert_cache_slots` | 16 | FIFO slots for insert path cache |
| `pg_xpatch.group_cache_size_mb` | 16 | Group max-seq cache |
| `pg_xpatch.tid_cache_size_mb` | 16 | TID seq cache |
| `pg_xpatch.seq_tid_cache_size_mb` | 16 | Seq-to-TID cache |

These are configured via `pgit config --global container.xpatch_*` and applied
at container startup. Changes require `pgit local destroy && pgit local start`.

Monitor cache health:

```sql
SELECT * FROM xpatch.cache_stats();
```

- High `skip_count` = entries rejected by size limit. Increase `cache_max_entry_kb`.
- High `miss_count` relative to `hit_count` = cache too small or random access
  patterns. Increase `cache_size_mb`.

## Cheat sheet

| Pattern | Speed | Notes |
|---------|-------|-------|
| PK lookup `(group_id, version_id)` | Fast | Single Index Scan |
| `WHERE group_id ORDER BY ASC LIMIT N` | Fastest | Sequential front-to-back |
| `WHERE group_id ORDER BY DESC LIMIT N` | Fast warm, slow cold | Needs prior cache warming |
| `WHERE group_id AND version_id > X` | Slow | Bitmap Heap Scan |
| `WHERE group_id AND version_id = ANY()` | Slow cold, fast warm | Bitmap Heap Scan |
| `JOIN` onto xpatch table | Dangerous | Planner may seq scan |
| `COUNT(*) / MIN() / MAX()` | Very slow | Full table decompress |
| No `group_id` in WHERE | Very slow | Scans all groups |
