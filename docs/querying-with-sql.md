---
title: Querying with SQL and search
description: Run custom SQL against your repository, search every version of every file, and write queries that stay fast on delta-compressed tables.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/querying-with-sql
  type: how-to
  applies_to:
    - pgit 4
  language: en
  difficulty: intermediate
  time_estimate: 12m
  status: stable
  aliases:
    - pgit sql
    - custom queries
    - pgit search
    - xpatch query patterns
    - query performance
  prev: ./analyzing-history.md
  next: ./remotes.md
---

# Querying with SQL and search

The [built-in analyses](./analyzing-history.md) cover the common questions. When you have an uncommon one, the whole repository is sitting in plain PostgreSQL tables, and `pgit sql` hands you a connection. This guide covers running queries, searching across history, and writing SQL that stays fast on the delta-compressed tables.

## Running a query

```bash
pgit sql "SELECT id, author_name, message FROM pgit_commits ORDER BY seq DESC LIMIT 10"
```

Results open in the same interactive viewer the analyses use. Add `--json` for a JSON array, `--raw` for tab-separated output, or `--no-pager` for a plain table. `--timeout` sets the query timeout in seconds (default 60), and `--remote` runs against a configured remote.

### Reads are safe; writes are gated

By default `pgit sql` allows read-only queries only. Anything that starts with `INSERT`, `UPDATE`, `DELETE`, `DROP`, `CREATE`, `ALTER`, or `TRUNCATE` is refused unless you add `--write`.

!!! danger "Writing bypasses pgit's safety checks"
    `pgit sql --write` lets you modify tables directly, which can corrupt a repository: the storage is append-only and the delta chains assume immutable history. Use it only when you know exactly what you are doing. For exploration, leave it off and you cannot break anything.

## Discovering the schema

pgit documents its own tables, so you do not have to keep this page open:

```bash
pgit sql tables                # list every pgit table
pgit sql schema                # all tables with a one-line purpose
pgit sql schema pgit_commits   # one table's columns and types
pgit sql examples              # ready-to-run example queries
```

The [database schema reference](./database-schema.md) has the full column list if you prefer to read it here.

## Example queries

A few starting points (these come from `pgit sql examples`):

```sql
-- Most recent commits
SELECT id, author_name, message, authored_at
FROM pgit_commits
ORDER BY seq DESC
LIMIT 10;
```

```sql
-- Files with the most versions
SELECT p.path, COUNT(*) AS versions
FROM pgit_file_refs r
JOIN pgit_paths p ON p.path_id = r.path_id
GROUP BY p.path
ORDER BY versions DESC
LIMIT 10;
```

```sql
-- Deleted files (content_hash is NULL when a file was removed)
SELECT DISTINCT p.path
FROM pgit_file_refs r
JOIN pgit_paths p ON p.path_id = r.path_id
WHERE r.content_hash IS NULL
ORDER BY p.path;
```

Notice both file queries join the two **heap** tables (`pgit_file_refs`, `pgit_paths`) and never touch a compressed content table. That is the single most important habit for fast queries, and the next section explains why.

## Writing queries that stay fast

pgit's tables come in two flavours, and they have very different performance characteristics:

- **Heap tables** (`pgit_paths`, `pgit_file_refs`, `pgit_commit_graph`, `pgit_refs`, `pgit_metadata`, `pgit_sync_state`) are normal PostgreSQL tables. No decompression cost. Filter, join, and aggregate on these freely.
- **xpatch tables** (`pgit_commits`, `pgit_text_content`, `pgit_binary_content`) store delta chains. Reading a row may decompress part of a chain. Every rule below is about minimizing how much of a chain you touch.

!!! tip "The one-sentence version"
    Get everything you can from the heap tables first; only read an xpatch table when you need actual content or a commit message, and when you do, read it by primary key or front-to-back.

### Rules for the content tables

The content tables are keyed by `(group_id, version_id)`. Resolve the `group_id` for a path from `pgit_paths.group_id`, then:

- **Always constrain `group_id`.** Without it, the planner may scan every group and decompress all of them.
- **Primary-key lookups are cheap.** `WHERE group_id = $1 AND version_id = $2` reconstructs exactly one row.
- **Front-to-back is fastest.** `WHERE group_id = $1 ORDER BY version_id ASC LIMIT $2` reads the chain in natural order, each row reusing the previous decompression. Use `LIMIT` so you do not pull the whole group.
- **Avoid range and set filters on `version_id`.** `version_id > $2` or `version_id = ANY($2)` push the planner into a bitmap scan that decompresses the whole group, then filters.
- **Avoid `OFFSET`.** It decompresses and discards rows before returning. Fetch a larger `LIMIT` and skip in your application instead.

### Rules for the commits table

`pgit_commits` is one big delta chain ordered by `seq` (not by timestamp, and not by `version_id`). So:

- **Scan front-to-back with `ORDER BY seq ASC`** when you need to read many commits. This is exactly what `analyze authors` does.
- **Look up a single commit by its primary key** (`WHERE id = $1`); that is cheap.
- **Do not `JOIN` onto `pgit_commits`.** A join can drive a sequential scan of the whole chain. Instead do a two-step lookup: read the id you want from a heap table, then fetch that one commit by id.

```sql
-- Avoid: the join can scan and decompress all of pgit_commits
SELECT c.* FROM pgit_commits c
JOIN pgit_refs r ON r.commit_id = c.id
WHERE r.name = 'HEAD';

-- Prefer: two cheap steps
SELECT commit_id FROM pgit_refs WHERE name = 'HEAD';   -- heap table
SELECT * FROM pgit_commits WHERE id = $1;              -- PK lookup
```

### Counts and sizes without a full scan

`COUNT(*)`, `MIN()`, and `MAX()` over an xpatch table decompress every row. For counts and storage figures, ask xpatch directly:

```sql
SELECT * FROM xpatch.stats('pgit_commits');
```

For a `MIN`/`MAX` over commit ids or versions, use the heap table `pgit_file_refs` instead, which carries those columns uncompressed.

### Cheat sheet

| Pattern | Speed |
| ------- | ----- |
| PK lookup `(group_id, version_id)` or `id` | Fast |
| `WHERE group_id ... ORDER BY version_id ASC LIMIT n` | Fastest |
| `ORDER BY ... DESC LIMIT n` | Fast once the cache is warm, slow cold |
| `version_id > x` or `version_id = ANY(...)` | Slow (bitmap scan) |
| `JOIN` onto an xpatch table | Risky (may seq-scan) |
| `COUNT(*)` / `MIN` / `MAX` on xpatch | Very slow (full decompress) |
| No `group_id` in `WHERE` | Very slow (scans all groups) |

For the deeper version of all this, including the shared read cache, see [pg-xpatch read performance](/imgajeed/pg-xpatch/tuning-read-performance) and [caching and performance](/imgajeed/pg-xpatch/caching-and-performance).

## Searching across history

`pgit search` runs a regular expression against file content stored in the database, which means it can search history, not just your working tree.

```bash
pgit search "TODO"                   # search the latest version of each file (at HEAD)
pgit search "func.*Error"            # regex (RE2 syntax)
pgit search -i "fixme"               # case-insensitive
pgit search --path "*.go" "panic\("  # restrict to a path glob (escape regex metacharacters like the paren)
```

By default search looks at HEAD. To go through history:

- `--all` searches every version of every file. Identical matches across versions are grouped into one result unless you add `--no-group`.
- `--commit <ref>` searches at one specific commit.

Other flags: `--limit` (`-n`, default 50) caps results, and `--remote` searches a remote database. `pgit grep` is an alias for `pgit search`.

!!! note "Searching all of history is a real scan"
    `--all` reconstructs old file versions, so on a huge repo it does real work (tens of seconds on a multi-million-commit history). On the latest checkout it is quick.

## Where to go next

!!! cards { cols=2 }
    - [Database schema reference](./database-schema.md){ icon=table }
      Every table and column you can query.

    - [Configuration and tuning](./configuration-and-tuning.md){ icon=sliders }
      Cache and parallelism settings that affect query speed.
