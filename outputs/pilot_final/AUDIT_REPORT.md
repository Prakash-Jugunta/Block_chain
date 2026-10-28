# ForkRisk Milestone 2 — Final Audit Report & Sign-Off

**Date:** 2026-10-24  
**Go version:** go1.25.1 windows/amd64  
**Working directory:** e:\sem7\blockchain\casestudy

---

## Commands Executed and Results

### Build
```
cd e:\sem7\blockchain\casestudy
go build ./...
```
Output: (no output — zero errors)  
Exit code: 0

### Test suite
```
go test ./... -count=1
```
Output:
```
ok  	github.com/pranav-k/forkrisk/internal/config	0.051s
ok  	github.com/pranav-k/forkrisk/internal/engine	0.171s
ok  	github.com/pranav-k/forkrisk/internal/fork	0.035s
ok  	github.com/pranav-k/forkrisk/internal/logstore	0.047s
ok  	github.com/pranav-k/forkrisk/internal/metrics	0.042s
ok  	github.com/pranav-k/forkrisk/internal/node	0.045s
ok  	github.com/pranav-k/forkrisk/tests	0.129s
```
- **Top-level test functions (`func Test...`):** Exactly **109** defined across all packages.
- **Total test cases executed (including subtests):** Exactly **121** `=== RUN` cases executed.
- **Tests failed:** **0** (121 `--- PASS:`).

Package breakdown of top-level test functions:
- `internal/config`: 14
- `internal/engine`: 32
- `internal/fork`: 18
- `internal/logstore`: 2
- `internal/metrics`: 13
- `internal/node`: 16
- `tests`: 14
Total = 109.

### gofmt
```
gofmt -l .
```
Output: (no output — all Go source files formatted)  
Exit code: 0

---

## Files Changed

### 1. Fix block_capacity Semantics
- **`internal/engine/engine.go`**: Implemented intended semantics: `block_capacity: 0` means unlimited transactions per block (consumes the entire pending mempool `txPool`); `block_capacity > 0` enforces a strict ceiling of $\min(\text{txPool}, \text{block\_capacity})$. Excess transactions remain in `txPool` and are conserved. Removed silent substitution of `tx_per_block` when capacity is zero.
- **`internal/engine/engine_test.go`**: Added `TestTxPool_CapacityZeroConsumesAll` proving that capacity zero drains the pending pool while positive capacity (10) caps proposals and accumulates a backlog.

### 2. Preserve Explicit Zero Transaction-Arrival Rate
- **`internal/config/config.go`**: Added `TxArrivalRateExplicit bool` and custom `UnmarshalYAML` via `yaml.Node` inspection. An explicit YAML value of `tx_arrival_rate: 0` (or `0.0`) remains 0.0, while omitted values receive the default of 1.0. Validation rejects negative arrival rates.
- **`internal/config/config_test.go`**: Added `TestLoadFromFile_TxArrivalRateSemantics` testing omitted, explicit zero, and negative arrival rates.
- **`tests/simulate_test.go`**: Added `TestZeroArrivalRate_FromYAMLFile` executing a full simulation from a YAML file with `tx_arrival_rate: 0` and verifying 0 offered, 0 processed, and 0 pending transactions.

### 3. Strengthen Transaction-Arrival Tests
- **`internal/engine/engine_test.go`**:
  - `TestTxArrival_DecoupledFromBlockInterval`: Verifies exact equality of offered transaction count (504 offered) between 1.0s and 0.2s block intervals under identical seed.
  - `TestTxArrival_RateScalesOfferedLoad`: Verifies that block proposal timestamps are exactly identical between arrival rate 1.0 and 10.0, proving proposal schedules are invariant under load.

### 4. Documentation & Design Records
- **`docs/decisions.md`**: Added Decision D29 documenting block capacity zero semantics, explicit zero arrival rate handling, and test/provenance reconciliations.
- **`CASE_STUDY_DEMO_PLAN.md`**: Updated Section 6 with documented semantics for `block_capacity: 0` (unlimited) vs positive capacity, explicit zero arrival rate, and final pilot directory layout.
- **`README.md`**: Updated test counts (109 top-level, 121 executed), baseline seed 1 SHA-256 reproducibility hash (`b99da89e...`), pilot results table, and acceptance checklist.

---

## Pilot Verification & Directory Layout

### Directory Roles & Classification
Users and auditors can distinguish among three categories of run artifacts:
1. **Authoritative Final Pilot (`outputs/pilot_final/`):** Contains the official, verified Milestone 2 final pilot summary CSV (`pilot_summary.csv`), detailed report (`pilot_report.md`), and this audit report (`AUDIT_REPORT.md`). The corresponding 24 runs reside in `outputs/runs/`.
2. **Historical Pilot Evidence (`outputs/pilot/`):** Retains intermediate pilot runs and historical audit evidence from earlier milestones and calibration sweeps (`pilot_summary.csv`, `pilot_report.md`, `AUDIT_REPORT.md`, `historical_m2_pilot_*`).
3. **Incomplete Historical Runs (`outputs/runs/historical_incomplete/`):** Preserves 5 early prototype runs from 2026-10-21 (`baseline_seed7_*` and `two_node_test_seed1_*`) that lack matching `metadata.json` files because automated metadata generation had not yet been authored in `cmd/simulate`. Synthetic metadata was deliberately not fabricated to preserve evidence integrity.

### Final Pilot Evidence (`outputs/pilot_final/`)
- **Total runs:** 24 (8 scenarios × seeds {1, 2, 3})
- **CSV:** `outputs/pilot_final/pilot_summary.csv` — exactly 24 data rows + 1 header row (25 lines total), 26 columns in every row.
- **Transaction conservation:** Every run satisfies $\text{TotalOfferedTx} = \text{TotalProcessedTx} + \text{FinalPendingTx}$.
- **Baseline unlimited capacity:** Seed 1 offered 3,018 tx, processed 3,009 tx, leaving 9 pending tx (max queue depth 19).
- **Tx-load saturation (~100 offered tx/s, capacity 10):** Seed 1 offered 30,021 tx (~100 offered tx/s), processed 2,990 tx into blocks (capped at at most 10 tx/block, actual measured throughput ~9.66 raw tx/s), leaving a backlog of 27,031 pending transactions in the mempool.
- **Offered load decoupling:** Seed 1 offers exactly 3,018 tx across all stationary scenarios with arrival rate 1.0.
- **Reproducibility:** Baseline seed 1 executed twice under independent run IDs produced byte-identical SHA-256 (`b99da89e7645eead22cb7d8600075ee5b3367676d8b54dfa1c894f2a6168a226`):
  - Run 1: `outputs/runs/baseline_seed1_1792863044/events.jsonl`
  - Run 2: `outputs/runs/baseline_seed1_1792863159/events.jsonl`
- **Pre-transition replay fidelity:** Baseline seed 1 and dynamic_network seed 1 match byte-for-byte across exactly 4,609 consecutive events prior to the block 150 transition.

---

## Transaction-Load Experiment Scope and Documented Limitations

1. **Decoupled Arrival and Saturation Verification:**
   - Transaction arrivals are decoupled from block proposal frequency and follow a Poisson process ($\lambda = \text{tx\_arrival\_rate} \times \text{tx\_per\_block} = 10.0 \times 10 = 100\text{ offered tx/s}$ in `tx_load_stress`).
   - `block_capacity: 0` provides unlimited capacity (draining `txPool`), while positive capacity imposes a strict per-block ceiling (at most 10 transactions included per block).
   - `tx_load_stress` verifies queue saturation and persistent backlog (Seed 1: 30,021 offered, 2,990 processed with measured throughput ~9.66 raw tx/s, 27,031 transactions queued in the mempool). The configured maximum capacity is distinguished from the actual measured throughput.
2. **Propagation Delay Independence (Explicit Limitation):**
   - Transaction counts and block payloads are **not** currently coupled to network block payload size or message propagation delays.
   - The current results do **not** establish that higher transaction load itself causes greater fork risk through larger block propagation delays, because such a relationship is not currently modeled in the Gamma latency/propagation sampler.
   - This boundary is recorded as an intentional limitation of Milestone 2.

---

## Unresolved Issues

None. All Milestone 2 requirements, configuration semantics, test decoupling verifications, and pilot artifact validations have completed with 100% evidence.

---

## Final Archive

Archive name: `forkrisk_milestone2_final_audited.zip`  
Contains latest source, tests, scenario YAMLs, updated documentation, and both final (`outputs/pilot_final/`) and historical (`outputs/pilot/`) pilot artifacts.

