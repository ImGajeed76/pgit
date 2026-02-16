# pgit Compression Benchmark

> **Note:** Raw sizes were corrected after generation. The benchmark tool had a bug where
> `git cat-file --batch` (which outputs object content) was used instead of `--batch-check`
> (header only), causing content lines to be parsed as sizes. Raw sizes below were recomputed
> using `git cat-file --batch-all-objects --batch-check='%(objecttype) %(objectsize)'` from
> the original clones. Stored sizes (git packfile and pgit actual) were unaffected by the bug.

**Date:** 2026-02-16  
**Repositories:** 19 across 6 languages (Rust, Go, Python, JavaScript, TypeScript, C)  
**Total commits:** 193,635

## Summary

| Repository | Commits | Raw Size | git (normal) | git (aggressive) | pgit (actual) | Best Ratio | Duration |
|:-----------|--------:|---------:|-------------:|-----------------:|--------------:|-----------:|---------:|
| serde | 4,352 | 203.5 MB | 8.6 MB | 5.6 MB | 3.9 MB | 52.2x (pgit) | 41s |
| ripgrep | 2,207 | 111.8 MB | 5.3 MB | 3.0 MB | 2.9 MB | 38.6x (pgit) | 65s |
| tokio | 4,394 | 195.5 MB | 15.6 MB | 8.3 MB | 7.7 MB | 25.4x (pgit) | 44s |
| ruff | 14,116 | 2.8 GB | 101.8 MB | 51.0 MB | 56.6 MB | 56.5x (git) | 789s |
| cargo | 21,833 | 1.2 GB | 63.5 MB | 29.8 MB | 30.3 MB | 42.6x (git) | 773s |
| fzf | 3,482 | 209.2 MB | 7.6 MB | 3.4 MB | 2.7 MB | 77.5x (pgit) | 37s |
| gin | 1,961 | 51.7 MB | 3.8 MB | 1.9 MB | 1.6 MB | 32.3x (pgit) | 22s |
| hugo | 9,520 | 569.3 MB | 133.1 MB | 108.8 MB | 222.9 MB | 5.2x (git) | 222s |
| cli | 10,776 | 287.3 MB | 48.2 MB | 41.8 MB | 41.8 MB | 6.9x (tie) | 335s |
| flask | 5,506 | 165.6 MB | 11.4 MB | 6.0 MB | 5.5 MB | 30.1x (pgit) | 253s |
| requests | 6,405 | 112.4 MB | 12.4 MB | 9.3 MB | 9.5 MB | 12.1x (git) | 50s |
| express | 6,128 | 150.0 MB | 9.3 MB | 5.8 MB | 5.2 MB | 28.8x (pgit) | 440s |
| core | 6,930 | 598.9 MB | 30.4 MB | 11.6 MB | 9.9 MB | 60.5x (pgit) | 192s |
| svelte | 10,948 | 779.1 MB | 112.2 MB | 96.4 MB | 102.6 MB | 8.1x (git) | 208s |
| prettier | 11,084 | 2.0 GB | 162.3 MB | 66.2 MB | 96.4 MB | 31.7x (git) | 1975s |
| react | 21,368 | 2.2 GB | 123.1 MB | 104.9 MB | 121.4 MB | 21.2x (git) | 1187s |
| jq | 1,871 | 121.2 MB | 7.2 MB | 3.9 MB | 5.1 MB | 31.1x (git) | 44s |
| redis | 12,936 | 2.0 GB | 187.0 MB | 71.6 MB | 76.9 MB | 28.5x (git) | 743s |
| curl | 37,818 | 3.3 GB | 105.8 MB | 48.4 MB | 45.0 MB | 74.9x (pgit) | 866s |

**Scorecard: pgit 9 wins, git 9 wins, 1 tie(s)** out of 19 repositories.

## Compression: git aggressive vs pgit

### Stored Size

![Stored Size Comparison](https://quickchart.io/chart?w=900&h=400&c=%7B%22type%22%3A%22bar%22%2C%22data%22%3A%7B%22labels%22%3A%5B%22serde%22%2C%22ripgrep%22%2C%22tokio%22%2C%22ruff%22%2C%22cargo%22%2C%22fzf%22%2C%22gin%22%2C%22hugo%22%2C%22cli%22%2C%22flask%22%2C%22requests%22%2C%22express%22%2C%22core%22%2C%22svelte%22%2C%22prettier%22%2C%22react%22%2C%22jq%22%2C%22redis%22%2C%22curl%22%5D%2C%22datasets%22%3A%5B%7B%22label%22%3A%22git%20aggressive%22%2C%22data%22%3A%5B5.6%2C3.0%2C8.3%2C51.0%2C29.8%2C3.4%2C1.9%2C108.8%2C41.8%2C6.0%2C9.3%2C5.8%2C11.6%2C96.4%2C66.2%2C104.9%2C3.9%2C71.6%2C48.4%5D%2C%22backgroundColor%22%3A%22%233B82F6%22%7D%2C%7B%22label%22%3A%22pgit%20actual%20data%22%2C%22data%22%3A%5B3.9%2C2.9%2C7.7%2C56.6%2C30.3%2C2.7%2C1.6%2C222.9%2C41.8%2C5.5%2C9.5%2C5.2%2C9.9%2C102.6%2C96.4%2C121.4%2C5.1%2C76.9%2C45.0%5D%2C%22backgroundColor%22%3A%22%237C3AED%22%7D%5D%7D%2C%22options%22%3A%7B%22title%22%3A%7B%22display%22%3Atrue%2C%22text%22%3A%22Stored%20Size%20%28MB%29%20%5Cu2014%20lower%20is%20better%22%7D%2C%22plugins%22%3A%7B%22datalabels%22%3A%7B%22display%22%3Atrue%2C%22anchor%22%3A%22end%22%2C%22align%22%3A%22top%22%2C%22font%22%3A%7B%22size%22%3A8%7D%7D%7D%7D%7D)

### Compression Ratio

Higher is better (raw uncompressed / stored size).

![Compression Ratio Comparison](https://quickchart.io/chart?w=900&h=400&c=%7B%22type%22%3A%22bar%22%2C%22data%22%3A%7B%22labels%22%3A%5B%22serde%22%2C%22ripgrep%22%2C%22tokio%22%2C%22ruff%22%2C%22cargo%22%2C%22fzf%22%2C%22gin%22%2C%22hugo%22%2C%22cli%22%2C%22flask%22%2C%22requests%22%2C%22express%22%2C%22core%22%2C%22svelte%22%2C%22prettier%22%2C%22react%22%2C%22jq%22%2C%22redis%22%2C%22curl%22%5D%2C%22datasets%22%3A%5B%7B%22label%22%3A%22git%20aggressive%22%2C%22data%22%3A%5B36.3%2C37.3%2C23.6%2C56.5%2C42.6%2C61.5%2C27.2%2C5.2%2C6.9%2C27.6%2C12.1%2C25.9%2C51.6%2C8.1%2C31.7%2C21.2%2C31.1%2C28.5%2C69.6%5D%2C%22backgroundColor%22%3A%22%233B82F6%22%7D%2C%7B%22label%22%3A%22pgit%20actual%20data%22%2C%22data%22%3A%5B52.2%2C38.6%2C25.4%2C50.9%2C41.9%2C77.5%2C32.3%2C2.6%2C6.9%2C30.1%2C11.8%2C28.8%2C60.5%2C7.6%2C21.8%2C18.4%2C23.8%2C26.6%2C74.9%5D%2C%22backgroundColor%22%3A%22%237C3AED%22%7D%5D%7D%2C%22options%22%3A%7B%22title%22%3A%7B%22display%22%3Atrue%2C%22text%22%3A%22Compression%20Ratio%20%5Cu2014%20higher%20is%20better%22%7D%2C%22plugins%22%3A%7B%22datalabels%22%3A%7B%22display%22%3Atrue%2C%22anchor%22%3A%22end%22%2C%22align%22%3A%22top%22%2C%22font%22%3A%7B%22size%22%3A8%7D%7D%7D%7D%7D)

## Per-Repository Details

### serde

**URL:** https://github.com/serde-rs/serde  
**Branch:** master  
**Commits:** 4,352 | **Files:** 880  
**Duration:** 41s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 8.6 MB | 5.6 MB | 6.0 MB | 3.9 MB |
| Compression ratio | 23.7x | 36.3x | 33.9x | 52.2x |
| vs git normal | - | - | 0.70x | 0.45x |
| vs git aggressive | - | - | 1.07x | 0.70x |

**PostgreSQL overhead:** 2.2 MB (35.8% of on-disk)

> **pgit wins** on actual data compression: 52.2x vs git aggressive 36.3x (30% better ratio)

---

### ripgrep

**URL:** https://github.com/BurntSushi/ripgrep  
**Branch:** master  
**Commits:** 2,207 | **Files:** 449  
**Duration:** 65s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 5.3 MB | 3.0 MB | 4.1 MB | 2.9 MB |
| Compression ratio | 21.1x | 37.3x | 27.3x | 38.6x |
| vs git normal | - | - | 0.77x | 0.55x |
| vs git aggressive | - | - | 1.37x | 0.97x |

**PostgreSQL overhead:** 1.2 MB (28.9% of on-disk)

> **pgit wins** on actual data compression: 38.6x vs git aggressive 37.3x (3% better ratio)

---

### tokio

**URL:** https://github.com/tokio-rs/tokio  
**Branch:** master  
**Commits:** 4,394 | **Files:** 2,403  
**Duration:** 44s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 15.6 MB | 8.3 MB | 10.6 MB | 7.7 MB |
| Compression ratio | 12.5x | 23.6x | 18.4x | 25.4x |
| vs git normal | - | - | 0.68x | 0.49x |
| vs git aggressive | - | - | 1.28x | 0.93x |

**PostgreSQL overhead:** 2.9 MB (27.5% of on-disk)

> **pgit wins** on actual data compression: 25.4x vs git aggressive 23.6x (7% better ratio)

---

### ruff

**URL:** https://github.com/astral-sh/ruff  
**Branch:** main  
**Commits:** 14,116 | **Files:** 25,081  
**Duration:** 789s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 101.8 MB | 51.0 MB | 72.8 MB | 56.6 MB |
| Compression ratio | 28.3x | 56.5x | 39.5x | 50.9x |
| vs git normal | - | - | 0.72x | 0.56x |
| vs git aggressive | - | - | 1.43x | 1.11x |

**PostgreSQL overhead:** 16.2 MB (22.3% of on-disk)

> **git aggressive wins** on compression: 56.5x vs pgit actual 50.9x (11% better ratio)

---

### cargo

**URL:** https://github.com/rust-lang/cargo  
**Branch:** master  
**Commits:** 21,833 | **Files:** 6,589  
**Duration:** 773s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 63.5 MB | 29.8 MB | 43.9 MB | 30.3 MB |
| Compression ratio | 20.0x | 42.6x | 28.9x | 41.9x |
| vs git normal | - | - | 0.69x | 0.48x |
| vs git aggressive | - | - | 1.47x | 1.02x |

**PostgreSQL overhead:** 13.6 MB (31.1% of on-disk)

> **git aggressive wins** on compression: 42.6x vs pgit actual 41.9x (2% better ratio)

---

### fzf

**URL:** https://github.com/junegunn/fzf  
**Branch:** master  
**Commits:** 3,482 | **Files:** 184  
**Duration:** 37s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 7.6 MB | 3.4 MB | 4.1 MB | 2.7 MB |
| Compression ratio | 27.5x | 61.5x | 51.0x | 77.5x |
| vs git normal | - | - | 0.54x | 0.36x |
| vs git aggressive | - | - | 1.21x | 0.79x |

**PostgreSQL overhead:** 1.4 MB (33.8% of on-disk)

> **pgit wins** on actual data compression: 77.5x vs git aggressive 61.5x (21% better ratio)

---

### gin

**URL:** https://github.com/gin-gonic/gin  
**Branch:** master  
**Commits:** 1,961 | **Files:** 251  
**Duration:** 22s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 3.8 MB | 1.9 MB | 2.6 MB | 1.6 MB |
| Compression ratio | 13.6x | 27.2x | 19.9x | 32.3x |
| vs git normal | - | - | 0.68x | 0.42x |
| vs git aggressive | - | - | 1.37x | 0.84x |

**PostgreSQL overhead:** 1.0 MB (38.6% of on-disk)

> **pgit wins** on actual data compression: 32.3x vs git aggressive 27.2x (16% better ratio)

---

### hugo

**URL:** https://github.com/gohugoio/hugo  
**Branch:** master  
**Commits:** 9,520 | **Files:** 11,284  
**Duration:** 222s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 133.1 MB | 108.8 MB | 240.4 MB | 222.9 MB |
| Compression ratio | 4.3x | 5.2x | 2.4x | 2.6x |
| vs git normal | - | - | 1.81x | 1.67x |
| vs git aggressive | - | - | 2.21x | 2.05x |

**PostgreSQL overhead:** 17.5 MB (7.3% of on-disk)

> **git aggressive wins** on compression: 5.2x vs pgit actual 2.6x (105% better ratio)

---

### cli

**URL:** https://github.com/cli/cli  
**Branch:** trunk  
**Commits:** 10,776 | **Files:** 3,010  
**Duration:** 335s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 48.2 MB | 41.8 MB | 49.8 MB | 41.8 MB |
| Compression ratio | 6.0x | 6.9x | 5.8x | 6.9x |
| vs git normal | - | - | 1.03x | 0.87x |
| vs git aggressive | - | - | 1.19x | 1.00x |

**PostgreSQL overhead:** 7.9 MB (15.9% of on-disk)

> **Tie** — git aggressive and pgit actual are within 0.1 MB (6.9x vs 6.9x)

---

### flask

**URL:** https://github.com/pallets/flask  
**Branch:** main  
**Commits:** 5,506 | **Files:** 642  
**Duration:** 253s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 11.4 MB | 6.0 MB | 8.1 MB | 5.5 MB |
| Compression ratio | 14.5x | 27.6x | 20.4x | 30.1x |
| vs git normal | - | - | 0.71x | 0.48x |
| vs git aggressive | - | - | 1.35x | 0.92x |

**PostgreSQL overhead:** 2.6 MB (32.0% of on-disk)

> **pgit wins** on actual data compression: 30.1x vs git aggressive 27.6x (8% better ratio)

---

### requests

**URL:** https://github.com/psf/requests  
**Branch:** main  
**Commits:** 6,405 | **Files:** 460  
**Duration:** 50s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 12.4 MB | 9.3 MB | 12.0 MB | 9.5 MB |
| Compression ratio | 9.1x | 12.1x | 9.4x | 11.8x |
| vs git normal | - | - | 0.97x | 0.77x |
| vs git aggressive | - | - | 1.29x | 1.02x |

**PostgreSQL overhead:** 2.4 MB (20.3% of on-disk)

> **git aggressive wins** on compression: 12.1x vs pgit actual 11.8x (2% better ratio)

---

### express

**URL:** https://github.com/expressjs/express  
**Branch:** master  
**Commits:** 6,128 | **Files:** 906  
**Duration:** 440s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 9.3 MB | 5.8 MB | 7.6 MB | 5.2 MB |
| Compression ratio | 16.1x | 25.9x | 19.7x | 28.8x |
| vs git normal | - | - | 0.82x | 0.56x |
| vs git aggressive | - | - | 1.31x | 0.90x |

**PostgreSQL overhead:** 2.4 MB (31.0% of on-disk)

> **pgit wins** on actual data compression: 28.8x vs git aggressive 25.9x (10% better ratio)

---

### core

**URL:** https://github.com/vuejs/core  
**Branch:** main  
**Commits:** 6,930 | **Files:** 1,342  
**Duration:** 192s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 30.4 MB | 11.6 MB | 13.5 MB | 9.9 MB |
| Compression ratio | 19.7x | 51.6x | 44.4x | 60.5x |
| vs git normal | - | - | 0.44x | 0.33x |
| vs git aggressive | - | - | 1.16x | 0.85x |

**PostgreSQL overhead:** 3.5 MB (26.2% of on-disk)

> **pgit wins** on actual data compression: 60.5x vs git aggressive 51.6x (15% better ratio)

---

### svelte

**URL:** https://github.com/sveltejs/svelte  
**Branch:** main  
**Commits:** 10,948 | **Files:** 29,746  
**Duration:** 208s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 112.2 MB | 96.4 MB | 120.4 MB | 102.6 MB |
| Compression ratio | 6.9x | 8.1x | 6.5x | 7.6x |
| vs git normal | - | - | 1.07x | 0.91x |
| vs git aggressive | - | - | 1.25x | 1.06x |

**PostgreSQL overhead:** 17.8 MB (14.8% of on-disk)

> **git aggressive wins** on compression: 8.1x vs pgit actual 7.6x (6% better ratio)

---

### prettier

**URL:** https://github.com/prettier/prettier  
**Branch:** main  
**Commits:** 11,084 | **Files:** 30,913  
**Duration:** 1975s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 162.3 MB | 66.2 MB | 113.4 MB | 96.4 MB |
| Compression ratio | 12.9x | 31.7x | 18.5x | 21.8x |
| vs git normal | - | - | 0.70x | 0.59x |
| vs git aggressive | - | - | 1.71x | 1.46x |

**PostgreSQL overhead:** 17.1 MB (15.1% of on-disk)

> **git aggressive wins** on compression: 31.7x vs pgit actual 21.8x (46% better ratio)

---

### react

**URL:** https://github.com/facebook/react  
**Branch:** main  
**Commits:** 21,368 | **Files:** 26,042  
**Duration:** 1187s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 123.1 MB | 104.9 MB | 142.8 MB | 121.4 MB |
| Compression ratio | 18.1x | 21.2x | 15.6x | 18.4x |
| vs git normal | - | - | 1.16x | 0.99x |
| vs git aggressive | - | - | 1.36x | 1.16x |

**PostgreSQL overhead:** 21.3 MB (14.9% of on-disk)

> **git aggressive wins** on compression: 21.2x vs pgit actual 18.4x (16% better ratio)

---

### jq

**URL:** https://github.com/jqlang/jq  
**Branch:** master  
**Commits:** 1,871 | **Files:** 607  
**Duration:** 44s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 7.2 MB | 3.9 MB | 6.4 MB | 5.1 MB |
| Compression ratio | 16.8x | 31.1x | 18.9x | 23.8x |
| vs git normal | - | - | 0.89x | 0.71x |
| vs git aggressive | - | - | 1.64x | 1.31x |

**PostgreSQL overhead:** 1.3 MB (20.2% of on-disk)

> **git aggressive wins** on compression: 31.1x vs pgit actual 23.8x (31% better ratio)

---

### redis

**URL:** https://github.com/redis/redis  
**Branch:** unstable  
**Commits:** 12,936 | **Files:** 2,939  
**Duration:** 743s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 187.0 MB | 71.6 MB | 85.0 MB | 76.9 MB |
| Compression ratio | 10.9x | 28.5x | 24.0x | 26.6x |
| vs git normal | - | - | 0.45x | 0.41x |
| vs git aggressive | - | - | 1.19x | 1.07x |

**PostgreSQL overhead:** 8.1 MB (9.5% of on-disk)

> **git aggressive wins** on compression: 28.5x vs pgit actual 26.6x (7% better ratio)

---

### curl

**URL:** https://github.com/curl/curl  
**Branch:** master  
**Commits:** 37,818 | **Files:** 7,157  
**Duration:** 866s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 105.8 MB | 48.4 MB | 62.1 MB | 45.0 MB |
| Compression ratio | 31.8x | 69.6x | 54.2x | 74.9x |
| vs git normal | - | - | 0.59x | 0.43x |
| vs git aggressive | - | - | 1.28x | 0.93x |

**PostgreSQL overhead:** 17.1 MB (27.5% of on-disk)

> **pgit wins** on actual data compression: 74.9x vs git aggressive 69.6x (7% better ratio)

---

## PostgreSQL Overhead

| Repository | On-disk | Actual Data | Overhead | Overhead % |
|:-----------|--------:|------------:|---------:|-----------:|
| serde | 6.0 MB | 3.9 MB | 2.2 MB | 35.8% |
| ripgrep | 4.1 MB | 2.9 MB | 1.2 MB | 28.9% |
| tokio | 10.6 MB | 7.7 MB | 2.9 MB | 27.5% |
| ruff | 72.8 MB | 56.6 MB | 16.2 MB | 22.3% |
| cargo | 43.9 MB | 30.3 MB | 13.6 MB | 31.1% |
| fzf | 4.1 MB | 2.7 MB | 1.4 MB | 33.8% |
| gin | 2.6 MB | 1.6 MB | 1.0 MB | 38.6% |
| hugo | 240.4 MB | 222.9 MB | 17.5 MB | 7.3% |
| cli | 49.8 MB | 41.8 MB | 7.9 MB | 15.9% |
| flask | 8.1 MB | 5.5 MB | 2.6 MB | 32.0% |
| requests | 12.0 MB | 9.5 MB | 2.4 MB | 20.3% |
| express | 7.6 MB | 5.2 MB | 2.4 MB | 31.0% |
| core | 13.5 MB | 9.9 MB | 3.5 MB | 26.2% |
| svelte | 120.4 MB | 102.6 MB | 17.8 MB | 14.8% |
| prettier | 113.4 MB | 96.4 MB | 17.1 MB | 15.1% |
| react | 142.8 MB | 121.4 MB | 21.3 MB | 14.9% |
| jq | 6.4 MB | 5.1 MB | 1.3 MB | 20.2% |
| redis | 85.0 MB | 76.9 MB | 8.1 MB | 9.5% |
| curl | 62.1 MB | 45.0 MB | 17.1 MB | 27.5% |

Overhead ranges from 7.3% (hugo) to 38.6% (gin).

PostgreSQL overhead includes: tuple headers (23 bytes/row), TOAST chunk metadata, page headers, alignment padding.

## Methodology

### Raw Uncompressed Size
- `git cat-file --batch-all-objects --batch-check='%(objecttype) %(objectsize)'` — sum of all object sizes
- Same number used as the numerator for all compression ratios

### Git Storage
- **Normal:** `git gc` then measure `.pack` file size
- **Aggressive:** `git gc --aggressive` then measure `.pack` file size
- Only the packfile is counted (`.idx`, `.rev`, `.bitmap` are reconstructable)

### pgit Storage
- **On-disk:** `pg_table_size()` sum across all `pgit_*` tables (heap + TOAST, no indexes)
- **Actual data:** `xpatch.stats()` `compressed_size_bytes` for xpatch tables + `SUM(octet_length(columns))` for normal tables
- Strips PostgreSQL overhead (tuple headers, TOAST chunk metadata, page headers, alignment)
- Indexes excluded (reconstructable, same as git)

### Compression Ratio
- `raw_uncompressed / stored_size` — same numerator for all methods
- Higher is better

---
*Generated by pgit-bench, raw sizes corrected manually*

