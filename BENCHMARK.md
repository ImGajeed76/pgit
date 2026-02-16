# pgit-bench Compression Report

**Date:** 2026-02-16 02:52:59
**Repositories:** 19

## Summary

| Repository | Commits | Raw Size | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual) | PG Overhead | Best Ratio | Duration |
|:-----------|--------:|---------:|-------------:|-----------------:|---------------:|--------------:|------------:|-----------:|---------:|
| serde | 4,352 | 203.5 MB | 8.6 MB | 5.6 MB | 6.0 MB | 3.9 MB | 2.2 MB (36%) | 52.5x (pgit) | 41s |
| ripgrep | 2,207 | 111.8 MB | 5.3 MB | 3.0 MB | 4.1 MB | 2.9 MB | 1.2 MB (29%) | 38.6x (pgit) | 65s |
| tokio | 4,394 | 195.6 MB | 15.6 MB | 8.3 MB | 10.6 MB | 7.7 MB | 2.9 MB (27%) | 25.4x (pgit) | 44s |
| ruff | 14,116 | 96.5 GB | 101.8 MB | 51.0 MB | 72.8 MB | 56.6 MB | 16.2 MB (22%) | 1939.8x (git) | 789s |
| cargo | 21,833 | 573.8 MB | 63.5 MB | 29.8 MB | 43.9 MB | 30.3 MB | 13.6 MB (31%) | 19.2x (git) | 773s |
| fzf | 3,482 | 209.7 MB | 7.6 MB | 3.4 MB | 4.1 MB | 2.7 MB | 1.4 MB (34%) | 78.1x (pgit) | 37s |
| gin | 1,961 | 51.7 MB | 3.8 MB | 1.9 MB | 2.6 MB | 1.6 MB | 1012.4 KB (39%) | 32.8x (pgit) | 22s |
| hugo | 9,520 | 1.7 MB | 133.1 MB | 108.8 MB | 240.4 MB | 222.9 MB | 17.5 MB (7%) | 0.0x (git) | 222s |
| cli | 10,776 | 4.8 GB | 48.2 MB | 41.8 MB | 49.8 MB | 41.8 MB | 7.9 MB (16%) | 118.1x (git) | 335s |
| flask | 5,506 | 165.7 MB | 11.4 MB | 6.0 MB | 8.1 MB | 5.5 MB | 2.6 MB (32%) | 30.2x (pgit) | 253s |
| requests | 6,405 | 60.3 MB | 12.4 MB | 9.3 MB | 12.0 MB | 9.5 MB | 2.4 MB (20%) | 6.5x (git) | 50s |
| express | 6,128 | 1.1 TB | 9.3 MB | 5.8 MB | 7.6 MB | 5.2 MB | 2.4 MB (31%) | 218411.6x (pgit) | 440s |
| core | 6,930 | 599.2 MB | 30.4 MB | 11.6 MB | 13.5 MB | 9.9 MB | 3.5 MB (26%) | 60.3x (pgit) | 192s |
| svelte | 10,948 | 415.6 MB | 112.2 MB | 96.4 MB | 120.4 MB | 102.6 MB | 17.8 MB (15%) | 4.3x (git) | 208s |
| prettier | 11,084 | 12.6 MB | 162.3 MB | 66.2 MB | 113.4 MB | 96.4 MB | 17.1 MB (15%) | 0.2x (git) | 1975s |
| react | 21,368 | 345.0 MB | 123.1 MB | 104.9 MB | 142.8 MB | 121.4 MB | 21.3 MB (15%) | 3.3x (git) | 1187s |
| jq | 1,871 | 50.5 GB | 7.2 MB | 3.9 MB | 6.4 MB | 5.1 MB | 1.3 MB (20%) | 13364.8x (git) | 44s |
| redis | 12,936 | 105.7 GB | 187.0 MB | 71.6 MB | 85.0 MB | 76.9 MB | 8.1 MB (9%) | 1511.6x (git) | 743s |
| curl | 37,818 | 18.2 TB | 105.8 MB | 48.4 MB | 62.1 MB | 45.0 MB | 17.1 MB (28%) | 424199.8x (pgit) | 866s |

## Compression: git aggressive vs pgit

### Stored Size

![Stored Size Comparison](https://quickchart.io/chart?w=600&h=300&c=%7Btype:%27bar%27%2Cdata:%7Blabels:%5B%27serde%27%2C%27ripgrep%27%2C%27tokio%27%2C%27ruff%27%2C%27cargo%27%2C%27fzf%27%2C%27gin%27%2C%27hugo%27%2C%27cli%27%2C%27flask%27%2C%27requests%27%2C%27express%27%2C%27core%27%2C%27svelte%27%2C%27prettier%27%2C%27react%27%2C%27jq%27%2C%27redis%27%2C%27curl%27%5D%2Cdatasets:%5B%7Blabel:%27git%20aggressive%27%2Cdata:%5B5.6%2C3.0%2C8.3%2C51.0%2C29.8%2C3.4%2C1.9%2C108.8%2C41.8%2C6.0%2C9.3%2C5.8%2C11.6%2C96.4%2C66.2%2C104.9%2C3.9%2C71.6%2C48.4%5D%2CbackgroundColor:%27%25233B82F6%27%7D%2C%7Blabel:%27pgit%20actual%20data%27%2Cdata:%5B3.9%2C2.9%2C7.7%2C56.6%2C30.3%2C2.7%2C1.6%2C222.9%2C41.8%2C5.5%2C9.5%2C5.2%2C9.9%2C102.6%2C96.4%2C121.4%2C5.1%2C76.9%2C45.0%5D%2CbackgroundColor:%27%25237C3AED%27%7D%5D%7D%2Coptions:%7Btitle:%7Bdisplay:true%2Ctext:%27Stored%20Size%20%28MB%29%20%E2%80%94%20lower%20is%20better%27%7D%2Cplugins:%7Bdatalabels:%7Bdisplay:true%2Canchor:%27end%27%2Calign:%27top%27%7D%7D%7D%7D)

### Compression Ratio

Higher is better (raw uncompressed / stored size).

![Compression Ratio Comparison](https://quickchart.io/chart?w=600&h=300&c=%7Btype:%27bar%27%2Cdata:%7Blabels:%5B%27serde%27%2C%27ripgrep%27%2C%27tokio%27%2C%27ruff%27%2C%27cargo%27%2C%27fzf%27%2C%27gin%27%2C%27hugo%27%2C%27cli%27%2C%27flask%27%2C%27requests%27%2C%27express%27%2C%27core%27%2C%27svelte%27%2C%27prettier%27%2C%27react%27%2C%27jq%27%2C%27redis%27%2C%27curl%27%5D%2Cdatasets:%5B%7Blabel:%27git%20aggressive%27%2Cdata:%5B36.6%2C36.8%2C23.6%2C1939.8%2C19.2%2C61.0%2C27.0%2C0.0%2C118.1%2C27.5%2C6.5%2C195714.6%2C51.8%2C4.3%2C0.2%2C3.3%2C13364.8%2C1511.6%2C395080.0%5D%2CbackgroundColor:%27%25233B82F6%27%7D%2C%7Blabel:%27pgit%20actual%20data%27%2Cdata:%5B52.5%2C38.6%2C25.4%2C1745.8%2C18.9%2C78.1%2C32.8%2C0.0%2C117.9%2C30.2%2C6.3%2C218411.6%2C60.3%2C4.0%2C0.1%2C2.8%2C10181.9%2C1407.0%2C424199.8%5D%2CbackgroundColor:%27%25237C3AED%27%7D%5D%7D%2Coptions:%7Btitle:%7Bdisplay:true%2Ctext:%27Compression%20Ratio%20%E2%80%94%20higher%20is%20better%27%7D%2Cplugins:%7Bdatalabels:%7Bdisplay:true%2Canchor:%27end%27%2Calign:%27top%27%7D%7D%7D%7D)

## Per-Repository Details

### serde

**URL:** https://github.com/serde-rs/serde  
**Branch:** master  
**Commits:** 4,352 | **Files:** 880  
**Duration:** 41s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 8.6 MB | 5.6 MB | 6.0 MB | 3.9 MB |
| Compression ratio | 23.6x | 36.6x | 33.7x | 52.5x |
| vs git normal | - | - | 0.70x | 0.45x |
| vs git aggressive | - | - | 1.09x | 0.70x |

**PostgreSQL overhead:** 2.2 MB (35.8% of on-disk)

> **pgit wins** on actual data compression: 52.5x vs git aggressive 36.6x (43% better ratio)

---

### ripgrep

**URL:** https://github.com/BurntSushi/ripgrep  
**Branch:** master  
**Commits:** 2,207 | **Files:** 449  
**Duration:** 65s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 5.3 MB | 3.0 MB | 4.1 MB | 2.9 MB |
| Compression ratio | 21.0x | 36.8x | 27.5x | 38.6x |
| vs git normal | - | - | 0.77x | 0.54x |
| vs git aggressive | - | - | 1.34x | 0.95x |

**PostgreSQL overhead:** 1.2 MB (28.9% of on-disk)

> **pgit wins** on actual data compression: 38.6x vs git aggressive 36.8x (5% better ratio)

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

> **pgit wins** on actual data compression: 25.4x vs git aggressive 23.6x (8% better ratio)

---

### ruff

**URL:** https://github.com/astral-sh/ruff  
**Branch:** main  
**Commits:** 14,116 | **Files:** 25,081  
**Duration:** 789s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 101.8 MB | 51.0 MB | 72.8 MB | 56.6 MB |
| Compression ratio | 971.1x | 1939.8x | 1357.2x | 1745.8x |
| vs git normal | - | - | 0.72x | 0.56x |
| vs git aggressive | - | - | 1.43x | 1.11x |

**PostgreSQL overhead:** 16.2 MB (22.3% of on-disk)

> **git aggressive wins** on compression: 1939.8x vs pgit actual 1745.8x (11% better ratio)

---

### cargo

**URL:** https://github.com/rust-lang/cargo  
**Branch:** master  
**Commits:** 21,833 | **Files:** 6,589  
**Duration:** 773s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 63.5 MB | 29.8 MB | 43.9 MB | 30.3 MB |
| Compression ratio | 9.0x | 19.2x | 13.1x | 18.9x |
| vs git normal | - | - | 0.69x | 0.48x |
| vs git aggressive | - | - | 1.47x | 1.02x |

**PostgreSQL overhead:** 13.6 MB (31.1% of on-disk)

> **git aggressive wins** on compression: 19.2x vs pgit actual 18.9x (2% better ratio)

---

### fzf

**URL:** https://github.com/junegunn/fzf  
**Branch:** master  
**Commits:** 3,482 | **Files:** 184  
**Duration:** 37s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 7.6 MB | 3.4 MB | 4.1 MB | 2.7 MB |
| Compression ratio | 27.6x | 61.0x | 51.7x | 78.1x |
| vs git normal | - | - | 0.53x | 0.35x |
| vs git aggressive | - | - | 1.18x | 0.78x |

**PostgreSQL overhead:** 1.4 MB (33.8% of on-disk)

> **pgit wins** on actual data compression: 78.1x vs git aggressive 61.0x (28% better ratio)

---

### gin

**URL:** https://github.com/gin-gonic/gin  
**Branch:** master  
**Commits:** 1,961 | **Files:** 251  
**Duration:** 22s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 3.8 MB | 1.9 MB | 2.6 MB | 1.6 MB |
| Compression ratio | 13.6x | 27.0x | 20.2x | 32.8x |
| vs git normal | - | - | 0.67x | 0.41x |
| vs git aggressive | - | - | 1.34x | 0.82x |

**PostgreSQL overhead:** 1012.4 KB (38.6% of on-disk)

> **pgit wins** on actual data compression: 32.8x vs git aggressive 27.0x (22% better ratio)

---

### hugo

**URL:** https://github.com/gohugoio/hugo  
**Branch:** master  
**Commits:** 9,520 | **Files:** 11,284  
**Duration:** 222s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 133.1 MB | 108.8 MB | 240.4 MB | 222.9 MB |
| Compression ratio | 0.0x | 0.0x | 0.0x | 0.0x |
| vs git normal | - | - | 1.81x | 1.67x |
| vs git aggressive | - | - | 2.21x | 2.05x |

**PostgreSQL overhead:** 17.5 MB (7.3% of on-disk)

> **git aggressive wins** on compression: 0.0x vs pgit actual 0.0x (105% better ratio)

---

### cli

**URL:** https://github.com/cli/cli  
**Branch:** trunk  
**Commits:** 10,776 | **Files:** 3,010  
**Duration:** 335s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 48.2 MB | 41.8 MB | 49.8 MB | 41.8 MB |
| Compression ratio | 102.3x | 118.1x | 99.1x | 117.9x |
| vs git normal | - | - | 1.03x | 0.87x |
| vs git aggressive | - | - | 1.19x | 1.00x |

**PostgreSQL overhead:** 7.9 MB (15.9% of on-disk)

> **git aggressive wins** on compression: 118.1x vs pgit actual 117.9x (0% better ratio)

---

### flask

**URL:** https://github.com/pallets/flask  
**Branch:** main  
**Commits:** 5,506 | **Files:** 642  
**Duration:** 253s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 11.4 MB | 6.0 MB | 8.1 MB | 5.5 MB |
| Compression ratio | 14.5x | 27.5x | 20.5x | 30.2x |
| vs git normal | - | - | 0.70x | 0.48x |
| vs git aggressive | - | - | 1.34x | 0.91x |

**PostgreSQL overhead:** 2.6 MB (32.0% of on-disk)

> **pgit wins** on actual data compression: 30.2x vs git aggressive 27.5x (10% better ratio)

---

### requests

**URL:** https://github.com/psf/requests  
**Branch:** main  
**Commits:** 6,405 | **Files:** 460  
**Duration:** 50s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 12.4 MB | 9.3 MB | 12.0 MB | 9.5 MB |
| Compression ratio | 4.9x | 6.5x | 5.0x | 6.3x |
| vs git normal | - | - | 0.96x | 0.77x |
| vs git aggressive | - | - | 1.29x | 1.03x |

**PostgreSQL overhead:** 2.4 MB (20.3% of on-disk)

> **git aggressive wins** on compression: 6.5x vs pgit actual 6.3x (3% better ratio)

---

### express

**URL:** https://github.com/expressjs/express  
**Branch:** master  
**Commits:** 6,128 | **Files:** 906  
**Duration:** 440s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 9.3 MB | 5.8 MB | 7.6 MB | 5.2 MB |
| Compression ratio | 123319.6x | 195714.6x | 150751.9x | 218411.6x |
| vs git normal | - | - | 0.82x | 0.56x |
| vs git aggressive | - | - | 1.30x | 0.90x |

**PostgreSQL overhead:** 2.4 MB (31.0% of on-disk)

> **pgit wins** on actual data compression: 218411.6x vs git aggressive 195714.6x (12% better ratio)

---

### core

**URL:** https://github.com/vuejs/core  
**Branch:** main  
**Commits:** 6,930 | **Files:** 1,342  
**Duration:** 192s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 30.4 MB | 11.6 MB | 13.5 MB | 9.9 MB |
| Compression ratio | 19.7x | 51.8x | 44.5x | 60.3x |
| vs git normal | - | - | 0.44x | 0.33x |
| vs git aggressive | - | - | 1.16x | 0.86x |

**PostgreSQL overhead:** 3.5 MB (26.2% of on-disk)

> **pgit wins** on actual data compression: 60.3x vs git aggressive 51.8x (16% better ratio)

---

### svelte

**URL:** https://github.com/sveltejs/svelte  
**Branch:** main  
**Commits:** 10,948 | **Files:** 29,746  
**Duration:** 208s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 112.2 MB | 96.4 MB | 120.4 MB | 102.6 MB |
| Compression ratio | 3.7x | 4.3x | 3.5x | 4.0x |
| vs git normal | - | - | 1.07x | 0.91x |
| vs git aggressive | - | - | 1.25x | 1.06x |

**PostgreSQL overhead:** 17.8 MB (14.8% of on-disk)

> **git aggressive wins** on compression: 4.3x vs pgit actual 4.0x (6% better ratio)

---

### prettier

**URL:** https://github.com/prettier/prettier  
**Branch:** main  
**Commits:** 11,084 | **Files:** 30,913  
**Duration:** 1975s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 162.3 MB | 66.2 MB | 113.4 MB | 96.4 MB |
| Compression ratio | 0.1x | 0.2x | 0.1x | 0.1x |
| vs git normal | - | - | 0.70x | 0.59x |
| vs git aggressive | - | - | 1.71x | 1.46x |

**PostgreSQL overhead:** 17.1 MB (15.1% of on-disk)

> **git aggressive wins** on compression: 0.2x vs pgit actual 0.1x (46% better ratio)

---

### react

**URL:** https://github.com/facebook/react  
**Branch:** main  
**Commits:** 21,368 | **Files:** 26,042  
**Duration:** 1187s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 123.1 MB | 104.9 MB | 142.8 MB | 121.4 MB |
| Compression ratio | 2.8x | 3.3x | 2.4x | 2.8x |
| vs git normal | - | - | 1.16x | 0.99x |
| vs git aggressive | - | - | 1.36x | 1.16x |

**PostgreSQL overhead:** 21.3 MB (14.9% of on-disk)

> **git aggressive wins** on compression: 3.3x vs pgit actual 2.8x (16% better ratio)

---

### jq

**URL:** https://github.com/jqlang/jq  
**Branch:** master  
**Commits:** 1,871 | **Files:** 607  
**Duration:** 44s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 7.2 MB | 3.9 MB | 6.4 MB | 5.1 MB |
| Compression ratio | 7214.1x | 13364.8x | 8127.8x | 10181.9x |
| vs git normal | - | - | 0.89x | 0.71x |
| vs git aggressive | - | - | 1.64x | 1.31x |

**PostgreSQL overhead:** 1.3 MB (20.2% of on-disk)

> **git aggressive wins** on compression: 13364.8x vs pgit actual 10181.9x (31% better ratio)

---

### redis

**URL:** https://github.com/redis/redis  
**Branch:** unstable  
**Commits:** 12,936 | **Files:** 2,939  
**Duration:** 743s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 187.0 MB | 71.6 MB | 85.0 MB | 76.9 MB |
| Compression ratio | 578.7x | 1511.6x | 1273.3x | 1407.0x |
| vs git normal | - | - | 0.45x | 0.41x |
| vs git aggressive | - | - | 1.19x | 1.07x |

**PostgreSQL overhead:** 8.1 MB (9.5% of on-disk)

> **git aggressive wins** on compression: 1511.6x vs pgit actual 1407.0x (7% better ratio)

---

### curl

**URL:** https://github.com/curl/curl  
**Branch:** master  
**Commits:** 37,818 | **Files:** 7,157  
**Duration:** 866s  

| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |
|:-------|-------------:|-----------------:|---------------:|-------------------:|
| Stored size | 105.8 MB | 48.4 MB | 62.1 MB | 45.0 MB |
| Compression ratio | 180475.8x | 395080.0x | 307447.9x | 424199.8x |
| vs git normal | - | - | 0.59x | 0.43x |
| vs git aggressive | - | - | 1.29x | 0.93x |

**PostgreSQL overhead:** 17.1 MB (27.5% of on-disk)

> **pgit wins** on actual data compression: 424199.8x vs git aggressive 395080.0x (7% better ratio)

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
| gin | 2.6 MB | 1.6 MB | 1012.4 KB | 38.6% |
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
- `git cat-file --batch-all-objects --batch='%(objecttype) %(objectsize)'` -- sum of all object sizes
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
