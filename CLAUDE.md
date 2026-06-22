# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a fork of [pingcap/go-ycsb](https://github.com/pingcap/go-ycsb) — a Go port of the Yahoo! Cloud Serving Benchmark (YCSB). The fork is heavily customized for **LSM-tree write amplification research**, focusing on BadgerDB v4 benchmarking with novel workload distribution models (MixGraph prefix-hotspot model from the RocksDB FAST '20 paper).

Module: `github.com/pingcap/go-ycsb`

## Build & Test

```bash
# Build the CLI binary
make build
# Output: ./bin/go-ycsb

# Run tests (limited — only pkg/util and pkg/workload have test files)
go test ./pkg/...
```

## Running Benchmarks

```bash
# Load phase: populate the database
./bin/go-ycsb load badger -P workloads/workloada -P zzl_badger.properties \
  -p recordcount=1000000 -p threadcount=16

# Run phase: execute the workload against the loaded data
./bin/go-ycsb run badger -P workloads/workloada -P zzl_badger.properties \
  -p recordcount=1000000 -p operationcount=1000000 \
  -p requestdistribution=zipfian -p threadcount=16

# Interactive shell for manual DB operations
./bin/go-ycsb shell basic
```

### Key parameters (passed with `-p`)

| Parameter | Description |
|-----------|-------------|
| `recordcount` | Total records to load |
| `operationcount` | Total operations to run |
| `threadcount` | Concurrent goroutines |
| `requestdistribution` | Key access pattern: `uniform`, `zipfian`, `latest`, `hotspot`, `exponential`, `mixgraph` |
| `insertorder` | `hashed` (default, maximum LSM stress) or `ordered` |
| `fieldlength` / `fieldcount` | Value size/shape configuration |
| `readproportion` / `updateproportion` / `insertproportion` / `scanproportion` | Workload mix ratios |
| `target` | Rate-limit to a specific QPS |
| `maxexecutiontime` | Max run duration in seconds |

## Architecture

### Registration Pattern (plugin system)

The codebase uses Go `init()` functions for automatic plugin registration. Every DB backend and workload registers itself at init time:

- **DB backends** (`db/<name>/`): Call `ycsb.RegisterDBCreator("name", creator)` in `init()`
- **Workloads** (`pkg/workload/`): Call `ycsb.RegisterWorkloadCreator("name", creator)` in `init()`
- All backends are imported via blank imports in `cmd/go-ycsb/main.go`

### Core Interfaces (`pkg/ycsb/`)

- **`ycsb.DB`** — Database operations: `Read`, `Scan`, `Update`, `Insert`, `Delete`, plus thread lifecycle (`InitThread`/`CleanupThread`)
- **`ycsb.BatchDB`** — Optional batch variants: `BatchInsert`, `BatchRead`, `BatchUpdate`, `BatchDelete`
- **`ycsb.Workload`** — Benchmark logic: `DoTransaction`, `DoInsert`, `DoBatchTransaction`, `DoBatchInsert`, plus thread lifecycle
- **`ycsb.Generator`** — Number sequence generator: `Next(r *rand.Rand) int64`, `Last() int64`
- **`ycsb.Measurer`** — Latency measurement interface

### Data Flow

1. `cmd/go-ycsb/main.go` parses CLI args, creates DB + Workload via registered creators
2. Wraps DB with `client.DbWrapper` (decorator pattern) which times each operation and feeds latency measurements
3. `client.Client.Run()` spawns `threadcount` goroutines, each calling `workload.DoTransaction()` or `workload.DoInsert()` in a loop
4. `DoTransaction` → selects key (via keyChooser generator) → selects operation type (via operationChooser or MixGraph's per-key operation chooser) → calls the corresponding DB method

### Key Customizations from Upstream

#### MixGraph Generator (`pkg/generator/mixgraph.go`)

The most significant addition. Implements a prefix-localized hotspot distribution:
- Partitions the key space into `numRegions` prefixes with bimodal exponential decay weights (hot prefixes get most traffic via `expA`/`expB`, cold prefixes get flat low traffic via `expC`/`expD`)
- Shuffles physical prefix mapping (fixed seed for reproducibility)
- Within each prefix region, a power-law distribution (`keyDistA`/`keyDistB`) controls key-level skew
- Each region has its own per-key operation chooser (read/update ratios vary by region hotness)

#### Write Amplification Tracking (`db/badger/db.go`)

- `GlobalLogicalWriteBytes` (atomic int64): Tracks total logical bytes written (key + value size)
- At `Close()`, reads `/proc/self/io` to get OS-level physical write bytes
- Computes and prints system-level write amplification (physical / logical)

#### GPD Value Generator (`pkg/generator/gpd_generator.go`)

Generates variable-length values following a Generalized Pareto Distribution when `mixgraph.gpd_value=true`.

#### Modified Operation Ordering (`pkg/workload/core.go`)

When using MixGraph: key is selected first, then the operation type is determined by the key's region. This ensures hot keys get the correct operation mix for their region.

## Adding a New DB Backend

1. Create a directory `db/<name>/`
2. Implement a `DBCreator` and a `DB` (implementing `ycsb.DB`)
3. Register via `ycsb.RegisterDBCreator("name", creator)` in an `init()` function
4. Add a blank import in `cmd/go-ycsb/main.go`

## A/B Testing Workflow

The project compares a "normal" Badger against a modified "heatLSM" Badger:

1. Two local copies of Badger exist at `/home/hanjiang/DB-CODE/nomalBadger` and `/home/hanjiang/DB-CODE/priZzlBadger`
2. `go mod edit -replace github.com/dgraph-io/badger/v4=<path>` swaps between versions
3. `make build` recompiles with the replacement
4. `zzlTestBash/remoteBadgerClearAndTest.sh` automates: compile → scp binary+configs to remote test machine → run load+run phases → collect logs

**Important**: Because go-ycsb statically links DB libraries, any change to the Badger code requires a full `make build`.

## Test Result Logs

- Local logs: `result_<Version>_T<ThreadCount>_Round<N>.log`
- Logs contain per-operation latency histograms (READ, UPDATE, INSERT, SCAN, TOTAL, plus ERROR variants)
- Also contain LSM compaction stats and write amplification data
- `zzlTestResult.md` explains how to interpret the output format
