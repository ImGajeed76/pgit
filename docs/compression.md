---
title: Compression vs git
description: How pgit's delta compression compares to git packfiles, with the honest two-number story and how to measure your own repository.
authors:
  - handle: imgajeed

docolin:
  schema_version: 1
  kind: tools/pgit/compression
  type: explanation
  applies_to:
    - pgit 4
  language: en
  difficulty: intermediate
  time_estimate: 8m
  status: stable
  aliases:
    - pgit vs git compression
    - pgit benchmark
    - delta compression results
    - storage size
  references:
    - https://github.com/ImGajeed76/oseifert-data/blob/master/data/blog/linux-kernel-pgit.md
    - https://github.com/ImGajeed76/oseifert-data/blob/master/data/blog/building-pgit.md
  prev: ./how-it-works.md
  next: ./importing-a-repo.md
---

# Compression vs git

A fair question about putting a repo in PostgreSQL is whether it blows up on disk. The short answer: pgit's delta compression is competitive with git's packfiles, it wins on some repos and loses on others, and the real payoff is that the result is queryable. The compression is the means, not the headline.

This page uses the maintainer's own benchmarks. Treat the numbers as a guide, not a promise: compression depends entirely on how similar your file versions are, so the only number that describes your repo is the one you measure on your repo.

## Two numbers, not one

pgit reports storage two ways, and they answer different questions:

- **On-disk** is what PostgreSQL actually occupies: the compressed content plus tuple headers, TOAST metadata, page alignment, and padding. It is the honest "how much disk did this cost" figure.
- **Actual data** strips that database overhead and counts only the compressed content and raw column bytes. It is the apples-to-apples figure against a git packfile, which has no such per-row overhead.

!!! warning "On disk, git aggressive is smaller, every time"
    Across the 20-repo benchmark, pgit's **on-disk** size is larger than `git gc --aggressive` in every single repository, because PostgreSQL adds 10% to 40% overhead. pgit's wins are on the **actual data** metric, where that overhead is removed. Be clear about which number you are quoting.

## The 20-repo benchmark

The bundled `pgit-bench` tool imported 20 real repositories (273,703 commits across six languages) and compared pgit against git. Methodology: raw size is the sum of all git object sizes; git's number is the `.pack` file after `gc`/`gc --aggressive`; pgit's actual-data number is the xpatch compressed size plus raw bytes of the heap columns, indexes excluded on both sides.

On the actual-data metric, the scorecard is **pgit 12 wins, git 8 wins** against `git gc --aggressive`.

| Repo | git --aggressive | pgit (actual data) |
| ----- | ---------------- | ------------------ |
| fzf | 61.3 | 71.1 |
| serde | 36.5 | 51.6 |
| core | 51.8 | 53.6 |
| cargo | 42.5 | 42.7 |
| redis | 28.6 | 26.6 |
| react | 21.3 | 19.9 |
| prettier | 31.8 | 23.0 |
| git | 82.0 | 66.8 |

{ .chart type=bar title="Compression ratio, higher is better (8 of 20 repos)" }

The pattern is consistent: pgit pulls ahead on source-heavy repos with many small incremental edits (fzf, serde, core), and falls behind where git's cross-file delta search wins, typically repos with large generated, vendored, or reformatted files (prettier, react, git itself).

!!! note "Numbers are from one run"
    These figures were generated on 2026-02-22 with `pgit-bench`. Absolute sizes and the exact win/loss split depend on the repositories, their state on that day, and the pg-xpatch version. The full table, charts, and per-repo detail live in [BENCHMARK.md](../BENCHMARK.md).

## A bigger example: the Linux kernel

The maintainer imported the entire Linux kernel history (1,428,882 commits, 24.4 million file versions) into pgit. The compression story there is a clean illustration of the trade-off:

| Measure | Size |
| ------- | ---- |
| Raw git objects | 144.43 GB |
| git gc --aggressive | 1.95 GB |
| pgit actual data | 2.7 GB |
| pgit on-disk (with indexes) | 6.6 GB |

git wins outright on raw bytes: 1.95 GB beats pgit's 2.7 GB of actual data, and `git gc --aggressive` took roughly 25 minutes to produce that packfile. The kernel's text content alone compressed 114x (123 GB down to 1.1 GB) inside pgit. As the author put it, git winning the byte count is expected; the question is what you can do after. With pgit you get a 6.6 GB database you can run SQL across in seconds, which a packfile cannot do.

## When pgit wins and when it does not

- **Wins:** source code with incremental line-level edits, long histories of the same files, lots of renames and copies (which share a delta chain).
- **Loses:** repos dominated by large binary assets, vendored dependencies, minified or generated output, or frequent wholesale reformatting, where consecutive versions are not similar.

If every version is unrelated to the last, a delta is as big as the content, and git's packfile (which searches for similar objects across the whole repo) will usually do better.

## Measure your own

Do not trust a headline number, including these. Import your repo and ask pgit directly:

```bash
pgit stats
```

That prints commit and file counts, on-disk size per table, actual data, PostgreSQL overhead, and the data compression ratio. For the per-table delta-chain detail (keyframes, deltas, average chain depth, cache hit rate), add the slower flag:

```bash
pgit stats --xpatch
```

Both accept `--json` if you want to feed the numbers somewhere else.

## Where to go next

!!! cards { cols=2 }
    - [Importing a repository](./importing-a-repo.md){ icon=download }
      Get your own history in so you can measure it.

    - [Tuning compression in pg-xpatch](/imgajeed/pg-xpatch/tuning-compression){ icon=sliders }
      The knobs that trade write speed against storage, from the engine's docs.
