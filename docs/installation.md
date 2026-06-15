---
title: Installing pgit
description: Install the pgit CLI with Go, a prebuilt binary, or a system package, then verify the one runtime requirement.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/installation
  type: how-to
  applies_to:
    - pgit 4
    - docker
    - podman
  language: en
  difficulty: beginner
  time_estimate: 10m
  status: stable
  aliases:
    - install pgit
    - pgit setup
    - go install pgit
  prev: ./overview.md
  next: ./quickstart.md
---

# Installing pgit

pgit ships as a single static binary. Installing it is quick; the one thing to get right is the runtime requirement, because the local database lives in a container.

## Requirements

- **Docker or Podman.** pgit runs PostgreSQL (with [pg-xpatch](/imgajeed/pg-xpatch/overview)) in a container for the local database. One of the two must be installed and working.
- **git**, only if you plan to `import` an existing repository. Import shells out to `git fast-export`.
- **A pg-xpatch PostgreSQL server**, only for remote `push` / `pull` / `clone`. The local container already includes pg-xpatch, so you do not need a separate server for local use.

!!! info "Why a container instead of embedded PostgreSQL?"
    pg-xpatch is a compiled PostgreSQL extension. Bundling and cross-compiling it for every OS proved impractical, so pgit leans on a prebuilt image (`ghcr.io/imgajeed76/pg-xpatch`) instead. The trade-off buys reliable cross-platform behaviour for the cost of a container runtime.

## Install the CLI

=== "Go"
    ```bash
    go install github.com/imgajeed76/pgit/v4/cmd/pgit@latest
    ```

    The binary lands in your Go bin directory (usually `~/go/bin`). Make sure that directory is on your `PATH`.

=== "Binary"
    Download the archive for your platform from the [releases page](https://github.com/imgajeed76/pgit/releases), unpack it, and put `pgit` somewhere on your `PATH`.

    - Linux: `pgit_*_linux_amd64.tar.gz` or `pgit_*_linux_arm64.tar.gz`
    - macOS: `pgit_*_darwin_amd64.tar.gz` or `pgit_*_darwin_arm64.tar.gz`
    - Windows: `pgit_*_windows_amd64.zip`

=== "Package"
    Grab the package matching your distro from the [releases page](https://github.com/imgajeed76/pgit/releases), then:

    ```bash
    # Debian / Ubuntu
    sudo dpkg -i pgit_*_linux_amd64.deb

    # RHEL / Fedora
    sudo rpm -i pgit_*_linux_amd64.rpm

    # Alpine
    sudo apk add --allow-untrusted pgit_*_linux_amd64.apk
    ```

    Packages install the binary to `/usr/bin/pgit`.

Confirm the binary runs:

```bash
pgit version
```

## Pick a container runtime

pgit auto-detects the runtime. It prefers Docker and falls back to Podman. To force one, set `PGIT_CONTAINER_RUNTIME`:

```bash
export PGIT_CONTAINER_RUNTIME=podman   # or docker
```

## Verify your setup

`pgit doctor` runs through everything pgit needs and tells you what, if anything, is missing.

!!! steps "First run"
    1. Check the runtime and overall health:

       ```bash
       pgit doctor
       ```

       It reports the container runtime and version, container status, and (inside a repo) the database connection and your commit identity.

    2. Start the local database container:

       ```bash
       pgit local start
       ```

       The first start pulls the pg-xpatch image and brings PostgreSQL up on port 5433 (bound to localhost). A later import or commit would start it for you anyway, so this step is optional but reassuring.

    3. You are ready. Head to the [quickstart](./quickstart.md) to import a repo.

!!! tip "Shell completions"
    pgit generates completions for bash, zsh, fish, and PowerShell. For example: `pgit completion zsh > "${fpath[1]}/_pgit"`. Run `pgit completion --help` for the per-shell install line.

## Next

!!! cards { cols=2 }
    - [Quickstart](./quickstart.md){ icon=rocket }
      Import a real repository and run your first analysis.

    - [Managing the local container](./local-container.md){ icon=container }
      Start, stop, update, and reset the shared database container.
