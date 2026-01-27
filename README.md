# pgit

A Git-like version control CLI backed by PostgreSQL with [pg-xpatch](https://github.com/imgajeed76/pg-xpatch) delta compression.

## Features

- **Git-familiar commands**: init, add, commit, log, diff, checkout, push, pull, clone
- **PostgreSQL as remote**: Connection URL is your "remote" - no separate auth system
- **Delta compression**: pg-xpatch provides excellent compression for versioned files
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
