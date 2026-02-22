# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [4.1.1] - 2026-02-22

### Fixed

- Remove artificial 16-worker cap on `pgit import`. The limit was based on the default `xpatch_insert_cache_slots=16`, but that setting is configurable and exceeding it only degrades cache hit rate, not correctness. Workers are now capped at CPU count instead.
- Remove `max:"16"` validation on `import.workers` config, allowing any value above 1.

## [4.1.0] - 2026-02-22

### Changed

- **Import pipeline optimizations** targeting Linux kernel scale (1.3M commits, 24M file versions):
  - **Path grouping 30x faster**: `computePathGroups` now uses git's `original-oid` SHA-1 as content identity instead of re-reading every blob from disk and hashing with BLAKE3 (2.06s → 67ms on git.git)
  - **Streaming blob import**: Workers now read blob content on demand in chunks of 1000 instead of pre-loading entire groups into memory. Peak memory per worker is bounded to O(1000 * avg_blob_size) instead of O(total_group_size). Prevents OOM on files with hundreds of thousands of versions.
  - **Parallel index creation**: All secondary indexes are now built concurrently using errgroup. At scale (24M file_refs rows), the two file_refs indexes dominate — building them in parallel roughly halves the index rebuild phase.
  - **COPY for path registration**: `PreRegisterPaths` uses bulk COPY instead of per-row INSERT, eliminating ~70K round-trips for a typical import.
  - **Flat commit graph slice**: `buildCommitGraph` uses `[]CommitGraphEntry` instead of `[]*CommitGraphEntry`, avoiding 1.3M individual heap allocations at Linux kernel scale.
  - **COPY chunk size 200 → 1000**: Reduces per-COPY overhead by 5x for large groups.
  - **Aggressive memory release**: Commit messages, FileOps slices, and commitEntries are nil'd after each phase completes, freeing ~2-10GB before the memory-intensive blob import begins.
  - **Temp file moved to .pgit/**: Fast-export stream is written to the repository's `.pgit/` directory instead of `/tmp` (tmpfs), preventing "no space left on device" on repos with streams exceeding tmpfs capacity (e.g. Linux kernel at 50-100GB).

## [4.0.0] - 2026-02-22

### Added

- **Commit graph table with binary lifting** (`pgit_commit_graph`): new heap table that stores commit ancestry with pre-computed power-of-2 ancestor pointers, enabling O(log N) `HEAD~N` resolution regardless of depth. `HEAD~23639` on git.git goes from 5.8s to 0.13s.
- N:1 path-to-group mapping: multiple file paths can now share one delta compression group, deduplicating content from renames, copies, and reverts. Reduces content rows by 40% and raw storage by 30% on git.git.
- Content deduplication within groups: `CreateBlobsForGroup` tracks content hashes and reuses existing version IDs when the same content appears at multiple paths in a group.
- `pgit log --limit` alias for `--max-count`
- git/git added to benchmark suite (20 repos, 274k total commits)
- Performance tuning guide (`docs/performance-tuning.md`) with copy-paste profiles for small/medium/large machines

### Changed

- **Schema version 4** (requires re-import with `pgit import --force`):
  - `pgit_paths`: new `path_id` auto-increment PK, `group_id` is now shared (N:1)
  - `pgit_file_refs`: PK changed from `(group_id, commit_id)` to `(path_id, commit_id)`
  - `pgit_commit_graph`: new heap table with `(seq, id, depth, ancestors[])`
  - Content tables: `compress_depth` increased from 5 to 10
- Go module path migrated from `v3` to `v4`
- `resolveCommitRef` uses binary lifting (O(log N) heap lookups) instead of N sequential xpatch PK lookups
- `resolveBaseRef` uses the graph table for exact/partial ID matching instead of xpatch
- `search.go` uses `GetCommitsBatchByRange` (contiguous range scan) instead of `GetCommitsBatch` (ANY scatter-read)
- Connection pool `MaxConns` reduced from 64 to 32 to stay within PostgreSQL's default `max_connections=50`
- Benchmark scorecard improved from 9-9-1 (v3) to 12-8 pgit wins; hugo compression fixed from 2.6x to 5.1x; import durations reduced 5-25x across the suite

### Removed

- `pgit reset` command (v4 is append-only)
- `pgit resolve` command (v4 is append-only)
- Dead `HasConflictMarkers` function from `config/merge.go`

### Fixed

- Connection pool exhaustion (`too many clients already`) during import checkout phase
- Stale references to `pgit resolve` in `status.go` and `pull.go` help text
- `.goreleaser.yml` ldflags referencing v3 instead of v4
- pgit-bench chart Y-axis labels (now shows "Size (MB)" and "Ratio (raw / stored, x)")
- pgit-bench `--json <file>` now shows progress output (only suppresses when JSON goes to stdout)
- pgit-bench `collectPgitStats` now includes `pgit_commit_graph` table and index sizes

## [3.3.2] - 2026-02-20

### Fixed

- Align xpatch GUC defaults and limits to `pg_xpatch.c` definitions: correct `cache_max_entry_kb` default (256, was 4096) and `insert_cache_slots` default (16, was 64); remove artificial upper limits on most cache settings

## [3.3.1] - 2026-02-20

### Added

- Show elapsed time on the progress bar for each import phase

### Fixed

- Drop secondary indexes on `pgit_file_refs` during bulk import and rebuild afterward, fixing progressive slowdown at scale
- Fix `encode_threads=0` being silently overwritten to 4; change default to 0 (sequential encoding)
- Apply session GUCs (`synchronous_commit`, `commit_delay`) to all pool connections, not just the initial pool

## [3.3.0] - 2026-02-20

### Added

- `--timeout` flag for import (default 24h) and configurable `import.workers` in global config

### Changed

- Rewrite blob import for ~5x speedup on large repos (git/git: 20 min → 4 min) with bulk path pre-registration, adaptive batching, and heavy/light path interleaving
- Add container-level WAL tuning (`max_wal_size=4GB`, `full_page_writes=off`)
- Wire up pg-xpatch 0.6.0 GUCs (`encode_threads`, cache settings); reduce `compress_depth` from 50 to 5

### Fixed

- Use per-group transactions with chunked COPY for blob import, fixing xpatch delta chain corruption from cross-connection cache interference

## [3.2.0] - 2026-02-19

### Added

- Three-way merge engine with diff3 algorithm: auto-merges non-overlapping changes, only marks specific conflicting lines
- Rewrite `pgit pull` with proper divergence handling, conflict detection, and transactional commits
- `--resume` flag for `pgit import` to continue interrupted imports; tracks state via `pgit_metadata`
- `--fastexport <file>` flag to skip `git fast-export` by providing a pre-generated stream file
- `-U`/`--unified` flag on `pgit diff` and `pgit show` for configurable context lines
- `--remote` flag on all `pgit analyze` subcommands for remote database queries
- `--sort` and `--reverse` flags on `pgit analyze` subcommands
- `--chart` flag on `pgit analyze activity` with sub-character bar precision
- `pgit log [commit]` to start log from a specific commit (not just HEAD)
- Show matching candidates with short IDs on ambiguous commit references

### Changed

- Sort diff output deterministically by path
- Parallelize file content fetches in `pgit diff` and `pgit show` with `errgroup`
- Group identical matches across versions in `pgit search --all` mode

### Fixed

- **Security:** bind container to `127.0.0.1` instead of `0.0.0.0`; require password auth (scram-sha-256) instead of trust
- Fix `atomic.Value` panic under concurrent load in import and search
- Fix progress bar data race from concurrent worker goroutines
- Fix `pgit stats` using heap-only size instead of heap + TOAST + FSM; add missing tables to total

## [3.1.0] - 2026-02-17

### Added

- `pgit analyze` subcommand with 6 pre-built analyses: `churn`, `coupling`, `hotspots`, `authors`, `activity`, `bus-factor`
- All analyses support `--json`, `--raw`, `--no-pager`, `--limit`, and `--path` glob filter
- `y`/`Y` keybindings in TUI table to copy cell/row to system clipboard

### Changed

- Extract interactive table viewer into reusable `internal/ui/table` package, shared between `pgit sql` and `pgit analyze`

### Fixed

- Fix column types and order in `pgit sql schema` display
- Replace xpatch anti-pattern examples with heap-only queries in `pgit sql examples` and README
- Fix `pgit repos` size calculation to use v3 tables instead of legacy `pgit_blobs`
- Fix stale column references in example SQL queries

## [3.0.0] - 2026-02-16

### Added

- **Schema v3**: split content into `pgit_text_content` (TEXT) and `pgit_binary_content` (BYTEA); add committer fields; rename `created_at` → `authored_at`; add `is_binary` flag to `pgit_file_refs`
- Rewrite import to use `git fast-export` for correct merge handling and rename/delete tracking
- `pgit-bench` CLI for automated compression benchmarking against git
- Compression benchmark results (`BENCHMARK.md`) for 19 repos across 6 languages
- pg-xpatch query patterns guide (`docs/xpatch-query-patterns.md`)
- Configurable `xpatch_cache_size_mb` (default 256 MB) in container config

### Changed

- **BREAKING:** Schema v3 requires re-import; `created_at` → `authored_at`, single `pgit_content` → split `pgit_text_content`/`pgit_binary_content`
- Bump Go module path from `v2` to `v3`
- Eliminate xpatch full-table scans across all read commands (git/git benchmarks): `show` 130x, `diff` 167x, `blame` 8x, `log` 9x, `stats` 4x faster
- Replace `GetHeadCommit()` with O(1) ref lookup across all commands
- Rewrite search with parallel-by-group strategy sorted by chain depth for fast early termination

### Fixed

- Scan full file content for null bytes in binary detection, fixing TEXT import failures for files with embedded `\x00`
- Correct raw size calculation in pgit-bench and fix chart color encoding
- Resolve all golangci-lint errors

## [2.1.0] - 2026-02-03

### Added

- `pgit rm` command: stage file deletions with `--cached`, `-r`, `-f` options
- `pgit mv` command: move/rename files and stage changes
- `pgit clean` command: remove untracked files (`-n` dry-run, `-f` force, `-d` dirs)
- `pgit grep` alias for the search command
- `pgit update` command: check for new releases on GitHub
- `pgit repos delete` command with `--force` and `--search` flags
- `pgit sql schema`, `pgit sql tables`, and `pgit sql examples` subcommands
- `pgit diff --stat` for diffstat summary; commit range syntax (`commit1..commit2`) and `--` path separator
- `pgit log --graph` for ASCII commit graph visualization
- Support `HEAD~N` and `HEAD^` ancestor syntax across all commands
- Reflection-based config system with struct tags, auto-generated help, and validation
- Horizontal scrolling in SQL TUI viewer with ANSI-aware viewport slicing
- Git config fallback: copy `user.name`/`user.email` from git on `pgit init`

### Changed

- Commit output shows insertions/deletions statistics
- Status shows helpful hints for staged/unstaged/untracked files
- Improved error messages for invalid commit references and missing branches
- Update module path to `v2` for Go semantic versioning
- Use metadata-only query for commit tree lookup (~18x faster on large repos)

### Fixed

- `pgit checkout -- <file>` defaults to HEAD when no commit specified
- `pgit reset` validates commit refs before treating args as filenames
- Binary files show "Binary files differ" instead of garbled diff output

## [2.0.0] - 2026-02-01

### Added

- **Schema v2**: three-table architecture (`pgit_paths`, `pgit_file_refs`, `pgit_content`) replacing single `pgit_blobs`
- Parallel content fetching across 64-connection pool; batch commit lookups
- Configurable PostgreSQL tuning via `pgit config --global` (shared_buffers, work_mem, etc.)

### Changed

- **BREAKING:** Schema v2 requires re-import with `pgit import --force`
- Status command 43x faster by using `pgit_file_refs` lookup table instead of decompressing content

## [1.1.0] - 2026-02-01

### Added

- `pgit repos` command: list databases with path, commits, and size; `cleanup` subcommand for orphaned databases; `--search`/`--path`/`--depth` flags
- `pgit local update`: check GHCR for new pg-xpatch versions, pull and recreate container with data preserved
- `pgit local migrate`: migrate anonymous volumes to persistent named volume (`pgit-data`)
- `pgit local destroy` with `--purge` flag; volume info in `pgit local status`
- Interactive SQL TUI with Bubble Tea: search, CSV/JSON export, column controls, keyboard navigation
- `pgit reset` with `--soft`, `--mixed`, `--hard` modes and `HEAD~N` syntax
- Global config at `~/.config/pgit/config.toml`: container settings, import workers
- Rewrite import for parallel processing with per-worker `git cat-file --batch`, `pgx.CopyFrom` batch inserts, and scrollable branch picker
- Auto-checkout working tree after import
- `--timeout` flag for `pgit sql`
- Improved editor detection with multiple fallbacks
- `pgit add -A` without path argument; reject empty commit messages; `pgit checkout -- file` syntax

### Fixed

- Handle non-UTF-8 encodings in git import
- Add ETA display to progress bars
- Group search results by path+commit; show relative timestamps
- Import file deletions from git history; fix gitignore root-relative patterns
- Fix symlink-to-regular-file transitions causing xpatch decode errors
- Use `xpatch.stats()` for O(1) stats queries instead of `COUNT(*)`
- Fix off-by-one in `pgit log --max-count` returning N+1 commits
- Fix `--no-color` flag not applying to all output
- Better error messages for `push`/`pull` without configured remote

## [1.0.0] - 2026-01-28

### Added

- **Initial release** of pgit — a Git-like VCS backed by PostgreSQL with pg-xpatch delta compression
- Core commands: `init`, `add`, `status`, `commit`, `log`, `diff`, `show`, `checkout`, `blame`
- Remote sync: `push`, `pull`, `clone` with conflict detection and `resolve`
- `import` from existing git repositories with branch selection
- `search` for full-text regex search across all file history
- `sql` for arbitrary SQL against the repository database
- `stats` with `--xpatch` flag for compression internals
- `config` for per-repo settings; `remote` for connection management
- `local start|stop|status|logs` for Docker/Podman container management
- `doctor` for diagnosing connection and container issues
- Interactive log TUI with search and keyboard navigation
- JSON output for `status`, `log`, `stats`; `PGIT_ACCESSIBLE` env var
- Docker and Podman runtime auto-detection
- Shell completions (bash, zsh, fish, PowerShell)
- GoReleaser: multi-platform binaries, Homebrew, deb/rpm/apk packages

[Unreleased]: https://github.com/ImGajeed76/pgit/compare/v4.0.0...HEAD
[4.0.0]: https://github.com/ImGajeed76/pgit/compare/v3.3.2...v4.0.0
[3.3.2]: https://github.com/ImGajeed76/pgit/compare/v3.3.1...v3.3.2
[3.3.1]: https://github.com/ImGajeed76/pgit/compare/v3.3.0...v3.3.1
[3.3.0]: https://github.com/ImGajeed76/pgit/compare/v3.2.0...v3.3.0
[3.2.0]: https://github.com/ImGajeed76/pgit/compare/v3.1.0...v3.2.0
[3.1.0]: https://github.com/ImGajeed76/pgit/compare/v3.0.0...v3.1.0
[3.0.0]: https://github.com/ImGajeed76/pgit/compare/v2.1.0...v3.0.0
[2.1.0]: https://github.com/ImGajeed76/pgit/compare/v2.0.0...v2.1.0
[2.0.0]: https://github.com/ImGajeed76/pgit/compare/v1.1.0...v2.0.0
[1.1.0]: https://github.com/ImGajeed76/pgit/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/ImGajeed76/pgit/releases/tag/v1.0.0
