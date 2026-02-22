# pgit-bench Compression Report

**Date:** 2026-02-22 02:40:16
**Repositories:** 20

## Summary

| Repository | Commits | Raw Size | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual) | PG Overhead | Best Ratio | Duration |
|:-----------|--------:|---------:|-------------:|-----------------:|---------------:|--------------:|------------:|-----------:|---------:|
| serde | 4,353 | 203.5 MB | 8.6 MB | 5.6 MB | 6.6 MB | 3.9 MB | 2.6 MB (40%) | 51.6x (pgit) | 12s |
| ripgrep | 2,208 | 111.8 MB | 5.3 MB | 3.0 MB | 4.2 MB | 2.7 MB | 1.5 MB (35%) | 40.6x (pgit) | 17s |
| tokio | 4,403 | 195.7 MB | 15.7 MB | 8.3 MB | 11.2 MB | 7.7 MB | 3.5 MB (31%) | 25.3x (pgit) | 19s |
| ruff | 14,206 | 2.8 GB | 106.3 MB | 51.3 MB | 67.4 MB | 51.4 MB | 16.0 MB (24%) | 56.7x (git) | 113s |
| cargo | 21,850 | 1.2 GB | 63.1 MB | 29.9 MB | 41.4 MB | 29.8 MB | 11.6 MB (28%) | 42.7x (pgit) | 78s |
| fzf | 3,499 | 213.3 MB | 7.7 MB | 3.5 MB | 4.6 MB | 3.0 MB | 1.6 MB (35%) | 71.1x (pgit) | 11s |
| gin | 1,967 | 51.7 MB | 3.8 MB | 1.9 MB | 2.7 MB | 1.7 MB | 1.0 MB (38%) | 30.6x (pgit) | 8s |
| hugo | 9,538 | 570.6 MB | 132.6 MB | 108.8 MB | 123.0 MB | 111.0 MB | 12.0 MB (10%) | 5.2x (git) | 42s |
| cli | 10,820 | 288.6 MB | 48.4 MB | 41.8 MB | 48.2 MB | 41.3 MB | 6.9 MB (14%) | 7.0x (pgit) | 28s |
| flask | 5,516 | 167.3 MB | 11.4 MB | 6.1 MB | 8.3 MB | 5.5 MB | 2.8 MB (34%) | 30.2x (pgit) | 14s |
| requests | 6,405 | 112.4 MB | 12.5 MB | 9.3 MB | 11.7 MB | 9.1 MB | 2.7 MB (23%) | 12.4x (pgit) | 13s |
| express | 6,128 | 150.0 MB | 9.3 MB | 5.8 MB | 8.4 MB | 5.7 MB | 2.7 MB (32%) | 26.4x (pgit) | 13s |
| core | 6,930 | 598.9 MB | 31.0 MB | 11.6 MB | 15.1 MB | 11.2 MB | 3.9 MB (26%) | 53.6x (pgit) | 33s |
| svelte | 10,982 | 782.7 MB | 111.8 MB | 96.5 MB | 111.5 MB | 96.0 MB | 15.5 MB (14%) | 8.2x (pgit) | 96s |
| prettier | 11,084 | 2.0 GB | 162.3 MB | 66.1 MB | 106.5 MB | 91.1 MB | 15.4 MB (14%) | 31.8x (git) | 76s |
| react | 21,378 | 2.2 GB | 123.8 MB | 105.0 MB | 132.2 MB | 112.3 MB | 19.9 MB (15%) | 21.3x (git) | 136s |
| jq | 1,871 | 121.2 MB | 7.2 MB | 3.9 MB | 5.5 MB | 4.2 MB | 1.3 MB (23%) | 31.3x (git) | 7s |
| redis | 12,940 | 2.0 GB | 187.5 MB | 71.6 MB | 85.2 MB | 76.9 MB | 8.2 MB (10%) | 28.6x (git) | 50s |
| curl | 37,860 | 3.3 GB | 105.9 MB | 48.4 MB | 67.5 MB | 49.3 MB | 18.3 MB (27%) | 69.7x (git) | 121s |
| git | 79,765 | 7.3 GB | 275.8 MB | 90.6 MB | 143.1 MB | 111.3 MB | 31.8 MB (22%) | 82.0x (git) | 465s |

## Compression: git aggressive vs pgit

### Stored Size

![Stored Size Comparison](https://quickchart.io/chart?w=900&h=400&c=%7Btype:%27bar%27%2Cdata:%7Blabels:%5B%27serde%27%2C%27ripgrep%27%2C%27tokio%27%2C%27ruff%27%2C%27cargo%27%2C%27fzf%27%2C%27gin%27%2C%27hugo%27%2C%27cli%27%2C%27flask%27%2C%27requests%27%2C%27express%27%2C%27core%27%2C%27svelte%27%2C%27prettier%27%2C%27react%27%2C%27jq%27%2C%27redis%27%2C%27curl%27%2C%27git%27%5D%2Cdatasets:%5B%7Blabel:%27git%20aggressive%27%2Cdata:%5B5.6%2C3.0%2C8.3%2C51.3%2C29.9%2C3.5%2C1.9%2C108.8%2C41.8%2C6.1%2C9.3%2C5.8%2C11.6%2C96.5%2C66.1%2C105.0%2C3.9%2C71.6%2C48.4%2C90.6%5D%2CbackgroundColor:%27%233B82F6%27%7D%2C%7Blabel:%27pgit%20actual%20data%27%2Cdata:%5B3.9%2C2.7%2C7.7%2C51.4%2C29.8%2C3.0%2C1.7%2C111.0%2C41.3%2C5.5%2C9.1%2C5.7%2C11.2%2C96.0%2C91.1%2C112.3%2C4.2%2C76.9%2C49.3%2C111.3%5D%2CbackgroundColor:%27%237C3AED%27%7D%5D%7D%2Coptions:%7Btitle:%7Bdisplay:true%2Ctext:%27Stored%20Size%20%E2%80%94%20lower%20is%20better%27%7D%2Cscales:%7ByAxes:%5B%7BscaleLabel:%7Bdisplay:true%2ClabelString:%27Size%20%28MB%29%27%7D%2Cticks:%7BbeginAtZero:true%7D%7D%5D%7D%2Cplugins:%7Bdatalabels:%7Bdisplay:true%2Canchor:%27end%27%2Calign:%27top%27%2Cfont:%7Bsize:8%7D%7D%7D%7D%7D)

### Compression Ratio

Higher is better (raw uncompressed / stored size).

![Compression Ratio Comparison](https://quickchart.io/chart?w=900&h=400&c=%7Btype:%27bar%27%2Cdata:%7Blabels:%5B%27serde%27%2C%27ripgrep%27%2C%27tokio%27%2C%27ruff%27%2C%27cargo%27%2C%27fzf%27%2C%27gin%27%2C%27hugo%27%2C%27cli%27%2C%27flask%27%2C%27requests%27%2C%27express%27%2C%27core%27%2C%27svelte%27%2C%27prettier%27%2C%27react%27%2C%27jq%27%2C%27redis%27%2C%27curl%27%2C%27git%27%5D%2Cdatasets:%5B%7Blabel:%27git%20aggressive%27%2Cdata:%5B36.5%2C36.8%2C23.5%2C56.7%2C42.5%2C61.3%2C26.8%2C5.2%2C6.9%2C27.6%2C12.1%2C25.7%2C51.8%2C8.1%2C31.8%2C21.3%2C31.3%2C28.6%2C69.7%2C82.0%5D%2CbackgroundColor:%27%233B82F6%27%7D%2C%7Blabel:%27pgit%20actual%20data%27%2Cdata:%5B51.6%2C40.6%2C25.3%2C56.5%2C42.7%2C71.1%2C30.6%2C5.1%2C7.0%2C30.2%2C12.4%2C26.4%2C53.6%2C8.2%2C23.0%2C19.9%2C28.5%2C26.6%2C68.5%2C66.8%5D%2CbackgroundColor:%27%237C3AED%27%7D%5D%7D%2Coptions:%7Btitle:%7Bdisplay:true%2Ctext:%27Compression%20Ratio%20%E2%80%94%20higher%20is%20better%27%7D%2Cscales:%7ByAxes:%5B%7BscaleLabel:%7Bdisplay:true%2ClabelString:%27Ratio%20%28raw%20%2F%20stored%2C%20x%29%27%7D%2Cticks:%7BbeginAtZero:true%7D%7D%5D%7D%2Cplugins:%7Bdatalabels:%7Bdisplay:true%2Canchor:%27end%27%2Calign:%27top%27%2Cfont:%7Bsize:8%7D%7D%7D%7D%7D)

## Per-Repository Details

### serde

**URL:** https://github.com/serde-rs/serde  
**Branch:** master  
**Commits:** 4,353 | **Files:** 880  
**Duration:** 12s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 8.6 MB | 5.6 MB | 6.6 MB | 3.9 MB |
| Compression ratio | 23.6x | 36.5x | 30.9x | 51.6x |
| vs git normal | - | - | 0.76x | 0.46x |
| vs git aggressive | - | - | 1.18x | 0.71x |

**PostgreSQL overhead:** 2.6 MB (40.1% of on-disk)

> **pgit wins** on actual data compression: 51.6x vs git aggressive 36.5x (41% better ratio)

---

### ripgrep

**URL:** https://github.com/BurntSushi/ripgrep  
**Branch:** master  
**Commits:** 2,208 | **Files:** 449  
**Duration:** 17s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 5.3 MB | 3.0 MB | 4.2 MB | 2.7 MB |
| Compression ratio | 20.9x | 36.8x | 26.5x | 40.6x |
| vs git normal | - | - | 0.79x | 0.52x |
| vs git aggressive | - | - | 1.39x | 0.90x |

**PostgreSQL overhead:** 1.5 MB (34.8% of on-disk)

> **pgit wins** on actual data compression: 40.6x vs git aggressive 36.8x (11% better ratio)

---

### tokio

**URL:** https://github.com/tokio-rs/tokio  
**Branch:** master  
**Commits:** 4,403 | **Files:** 2,403  
**Duration:** 19s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 15.7 MB | 8.3 MB | 11.2 MB | 7.7 MB |
| Compression ratio | 12.5x | 23.5x | 17.4x | 25.3x |
| vs git normal | - | - | 0.72x | 0.49x |
| vs git aggressive | - | - | 1.35x | 0.93x |

**PostgreSQL overhead:** 3.5 MB (31.2% of on-disk)

> **pgit wins** on actual data compression: 25.3x vs git aggressive 23.5x (8% better ratio)

---

### ruff

**URL:** https://github.com/astral-sh/ruff  
**Branch:** main  
**Commits:** 14,206 | **Files:** 25,124  
**Duration:** 113s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 106.3 MB | 51.3 MB | 67.4 MB | 51.4 MB |
| Compression ratio | 27.3x | 56.7x | 43.1x | 56.5x |
| vs git normal | - | - | 0.63x | 0.48x |
| vs git aggressive | - | - | 1.31x | 1.00x |

**PostgreSQL overhead:** 16.0 MB (23.7% of on-disk)

> **git aggressive wins** on compression: 56.7x vs pgit actual 56.5x (0% better ratio)

---

### cargo

**URL:** https://github.com/rust-lang/cargo  
**Branch:** master  
**Commits:** 21,850 | **Files:** 6,590  
**Duration:** 78s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 63.1 MB | 29.9 MB | 41.4 MB | 29.8 MB |
| Compression ratio | 20.1x | 42.5x | 30.7x | 42.7x |
| vs git normal | - | - | 0.66x | 0.47x |
| vs git aggressive | - | - | 1.39x | 1.00x |

**PostgreSQL overhead:** 11.6 MB (28.1% of on-disk)

> **pgit wins** on actual data compression: 42.7x vs git aggressive 42.5x (0% better ratio)

---

### fzf

**URL:** https://github.com/junegunn/fzf  
**Branch:** master  
**Commits:** 3,499 | **Files:** 184  
**Duration:** 11s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 7.7 MB | 3.5 MB | 4.6 MB | 3.0 MB |
| Compression ratio | 27.6x | 61.3x | 46.3x | 71.1x |
| vs git normal | - | - | 0.60x | 0.39x |
| vs git aggressive | - | - | 1.32x | 0.86x |

**PostgreSQL overhead:** 1.6 MB (34.9% of on-disk)

> **pgit wins** on actual data compression: 71.1x vs git aggressive 61.3x (16% better ratio)

---

### gin

**URL:** https://github.com/gin-gonic/gin  
**Branch:** master  
**Commits:** 1,967 | **Files:** 251  
**Duration:** 8s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 3.8 MB | 1.9 MB | 2.7 MB | 1.7 MB |
| Compression ratio | 13.6x | 26.8x | 18.9x | 30.6x |
| vs git normal | - | - | 0.72x | 0.44x |
| vs git aggressive | - | - | 1.42x | 0.88x |

**PostgreSQL overhead:** 1.0 MB (38.2% of on-disk)

> **pgit wins** on actual data compression: 30.6x vs git aggressive 26.8x (14% better ratio)

---

### hugo

**URL:** https://github.com/gohugoio/hugo  
**Branch:** master  
**Commits:** 9,538 | **Files:** 11,289  
**Duration:** 42s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 132.6 MB | 108.8 MB | 123.0 MB | 111.0 MB |
| Compression ratio | 4.3x | 5.2x | 4.6x | 5.1x |
| vs git normal | - | - | 0.93x | 0.84x |
| vs git aggressive | - | - | 1.13x | 1.02x |

**PostgreSQL overhead:** 12.0 MB (9.7% of on-disk)

> **git aggressive wins** on compression: 5.2x vs pgit actual 5.1x (2% better ratio)

---

### cli

**URL:** https://github.com/cli/cli  
**Branch:** trunk  
**Commits:** 10,820 | **Files:** 3,019  
**Duration:** 28s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 48.4 MB | 41.8 MB | 48.2 MB | 41.3 MB |
| Compression ratio | 6.0x | 6.9x | 6.0x | 7.0x |
| vs git normal | - | - | 1.00x | 0.85x |
| vs git aggressive | - | - | 1.15x | 0.99x |

**PostgreSQL overhead:** 6.9 MB (14.4% of on-disk)

> **pgit wins** on actual data compression: 7.0x vs git aggressive 6.9x (1% better ratio)

---

### flask

**URL:** https://github.com/pallets/flask  
**Branch:** main  
**Commits:** 5,516 | **Files:** 642  
**Duration:** 14s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 11.4 MB | 6.1 MB | 8.3 MB | 5.5 MB |
| Compression ratio | 14.7x | 27.6x | 20.1x | 30.2x |
| vs git normal | - | - | 0.73x | 0.49x |
| vs git aggressive | - | - | 1.38x | 0.92x |

**PostgreSQL overhead:** 2.8 MB (33.6% of on-disk)

> **pgit wins** on actual data compression: 30.2x vs git aggressive 27.6x (9% better ratio)

---

### requests

**URL:** https://github.com/psf/requests  
**Branch:** main  
**Commits:** 6,405 | **Files:** 460  
**Duration:** 13s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 12.5 MB | 9.3 MB | 11.7 MB | 9.1 MB |
| Compression ratio | 9.0x | 12.1x | 9.6x | 12.4x |
| vs git normal | - | - | 0.94x | 0.72x |
| vs git aggressive | - | - | 1.26x | 0.98x |

**PostgreSQL overhead:** 2.7 MB (22.7% of on-disk)

> **pgit wins** on actual data compression: 12.4x vs git aggressive 12.1x (2% better ratio)

---

### express

**URL:** https://github.com/expressjs/express  
**Branch:** master  
**Commits:** 6,128 | **Files:** 906  
**Duration:** 13s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 9.3 MB | 5.8 MB | 8.4 MB | 5.7 MB |
| Compression ratio | 16.2x | 25.7x | 17.9x | 26.4x |
| vs git normal | - | - | 0.90x | 0.61x |
| vs git aggressive | - | - | 1.44x | 0.97x |

**PostgreSQL overhead:** 2.7 MB (32.5% of on-disk)

> **pgit wins** on actual data compression: 26.4x vs git aggressive 25.7x (3% better ratio)

---

### core

**URL:** https://github.com/vuejs/core  
**Branch:** main  
**Commits:** 6,930 | **Files:** 1,342  
**Duration:** 33s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 31.0 MB | 11.6 MB | 15.1 MB | 11.2 MB |
| Compression ratio | 19.3x | 51.8x | 39.7x | 53.6x |
| vs git normal | - | - | 0.49x | 0.36x |
| vs git aggressive | - | - | 1.31x | 0.96x |

**PostgreSQL overhead:** 3.9 MB (26.1% of on-disk)

> **pgit wins** on actual data compression: 53.6x vs git aggressive 51.8x (4% better ratio)

---

### svelte

**URL:** https://github.com/sveltejs/svelte  
**Branch:** main  
**Commits:** 10,982 | **Files:** 29,819  
**Duration:** 96s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 111.8 MB | 96.5 MB | 111.5 MB | 96.0 MB |
| Compression ratio | 7.0x | 8.1x | 7.0x | 8.2x |
| vs git normal | - | - | 1.00x | 0.86x |
| vs git aggressive | - | - | 1.16x | 0.99x |

**PostgreSQL overhead:** 15.5 MB (13.9% of on-disk)

> **pgit wins** on actual data compression: 8.2x vs git aggressive 8.1x (1% better ratio)

---

### prettier

**URL:** https://github.com/prettier/prettier  
**Branch:** main  
**Commits:** 11,084 | **Files:** 30,913  
**Duration:** 76s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 162.3 MB | 66.1 MB | 106.5 MB | 91.1 MB |
| Compression ratio | 12.9x | 31.8x | 19.7x | 23.0x |
| vs git normal | - | - | 0.66x | 0.56x |
| vs git aggressive | - | - | 1.61x | 1.38x |

**PostgreSQL overhead:** 15.4 MB (14.4% of on-disk)

> **git aggressive wins** on compression: 31.8x vs pgit actual 23.0x (38% better ratio)

---

### react

**URL:** https://github.com/facebook/react  
**Branch:** main  
**Commits:** 21,378 | **Files:** 26,044  
**Duration:** 136s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 123.8 MB | 105.0 MB | 132.2 MB | 112.3 MB |
| Compression ratio | 18.0x | 21.3x | 16.9x | 19.9x |
| vs git normal | - | - | 1.07x | 0.91x |
| vs git aggressive | - | - | 1.26x | 1.07x |

**PostgreSQL overhead:** 19.9 MB (15.0% of on-disk)

> **git aggressive wins** on compression: 21.3x vs pgit actual 19.9x (7% better ratio)

---

### jq

**URL:** https://github.com/jqlang/jq  
**Branch:** master  
**Commits:** 1,871 | **Files:** 608  
**Duration:** 7s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 7.2 MB | 3.9 MB | 5.5 MB | 4.2 MB |
| Compression ratio | 16.9x | 31.3x | 21.8x | 28.5x |
| vs git normal | - | - | 0.77x | 0.59x |
| vs git aggressive | - | - | 1.43x | 1.10x |

**PostgreSQL overhead:** 1.3 MB (23.5% of on-disk)

> **git aggressive wins** on compression: 31.3x vs pgit actual 28.5x (10% better ratio)

---

### redis

**URL:** https://github.com/redis/redis  
**Branch:** unstable  
**Commits:** 12,940 | **Files:** 2,939  
**Duration:** 50s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 187.5 MB | 71.6 MB | 85.2 MB | 76.9 MB |
| Compression ratio | 10.9x | 28.6x | 24.0x | 26.6x |
| vs git normal | - | - | 0.45x | 0.41x |
| vs git aggressive | - | - | 1.19x | 1.07x |

**PostgreSQL overhead:** 8.2 MB (9.7% of on-disk)

> **git aggressive wins** on compression: 28.6x vs pgit actual 26.6x (7% better ratio)

---

### curl

**URL:** https://github.com/curl/curl  
**Branch:** master  
**Commits:** 37,860 | **Files:** 7,158  
**Duration:** 121s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 105.9 MB | 48.4 MB | 67.5 MB | 49.3 MB |
| Compression ratio | 31.9x | 69.7x | 50.0x | 68.5x |
| vs git normal | - | - | 0.64x | 0.47x |
| vs git aggressive | - | - | 1.39x | 1.02x |

**PostgreSQL overhead:** 18.3 MB (27.0% of on-disk)

> **git aggressive wins** on compression: 69.7x vs pgit actual 68.5x (2% better ratio)

---

### git

**URL:** https://github.com/git/git  
**Branch:** master  
**Commits:** 79,765 | **Files:** 7,291  
**Duration:** 465s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 275.8 MB | 90.6 MB | 143.1 MB | 111.3 MB |
| Compression ratio | 26.9x | 82.0x | 51.9x | 66.8x |
| vs git normal | - | - | 0.52x | 0.40x |
| vs git aggressive | - | - | 1.58x | 1.23x |

**PostgreSQL overhead:** 31.8 MB (22.2% of on-disk)

> **git aggressive wins** on compression: 82.0x vs pgit actual 66.8x (23% better ratio)

---

## PostgreSQL Overhead

| Repository | On-disk | Actual Data | Overhead | Overhead % |
|:-----------|--------:|------------:|---------:|-----------:|
| serde | 6.6 MB | 3.9 MB | 2.6 MB | 40.1% |
| ripgrep | 4.2 MB | 2.7 MB | 1.5 MB | 34.8% |
| tokio | 11.2 MB | 7.7 MB | 3.5 MB | 31.2% |
| ruff | 67.4 MB | 51.4 MB | 16.0 MB | 23.7% |
| cargo | 41.4 MB | 29.8 MB | 11.6 MB | 28.1% |
| fzf | 4.6 MB | 3.0 MB | 1.6 MB | 34.9% |
| gin | 2.7 MB | 1.7 MB | 1.0 MB | 38.2% |
| hugo | 123.0 MB | 111.0 MB | 12.0 MB | 9.7% |
| cli | 48.2 MB | 41.3 MB | 6.9 MB | 14.4% |
| flask | 8.3 MB | 5.5 MB | 2.8 MB | 33.6% |
| requests | 11.7 MB | 9.1 MB | 2.7 MB | 22.7% |
| express | 8.4 MB | 5.7 MB | 2.7 MB | 32.5% |
| core | 15.1 MB | 11.2 MB | 3.9 MB | 26.1% |
| svelte | 111.5 MB | 96.0 MB | 15.5 MB | 13.9% |
| prettier | 106.5 MB | 91.1 MB | 15.4 MB | 14.4% |
| react | 132.2 MB | 112.3 MB | 19.9 MB | 15.0% |
| jq | 5.5 MB | 4.2 MB | 1.3 MB | 23.5% |
| redis | 85.2 MB | 76.9 MB | 8.2 MB | 9.7% |
| curl | 67.5 MB | 49.3 MB | 18.3 MB | 27.0% |
| git | 143.1 MB | 111.3 MB | 31.8 MB | 22.2% |

Overhead ranges from 9.7% (redis) to 40.1% (serde).

PostgreSQL overhead includes: tuple headers (23 bytes/row), TOAST chunk metadata, page headers, alignment padding.

## Methodology

### Raw Uncompressed Size
- `git cat-file --batch-all-objects --batch-check='%(objecttype) %(objectsize)'` -- sum of all object sizes
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
- `raw_uncompressed / stored_size` -- same numerator for all methods
- Higher is better

---
*Generated by pgit-bench*
