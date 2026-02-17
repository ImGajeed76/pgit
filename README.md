# pgit

A Git-like version control CLI backed by PostgreSQL with [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression.

> **Note:** pgit is primarily a demo for [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression. It's not intended to replace git—but it *is* genuinely useful for importing a repo and running SQL analytics on your commit history.

## Why pgit?

**Import any git repo. Query it with SQL.**

```bash
pgit init
pgit import /path/to/your/repo --branch main
pgit sql "SELECT ..."
```

No scripts, no parsing `git log` output. Just SQL.

```sql
-- Which files are always changed together?
SELECT pa.path, pb.path, COUNT(*) as times_together
FROM pgit_file_refs a
JOIN pgit_paths pa ON pa.group_id = a.group_id
JOIN pgit_file_refs b ON a.commit_id = b.commit_id AND a.group_id < b.group_id
JOIN pgit_paths pb ON pb.group_id = b.group_id
GROUP BY pa.path, pb.path
ORDER BY times_together DESC;
```

## Compression: pgit vs git

Benchmarked on [19 real repositories](BENCHMARK.md) across 6 languages (193k total commits). Comparing `git gc --aggressive` packfile vs pgit actual data (excluding indexes for both):

**Scorecard: pgit 9 wins, git 9 wins, 1 tie.**

| Repository | Commits | Raw Size | git --aggressive | pgit | Winner |
|:-----------|--------:|---------:|-----------------:|-----:|:-------|
| serde | 4,352 | 203.5 MB | 5.6 MB | 3.9 MB | pgit (30%) |
| fzf | 3,482 | 209.2 MB | 3.4 MB | 2.7 MB | pgit (21%) |
| curl | 37,818 | 3.3 GB | 48.4 MB | 45.0 MB | pgit (7%) |
| cargo | 21,833 | 1.2 GB | 29.8 MB | 30.3 MB | git (2%) |
| prettier | 11,084 | 2.0 GB | 66.2 MB | 96.4 MB | git (46%) |
| hugo | 9,520 | 569.3 MB | 108.8 MB | 222.9 MB | git (105%) |

See the [full benchmark results](BENCHMARK.md) for all 19 repositories with detailed per-repo breakdowns, charts, and methodology.

pgit uses [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression with zstd. Results vary by repository — pgit tends to win on source-code-heavy repos with incremental changes, while git wins on repos with large vendored dependencies or binary assets.

### Run the benchmarks yourself

pgit includes `pgit-bench`, a CLI tool that benchmarks compression against git on real repositories. Results are generated as a markdown report with charts.

```bash
# Build the benchmark tool
go build -o pgit-bench ./cmd/pgit-bench/

# Run against the curated repo list (19 repos, ~50 min at 3 parallel)
./pgit-bench --file bench_repos.txt --parallel 3 --report BENCHMARK.md
```

The repo list in [`bench_repos.txt`](bench_repos.txt) covers 19 projects across 6 languages (Rust, Go, Python, JavaScript, TypeScript, C) — from small utilities like jq (1.9k commits) to large projects like curl (38k commits).

You can also benchmark individual repos:

```bash
./pgit-bench https://github.com/tokio-rs/tokio
```

Requirements: `git` and `pgit` on PATH, local container running (`pgit local start`).

## Features

- **Git-familiar commands**: init, add, commit, log, diff, checkout, push, pull, clone
- **PostgreSQL as remote**: Connection URL is your "remote" - no separate auth system
- **SQL queryable**: Run arbitrary queries on your entire repo history
- **Delta compression**: pg-xpatch achieves competitive compression with git's packfiles ([benchmark results](BENCHMARK.md))
- **Search across history**: `pgit search "pattern"` searches all versions of all files
- **Local development**: Uses Docker/Podman container for local database
- **Import from Git**: Migrate existing repositories with full history

## Installation

### Using Go (recommended)

```bash
go install github.com/imgajeed76/pgit/v3/cmd/pgit@latest
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

## Quick Start

```bash
# Initialize a new repository
pgit init
pgit config user.name "Your Name"
pgit config user.email "you@example.com"

# Basic workflow
pgit add .
pgit commit -m "Initial commit"
pgit log

# Set up remote and sync
pgit remote add origin postgres://user:pass@host/database
pgit push origin
```

## Analyze Your Repository

pgit includes pre-built analyses that are optimized for the underlying storage engine. No SQL knowledge needed:

```bash
pgit analyze churn                  # most frequently modified files
pgit analyze coupling               # files always changed together
pgit analyze hotspots --depth 2     # churn aggregated by directory
pgit analyze authors                # commits per contributor
pgit analyze activity --period month # commit velocity over time
pgit analyze bus-factor             # files with fewest authors (knowledge silos)
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
JOIN pgit_paths p ON p.group_id = r.group_id
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

## Testing Remote Functionality

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

## Importing from Git

```bash
pgit init
pgit import /path/to/git/repo --branch main
```

## Commands

| Command | Description |
|---------|-------------|
| `pgit init` | Initialize new repository |
| `pgit add <files>` | Stage files for commit |
| `pgit rm <files>` | Remove files and stage the deletion |
| `pgit mv <src> <dst>` | Move/rename a file and stage the change |
| `pgit status` | Show working tree status |
| `pgit commit -m "msg"` | Record changes to the repository |
| `pgit reset [commit]` | Reset HEAD, index, and working tree |
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
| `pgit resolve <file>` | Mark merge conflicts as resolved |
| `pgit config <key> [value]` | Get and set repository options |
| `pgit clean` | Remove untracked files from working tree |
| `pgit doctor` | Check system health and diagnose issues |
| `pgit local <cmd>` | Manage local container (start, stop, status, logs, destroy, update) |
| `pgit repos` | Manage pgit repositories in the local container |
| `pgit update` | Check for pgit updates |

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
