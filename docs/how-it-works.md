---
title: How pgit stores a repository
description: The tables behind pgit, how delta groups collapse renames and copies, and why the analyses are shaped the way they are.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/how-it-works
  type: explanation
  applies_to:
    - pgit 4
  language: en
  difficulty: intermediate
  time_estimate: 9m
  status: stable
  aliases:
    - pgit storage model
    - pgit schema explained
    - delta groups
    - how pgit works
  prev: ./quickstart.md
  next: ./compression.md
---

# How pgit stores a repository

When you import a repo, pgit unpacks git's object graph into ordinary PostgreSQL tables. There is no magic blob format to learn: a commit is a row, a file version is a row, and the heavy content is delta-compressed by [pg-xpatch](/imgajeed/pg-xpatch/overview) underneath. Understanding the shape of those tables explains why some queries are instant and others stream the whole history.

## Two kinds of table

pgit's schema splits into two camps:

- **Heap tables** are normal PostgreSQL tables. They hold structure and metadata: which paths exist, which file version belongs to which commit, the commit DAG. They are small and fast to scan or index.
- **xpatch tables** use the `USING xpatch` access method. They hold the bulky, repetitive content (commit messages, file bodies) and store each version as a delta against the previous one.

The split is deliberate. Anything you filter, join, or count lives in a heap table so it stays cheap. Anything that is large and near-identical from version to version lives in an xpatch table so it compresses.

| Table | Storage | Holds |
| ----- | ------- | ----- |
| `pgit_commits` | xpatch | Commit metadata: message, author, committer, timestamps, parent |
| `pgit_commit_graph` | heap | The commit DAG, with binary-lifting ancestor pointers |
| `pgit_paths` | heap | Every path, mapped to a content group |
| `pgit_file_refs` | heap | Which file version exists in which commit |
| `pgit_text_content` | xpatch | Text file bodies, delta-compressed |
| `pgit_binary_content` | xpatch | Binary file bodies, delta-compressed |
| `pgit_refs` | heap | Named refs (HEAD, branches) |
| `pgit_sync_state` | heap | Per-remote sync bookmarks |
| `pgit_metadata` | heap | Key/value repo metadata (schema version, import state) |

The [database schema reference](./database-schema.md) lists every column. This page is about why they fit together the way they do.

## Delta groups: the key idea

A naive design would give every path its own delta chain. pgit does something smarter. During import it runs a **union-find** pass over content hashes: any two paths that ever held byte-identical content get merged into the same **group**. A rename, a copy, a file split, a revert that restores an old path: all of these end up sharing one delta chain.

That is what the `group_id` on `pgit_paths` means. It is an N:1 mapping: many paths can point at one group. The content tables (`pgit_text_content`, `pgit_binary_content`) are keyed by `(group_id, version_id)`, so the actual bytes are stored once per group, in version order, as a delta chain.

!!! info "Why this matters"
    Grouping by shared content instead of by path eliminates a large slice of duplicate storage. When a file moves from `drivers/char/drm/` to `drivers/gpu/drm/`, git and a path-keyed store would begin a fresh history; pgit keeps the two paths in one chain and deltas straight across the rename.

Content is hashed with BLAKE3 (truncated to 16 bytes). The empty file is excluded from grouping, since "both files are empty" is not a meaningful similarity. Each file version is also classified as text or binary (pgit scans for a NUL byte) and routed to the matching content table.

## How a version is reconstructed

Within a group, the first version is stored in full as a **keyframe**; later versions are stored as deltas against their predecessor. To read version N, xpatch walks back to the nearest keyframe and replays the deltas forward. pgit configures a keyframe every 100 versions for both content and commits, which bounds that walk.

`version_id` is the position of a content version within its group. `content_hash` on `pgit_file_refs` is the BLAKE3 hash of that version, or `NULL` when the file was deleted in that commit.

## Commits are one long chain, ordered by seq

All commit metadata lives in a single xpatch group (`pgit_commits`), delta-compressed across the whole history because successive commits share a lot (author names, email domains, message boilerplate). The `delta_columns` are the message and the author/committer name and email.

The chain is ordered by `seq`, a plain integer assigned at import in insertion order. This is load-bearing: author timestamps are not monotonic (rebases, cherry-picks, a developer's clock set to 2085), and ordering a delta chain by a non-monotonic key produces a tangled chain that is slow to decode. `seq` gives a clean, strictly increasing order.

!!! check "Why the analyses scan by seq"
    The commit-based analyses (`authors`, `activity`, `bus-factor`) read `pgit_commits` with `ORDER BY seq ASC`. Front-to-back is the optimal access pattern for a delta chain: each row reuses the previous row's decompression. Jumping around the chain at random would re-walk deltas every time.

Ancestry (`HEAD~50`, "is X an ancestor of Y") does not need the heavy commit table at all. `pgit_commit_graph` is a small heap table holding just the DAG structure plus binary-lifting ancestor pointers, so walking back N commits is a handful of index lookups, not a chain decode.

## Why some analyses are instant and others stream

This shapes performance directly:

- **`churn`, `coupling`, `hotspots`** touch only `pgit_file_refs` and `pgit_paths`, both heap tables. They stay fast even on millions of file versions, because no content is decompressed.
- **`authors`, `activity`, `bus-factor`** must read commit metadata, so they stream `pgit_commits` front-to-back once. That is slower, but linear, and it is the cheapest correct way to read a delta chain.

When you write your own SQL, the same rule applies: filtering and joining on heap tables is cheap, and full scans of the xpatch content tables are the expensive operation to plan around. [Querying with SQL](./querying-with-sql.md) gives concrete patterns.

## Where to go next

!!! cards { cols=2 }
    - [Compression vs git](./compression.md){ icon=minimize-2 }
      What the delta chains actually buy you, with honest numbers.

    - [Database schema reference](./database-schema.md){ icon=table }
      Every table and column, for writing queries.

    - [How pg-xpatch storage works](/imgajeed/pg-xpatch/storage-model){ icon=layers }
      Keyframes and deltas in depth, from the engine's own docs.
