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

## Compression: pgit vs git vs fossil

Benchmarked on real repositories (single branch, full history). Comparing packfile vs table data only (excluding indexes for both):

### git/git (79,588 commits, 3.8 GB raw content)

| | git --aggressive | pgit | fossil |
|--|------------------|------|--------|
| **Storage** | 91 MB | **53.5 MB** | 326 MB |
| **Import time** | - | 16 min | 32 min |

**pgit is 41% smaller than git with `git gc --aggressive`, and 84% smaller than fossil.**

### tokio (4,377 commits, 179 MB raw content)

| | git --aggressive | pgit | fossil |
|--|------------------|------|--------|
| **Storage** | 8.3 MB | **7.4 MB** | 8.1 MB |
| **Import time** | - | 17 sec | 13 sec |

pgit uses [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression with zstd. Compression improves with repository size - larger repos see better results.

## Features

- **Git-familiar commands**: init, add, commit, log, diff, checkout, push, pull, clone
- **PostgreSQL as remote**: Connection URL is your "remote" - no separate auth system
- **SQL queryable**: Run arbitrary queries on your entire repo history
- **Delta compression**: pg-xpatch achieves better compression than git's packfiles (up to 41% smaller)
- **Search across history**: `pgit search "pattern"` searches all versions of all files
- **Local development**: Uses Docker/Podman container for local database
- **Import from Git**: Migrate existing repositories with full history

## Installation

### Using Go (recommended)

```bash
go install github.com/imgajeed76/pgit/cmd/pgit@latest
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

## Query Your Repository

pgit stores everything in PostgreSQL, so you can query it directly:

```bash
# Built-in search across all history
pgit search "TODO" --path "*.rs"
pgit search --all "panic!" --ignore-case

# Raw SQL access
pgit sql "SELECT * FROM pgit_commits ORDER BY created_at DESC LIMIT 10"
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

-- File size growth over time
SELECT 
  EXTRACT(YEAR FROM c.created_at)::int as year,
  pg_size_pretty(AVG(LENGTH(ct.content))::bigint) as avg_size
FROM pgit_file_refs r
JOIN pgit_commits c ON r.commit_id = c.id
JOIN pgit_content ct ON ct.group_id = r.group_id AND ct.version_id = r.version_id
GROUP BY EXTRACT(YEAR FROM c.created_at)
ORDER BY year;
```

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
| `pgit status` | Show working tree status |
| `pgit commit -m "msg"` | Create a commit |
| `pgit log` | Show commit history (interactive) |
| `pgit diff` | Show changes |
| `pgit show <commit>` | Show commit details |
| `pgit checkout <commit>` | Restore files |
| `pgit blame <file>` | Show line-by-line attribution |
| `pgit search <pattern>` | Search across history |
| `pgit sql <query>` | Run SQL queries on repository |
| `pgit remote add <name> <url>` | Add remote database |
| `pgit push <remote>` | Push to remote |
| `pgit pull <remote>` | Pull from remote |
| `pgit clone <url> [dir]` | Clone repository |
| `pgit import <git-repo>` | Import from Git |
| `pgit stats` | Show repository statistics |
| `pgit local start/stop` | Manage local container |

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
