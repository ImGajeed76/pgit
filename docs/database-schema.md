---
title: Database schema reference
description: Every table and column in a pgit database, what each holds, and which tables are delta-compressed.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/database-schema
  type: reference
  applies_to:
    - pgit 4
  language: en
  difficulty: intermediate
  time_estimate: 8m
  status: stable
  aliases:
    - pgit tables
    - pgit schema
    - database columns
    - pgit_commits
    - pgit_file_refs
  prev: ./commands.md
---

# Database schema reference

This is the full table and column reference for a pgit database (schema version 5). You can get a live version any time with `pgit sql schema` and `pgit sql schema <table>`. For why the tables are split the way they are, read [How pgit stores a repository](./how-it-works.md).

Three tables use the pg-xpatch access method (delta-compressed); the rest are normal heap tables. The practical difference is covered in [Querying with SQL and search](./querying-with-sql.md): filter and join on heap tables, read xpatch tables by primary key or front-to-back.

!!! note "The schema is versioned"
    pgit stores its schema version in `pgit_metadata`. Upgrading pgit to a version with a newer schema requires a re-import (`pgit import --force`); there is no in-place migration.

## pgit_commits

Commit metadata. Storage: **xpatch** (delta-compressed). One delta chain for the whole history, ordered by `seq`. Delta-compressed columns are the message and the author/committer names and emails; a keyframe is written every 100 commits.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `id` | `TEXT PRIMARY KEY` | ULID identifier (encodes a timestamp) |
| `seq` | `INTEGER NOT NULL` | Insertion order; the xpatch ordering column |
| `parent_id` | `TEXT` | Parent commit id (`NULL` for the root commit) |
| `tree_hash` | `TEXT NOT NULL` | Hash of the file tree state |
| `message` | `TEXT NOT NULL` | Commit message |
| `author_name` | `TEXT NOT NULL` | Author name |
| `author_email` | `TEXT NOT NULL` | Author email |
| `authored_at` | `TIMESTAMPTZ NOT NULL` | Author timestamp (the real git date) |
| `committer_name` | `TEXT NOT NULL` | Committer name |
| `committer_email` | `TEXT NOT NULL` | Committer email |
| `committed_at` | `TIMESTAMPTZ NOT NULL` | Committer timestamp |

!!! tip "Order by seq, not id"
    The ULID `id` encodes a timestamp, but commit timestamps are not always monotonic (rebases, clock skew). Use `ORDER BY seq` for a stable, decode-friendly ordering. The analyses do.

## pgit_commit_graph

The commit DAG, with binary-lifting ancestor pointers for fast `HEAD~N` and ancestry queries. Storage: **heap**. Holds only structure, not content.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `seq` | `SERIAL PRIMARY KEY` | Auto-increment sequence number |
| `id` | `TEXT NOT NULL UNIQUE` | Commit id (references `pgit_commits.id`) |
| `depth` | `INTEGER NOT NULL` | Depth in the DAG (root = 0) |
| `ancestors` | `INTEGER[]` | Binary-lifting ancestor seq numbers (2^0, 2^1, 2^2, ...) |

## pgit_paths

The path registry. Storage: **heap**. Maps each file path to a content group (N paths can share one group; see [delta groups](./how-it-works.md#delta-groups-the-key-idea)).

| Column | Type | Notes |
| ------ | ---- | ----- |
| `path_id` | `INTEGER PRIMARY KEY GENERATED ALWAYS AS IDENTITY` | Unique path id |
| `group_id` | `INTEGER NOT NULL` | Delta compression group (shared across renames and copies) |
| `path` | `TEXT NOT NULL UNIQUE` | File path relative to the repository root |

## pgit_file_refs

Which file version exists in which commit. Storage: **heap**. Primary key `(path_id, commit_id)`. This is the table the fast analyses scan.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `path_id` | `INTEGER NOT NULL` | References `pgit_paths.path_id` (part of PK) |
| `commit_id` | `TEXT NOT NULL` | References `pgit_commits.id` (part of PK) |
| `version_id` | `INTEGER NOT NULL` | Version number within the delta group |
| `content_hash` | `BYTEA` | BLAKE3 hash of the content (`NULL` means the file was deleted) |
| `mode` | `INTEGER NOT NULL DEFAULT 33188` | Unix file mode (default `0100644`) |
| `is_symlink` | `BOOLEAN NOT NULL DEFAULT FALSE` | Whether this entry is a symlink |
| `symlink_target` | `TEXT` | Symlink target, if a symlink |
| `is_binary` | `BOOLEAN NOT NULL DEFAULT FALSE` | Whether the content is binary |

## pgit_text_content

Text file bodies. Storage: **xpatch**. Primary key `(group_id, version_id)`; delta-compressed by group, in version order.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `group_id` | `INTEGER NOT NULL` | Delta group (from `pgit_paths.group_id`, part of PK) |
| `version_id` | `INTEGER NOT NULL` | Version within the group (part of PK) |
| `content` | `TEXT NOT NULL DEFAULT ''` | File content, delta-compressed |

## pgit_binary_content

Binary file bodies. Storage: **xpatch**. Same shape as `pgit_text_content`, with a `BYTEA` body. Files are routed here when their content contains a NUL byte.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `group_id` | `INTEGER NOT NULL` | Delta group (part of PK) |
| `version_id` | `INTEGER NOT NULL` | Version within the group (part of PK) |
| `content` | `BYTEA NOT NULL DEFAULT ''::bytea` | File content, delta-compressed |

## pgit_refs

Named references. Storage: **heap**.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `name` | `TEXT PRIMARY KEY` | Reference name (for example `HEAD`) |
| `commit_id` | `TEXT NOT NULL` | References `pgit_commits.id` |

## pgit_sync_state

Per-remote sync bookmarks. Storage: **heap**.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `remote_name` | `TEXT PRIMARY KEY` | Remote name |
| `last_commit_id` | `TEXT` | Last synchronized commit id |
| `synced_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | Last sync time |

## pgit_metadata

Repository key/value metadata, including the schema version and import state. Storage: **heap**.

| Column | Type | Notes |
| ------ | ---- | ----- |
| `key` | `TEXT PRIMARY KEY` | Metadata key |
| `value` | `TEXT NOT NULL` | Metadata value |

## Where to go next

!!! cards { cols=2 }
    - [Querying with SQL and search](./querying-with-sql.md){ icon=database }
      Turn these tables into answers.

    - [How pgit stores a repository](./how-it-works.md){ icon=layers }
      Why the schema is shaped this way.
