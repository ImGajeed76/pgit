# pgit

A Git-like version control CLI backed by PostgreSQL with [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression.

> **Note:** pgit is primarily a demo for [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression. It's not intended to replace git—but it *is* genuinely useful for importing a repo and running SQL analytics on your commit history.

## Why pgit?

**Import any git repo. Analyze it instantly.**

```bash
pgit init
pgit import /path/to/your/repo --branch main
pgit analyze coupling
```

No scripts, no parsing `git log` output. Just answers.

```
file_a                    file_b                    commits_together
────────────────────────  ────────────────────────  ────────────────
src/parser.rs             src/lexer.rs              127
src/db/schema.go          src/db/migrations.go      84
README.md                 CHANGELOG.md              63
```

Need something custom? Everything is in PostgreSQL — write your own SQL:

```sql
-- Same analysis as above, as raw SQL
SELECT pa.path, pb.path, COUNT(*) as times_together
FROM pgit_file_refs a
JOIN pgit_paths pa ON pa.path_id = a.path_id
JOIN pgit_file_refs b ON a.commit_id = b.commit_id AND a.path_id < b.path_id
JOIN pgit_paths pb ON pb.path_id = b.path_id
GROUP BY pa.path, pb.path
ORDER BY times_together DESC;
```

## Features

- **Git-familiar commands**: init, add, commit, log, diff, checkout, push, pull, clone
- **Pre-built analyses**: churn, coupling, hotspots, authors, activity, bus-factor — one command each
- **SQL queryable**: Run arbitrary queries on your entire repo history
- **Delta compression**: pg-xpatch achieves competitive compression with git's packfiles ([benchmark results](BENCHMARK.md))
- **Search across history**: `pgit search "pattern"` searches all versions of all files
- **PostgreSQL as remote**: Connection URL is your "remote" - no separate auth system
- **Local development**: Uses Docker/Podman container for local database
- **Import from Git**: Migrate existing repositories with full history

## Quick Start

```bash
# Start the local database
pgit local start

# Initialize a new repository
pgit init
pgit config user.name "Your Name"
pgit config user.email "you@example.com"

# Basic workflow
pgit add .
pgit commit -m "Initial commit"
pgit log

# Or import an existing git repo
pgit init
pgit import /path/to/git/repo --branch main
```

## Analyze Your Repository

pgit includes pre-built analyses that are optimized for the underlying storage engine. No SQL knowledge needed:

```bash
pgit analyze churn                   # most frequently modified files
pgit analyze coupling                # files always changed together
pgit analyze hotspots --depth 2      # churn aggregated by directory
pgit analyze authors                 # commits per contributor
pgit analyze activity --period month # commit velocity over time
pgit analyze bus-factor              # files with fewest authors (knowledge silos)
```

All commands support `--json`, `--raw` (for piping), `--limit`, and `--path` (glob filter). Results are displayed in an interactive table with search, column expand/hide, and clipboard copy (`y`/`Y`).

## Query Your Repository

For custom queries, pgit stores everything in PostgreSQL so you can query it directly:

```bash
# Built-in search across all history
pgit search "TODO" --path "*.rs"
pgit search --all "panic!" --ignore-case

# Raw SQL access
pgit sql "SELECT * FROM pgit_commits ORDER BY authored_at DESC LIMIT 10"
```

### Example Queries

```sql
-- Most frequently changed files
SELECT p.path, COUNT(*) as versions
FROM pgit_file_refs r
JOIN pgit_paths p ON p.path_id = r.path_id
GROUP BY p.path
ORDER BY versions DESC
LIMIT 10;

-- Files by extension
SELECT
  COALESCE(NULLIF(SUBSTRING(path FROM '\.([^.]+)$'), ''), '(no ext)') as extension,
  COUNT(*) as file_count
FROM pgit_paths
GROUP BY extension
ORDER BY file_count DESC
LIMIT 15;
```

See `pgit sql examples` for more, or check [docs/xpatch-query-patterns.md](docs/xpatch-query-patterns.md) for query optimization tips.

## Installation

### Using Go (recommended)

```bash
go install github.com/imgajeed76/pgit/v4/cmd/pgit@latest
```

### From GitHub Releases

Download pre-built binaries from [Releases](https://github.com/imgajeed76/pgit/releases):

- **Linux**: `pgit_*_linux_amd64.tar.gz` or `pgit_*_linux_arm64.tar.gz`
- **macOS**: `pgit_*_darwin_amd64.tar.gz` or `pgit_*_darwin_arm64.tar.gz`
- **Windows**: `pgit_*_windows_amd64.zip`

### Package Managers

```bash
# Debian/Ubuntu
sudo dpkg -i pgit_*_linux_amd64.deb

# RHEL/Fedora
sudo rpm -i pgit_*_linux_amd64.rpm

# Alpine
sudo apk add --allow-untrusted pgit_*_linux_amd64.apk
```

## Requirements

- **Docker** or **Podman** - Required for the local database container
- **PostgreSQL with pg-xpatch** - Optional, only needed for remote operations (push/pull/clone)

<details>
<summary><strong>Why Docker instead of embedded PostgreSQL?</strong></summary>

We explored using [embedded-postgres-go](https://github.com/fergusstrange/embedded-postgres-go) to bundle PostgreSQL directly into the binary (more git-like, no container dependency). However, pg-xpatch requires custom PostgreSQL extensions, and cross-compiling these for all platforms (especially Windows and macOS amd64) proved impractical.

Since pgit is primarily a demo for pg-xpatch compression, Docker/Podman provides a reliable cross-platform solution without the build complexity.

</details>

## Compression: pgit vs git

Benchmarked on [20 real repositories](BENCHMARK.md) across 6 languages (274k total commits). Comparing `git gc --aggressive` packfile vs pgit actual data (excluding indexes for both):

**Scorecard: pgit 13 wins, git 7 wins** out of 20 repositories.

| Repository | Commits | Raw Size | git --aggressive | pgit | Winner |
|:-----------|--------:|---------:|-----------------:|-----:|:-------|
| fzf | 3,499 | 213.3 MB | 3.5 MB | 3.0 MB | pgit (16%) |
| core | 6,930 | 598.9 MB | 11.6 MB | 11.2 MB | pgit (4%) |
| curl | 37,860 | 3.3 GB | 48.4 MB | 49.3 MB | git (2%) |
| git | 79,765 | 7.3 GB | 90.6 MB | 111.3 MB | git (23%) |
| prettier | 11,084 | 2.0 GB | 66.1 MB | 91.1 MB | git (38%) |
| hugo | 9,538 | 570.6 MB | 108.8 MB | 111.0 MB | git (2%) |

See the [full benchmark results](BENCHMARK.md) for all 20 repositories with detailed per-repo breakdowns, charts, and methodology.

pgit uses [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression with zstd. Results vary by repository — pgit tends to win on source-code-heavy repos with incremental changes, while git wins on repos with large vendored dependencies or binary assets.

<details>
<summary><strong>Run the benchmarks yourself</strong></summary>

pgit includes `pgit-bench`, a CLI tool that benchmarks compression against git on real repositories. Results are generated as a markdown report with charts.

```bash
# Build the benchmark tool
go build -o pgit-bench ./cmd/pgit-bench/

# Run against the curated repo list (20 repos, ~8 min at --parallel 3)
./pgit-bench --file bench_repos.txt --parallel 3 --report BENCHMARK.md
```

The repo list in [`bench_repos.txt`](bench_repos.txt) covers 20 projects across 6 languages (Rust, Go, Python, JavaScript, TypeScript, C) — from small utilities like jq (1.9k commits) to large projects like git (80k commits).

You can also benchmark individual repos:

```bash
./pgit-bench https://github.com/tokio-rs/tokio
```

Requirements: `git` and `pgit` on PATH, local container running (`pgit local start`).

</details>

## v4 Changes

pgit v4 focuses on its strength as a **repository analysis tool**. The storage layer
now uses N:1 path-to-group mapping — files that share content (renames, copies,
reverts) are grouped into the same delta compression chain, eliminating ~40% of
duplicate storage.

As part of this direction, `pgit reset` and `pgit resolve` have been removed.
pgit is append-only by design: imported history is immutable, and the primary
workflow is `import` → `analyze` / `sql` / `search`.

## Commands

| Command | Description |
|---------|-------------|
| `pgit init` | Initialize new repository |
| `pgit add <files>` | Stage files for commit |
| `pgit rm <files>` | Remove files and stage the deletion |
| `pgit mv <src> <dst>` | Move/rename a file and stage the change |
| `pgit status` | Show working tree status |
| `pgit commit -m "msg"` | Record changes to the repository |
| `pgit log` | Show commit history (interactive) |
| `pgit diff` | Show changes between commits, working tree, etc. |
| `pgit show <commit>` | Show commit details |
| `pgit checkout <commit>` | Restore working tree files |
| `pgit blame <file>` | Show line-by-line attribution |
| `pgit search <pattern>` | Search file contents across history |
| `pgit analyze <analysis>` | Run pre-built analyses (churn, coupling, etc.) |
| `pgit sql <query>` | Run SQL queries on repository |
| `pgit stats` | Show repository statistics |
| `pgit remote add <name> <url>` | Add remote database |
| `pgit push <remote>` | Push to remote |
| `pgit pull <remote>` | Pull from remote |
| `pgit clone <url> [dir]` | Clone repository |
| `pgit import <git-repo>` | Import from Git |
| `pgit config <key> [value]` | Get and set repository options |
| `pgit clean` | Remove untracked files from working tree |
| `pgit doctor` | Check system health and diagnose issues |
| `pgit local <cmd>` | Manage local container (start, stop, status, logs, destroy, update) |
| `pgit repos` | Manage pgit repositories in the local container |
| `pgit update` | Check for pgit updates |

## Remote Setup

To test push/pull/clone, you can spin up a pg-xpatch container as your remote:

```bash
# Start a pg-xpatch container (creates database 'myproject' automatically)
docker run -d --name pgit-remote \
  -e POSTGRES_USER=pgit \
  -e POSTGRES_PASSWORD=pgit \
  -e POSTGRES_DB=myproject \
  -p 5433:5432 \
  ghcr.io/imgajeed76/pg-xpatch:latest

# Add it as a remote and push
pgit remote add origin postgres://pgit:pgit@localhost:5433/myproject
pgit push origin
```

The database name can be anything you want - just make sure it matches in both the container and the connection URL. pgit will initialize the schema automatically on first push.

## Shell Completions

```bash
# Bash
pgit completion bash > /etc/bash_completion.d/pgit

# Zsh
pgit completion zsh > "${fpath[1]}/_pgit"

# Fish
pgit completion fish > ~/.config/fish/completions/pgit.fish
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `PGIT_CONTAINER_RUNTIME` | Force `docker` or `podman` |
| `PGIT_ACCESSIBLE` | Set to `1` for accessibility mode (no animations) |
| `NO_COLOR` | Disable colored output |

## License

MIT

---
Built with ❤️ by [Oliver Seifert](https://oseifert.ch)
