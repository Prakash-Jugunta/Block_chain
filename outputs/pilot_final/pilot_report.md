# ForkRisk — Milestone 2 Final Pilot Report

**Generated:** 2026-10-24  
**Go version:** go1.25.1 windows/amd64  
**Pilot:** 8 scenarios × seeds {1, 2, 3} = 24 runs  
**CSV:** `outputs/pilot_final/pilot_summary.csv`  
**Run artifacts:** `outputs/runs/<scenario>_seed<N>_<timestamp>/`  

---

## Reproducibility Check

Baseline seed=1 was executed twice under independent run identifiers with identical configuration and seed:
- **Run 1** (`baseline_seed1_1792863044`): SHA-256 = `b99da89e7645eead22cb7d8600075ee5b3367676d8b54dfa1c894f2a6168a226`  
- **Run 2** (`baseline_seed1_1792863159`): SHA-256 = `b99da89e7645eead22cb7d8600075ee5b3367676d8b54dfa1c894f2a6168a226`  
**Status: PASS — Byte-identical event logs verified via SHA-256 across two independent runs.**

---

## Pre-Transition Replay Fidelity Verification

To verify that the dynamic network experiment genuinely tests prediction on stationary pre-transition history, the event logs of `baseline` seed 1 and `dynamic_network` seed 1 were compared event-by-event up to the block 150 transition:
- **Prior events before transition:** Exactly 4,609 consecutive events match byte-for-byte with identical sequence numbers, timestamps, block hashes, offered counts, and transaction counts.
- **Transition point:** Block 150 triggered at sequence number 7,178 (`sim_time` = 149.5218 s) injecting `ConfigChange` events (`mean_latency_ms: 50 -> 300`, `latency_jitter_ms: 10 -> 30`).
- **Status: PASS — Exact pre-transition replay fidelity verified.**

---

## Final Pilot Results (24 Runs, Measured Values)

| Scenario | Seed | Fork Rate | Stale Rate | Reorg Count | Mean Depth | Node-View Consist. | Peer Avail. | Raw TPS | Goodput (tx/s) |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 1 | 0.010033 | 0.324415 | 3 | 1.0000 | 0.627759 | 0.992695 | 9.723744 | 6.556822 |
| baseline | 2 | 0.043046 | 0.642384 | 37 | 2.7027 | 0.382781 | 0.986759 | 9.819703 | 3.539699 |
| baseline | 3 | 0.016722 | 0.317726 | 9 | 1.0000 | 0.615385 | 0.988807 | 9.336579 | 6.385807 |
| latency_stress | 1 | 0.204013 | 0.454849 | 208 | 1.0192 | 0.527425 | 0.992695 | 9.723744 | 5.293284 |
| latency_stress | 2 | 0.152318 | 0.589404 | 167 | 1.6407 | 0.463907 | 0.986759 | 9.819703 | 3.904853 |
| latency_stress | 3 | 0.177258 | 0.404682 | 204 | 1.0147 | 0.530435 | 0.988807 | 9.336579 | 5.720753 |
| propagation_stress | 1 | 0.314381 | 0.575251 | 490 | 1.3980 | 0.460870 | 0.991861 | 9.544016 | 3.933061 |
| propagation_stress | 2 | 0.185430 | 0.735099 | 246 | 1.9837 | 0.368874 | 0.986579 | 9.050331 | 2.340655 |
| propagation_stress | 3 | 0.193980 | 0.652174 | 307 | 1.6515 | 0.340134 | 0.988807 | 9.336579 | 3.209045 |
| churn_stress | 1 | 0.050167 | 0.916388 | 1 | 1.0000 | 0.120401 | 0.514719 | 9.707871 | 0.761402 |
| churn_stress | 2 | 0.033113 | 0.903974 | 3 | 1.3333 | 0.123841 | 0.518465 | 9.627542 | 0.886959 |
| churn_stress | 3 | 0.060201 | 0.896321 | 0 | 0.0000 | 0.120401 | 0.518861 | 9.331984 | 0.871243 |
| block_rate_stress | 1 | 0.192795 | 0.627752 | 1046 | 1.3040 | 0.359173 | 0.992695 | 9.746365 | 3.464225 |
| block_rate_stress | 2 | 0.104458 | 0.855622 | 598 | 1.3662 | 0.221690 | 0.986759 | 9.813124 | 1.424433 |
| block_rate_stress | 3 | 0.162775 | 0.605070 | 883 | 1.3148 | 0.351501 | 0.988807 | 9.368863 | 3.748191 |
| tx_load_stress | 1 | 0.010033 | 0.324415 | 3 | 1.0000 | 0.627759 | 0.992695 | 9.662344 | 6.527738 |
| tx_load_stress | 2 | 0.043046 | 0.546358 | 32 | 4.5938 | 0.480795 | 0.986759 | 9.901945 | 4.473969 |
| tx_load_stress | 3 | 0.016722 | 0.317726 | 9 | 1.0000 | 0.615385 | 0.988807 | 9.627136 | 6.560141 |
| combined_stress | 1 | 0.105316 | 0.820461 | 525 | 2.2495 | 0.210431 | 0.991861 | 9.563047 | 1.652520 |
| combined_stress | 2 | 0.175299 | 0.778884 | 843 | 2.3013 | 0.263446 | 0.986579 | 9.044267 | 1.955599 |
| combined_stress | 3 | 0.123246 | 0.798597 | 613 | 2.9592 | 0.217034 | 0.988807 | 9.368863 | 1.891852 |
| dynamic_network | 1 | 0.053512 | 0.397993 | 42 | 1.0000 | 0.570569 | 0.992695 | 9.723744 | 5.842648 |
| dynamic_network | 2 | 0.039735 | 0.642384 | 35 | 2.7714 | 0.379139 | 0.986759 | 9.819703 | 3.539699 |
| dynamic_network | 3 | 0.066890 | 0.367893 | 55 | 1.0182 | 0.571906 | 0.988807 | 9.336579 | 5.836976 |

Complete metrics are in `outputs/pilot_final/pilot_summary.csv` (24 rows, 26 columns).

---

## Event Log SHA-256 and Transaction Conservation Provenance

Every run satisfies the strict transaction conservation invariant: `TotalOfferedTx = TotalProcessedTx + FinalPendingTx`.

| Run Identifier | Scenario | Seed | Offered Tx | Processed Tx | Pending Tx | Max Pending | Event Log SHA-256 |
|---|---|---|---|---|---|---|---|
| `baseline_seed1_1792863044` | baseline | 1 | 3018 | 3009 | 9 | 19 | `b99da89e7645eead22cb7d8600075ee5b3367676d8b54dfa1c894f2a6168a226` |
| `baseline_seed2_1792863045` | baseline | 2 | 2985 | 2985 | 0 | 21 | `12db9d6392c3aa4bebda7a5aca9b0ca8bc1567e9d7c0992b1cb48bdb17cf4cc2` |
| `baseline_seed3_1792863045` | baseline | 3 | 2905 | 2892 | 13 | 21 | `7e984940d5a6c829d6ebb701fb061564aa9ef91a0089db02a0c980552fc016cb` |
| `latency_stress_seed1_1792863045` | latency_stress | 1 | 3018 | 3009 | 9 | 19 | `3081836e55e299ff224a36fd2e47e05358c19171722d52e22900614e5a7061e2` |
| `latency_stress_seed2_1792863046` | latency_stress | 2 | 2985 | 2985 | 0 | 21 | `c2aecf79bf57864aabc370cfb04e57f3f7b973637d4baf7a0f5a03a1aa8f1cf4` |
| `latency_stress_seed3_1792863046` | latency_stress | 3 | 2905 | 2892 | 13 | 21 | `a9c3242905146f13b2e7ff893ab41f9379442e544eb3a6470fa768d2f66762c9` |
| `propagation_stress_seed1_1792863046` | propagation_stress | 1 | 3018 | 3009 | 9 | 19 | `e9d3bb4da1d84606a0d93d639215ce4b42c4a7b182f50775fec0f3e2f8acf6c9` |
| `propagation_stress_seed2_1792863047` | propagation_stress | 2 | 2985 | 2985 | 0 | 21 | `7babcd38de98f29e1d3830916f077083b6f53f89f9e95c3c9f31c1a9adc6ec52` |
| `propagation_stress_seed3_1792863047` | propagation_stress | 3 | 2905 | 2892 | 13 | 21 | `c77ba33024eec89d736a15425d98f861fe728f9e65f2b319a7adee0f3b1547db` |
| `churn_stress_seed1_1792863047` | churn_stress | 1 | 3018 | 3009 | 9 | 19 | `775c280069d1d332e31745e31f00618349d2696159ef9cf457d6cfb21aa10333` |
| `churn_stress_seed2_1792863048` | churn_stress | 2 | 2985 | 2985 | 0 | 21 | `c61b6378f7608a7ad575430e5bd436a89eb26af8cd267aca3c0ce575feff205c` |
| `churn_stress_seed3_1792863048` | churn_stress | 3 | 2905 | 2892 | 13 | 21 | `9337987a096d51ee620207a0fc7fdf3ecd95774a9ed4abce9c8364487de555d3` |
| `block_rate_stress_seed1_1792863048` | block_rate_stress | 1 | 3018 | 3016 | 2 | 8 | `a2a86aae9864c14dee59fcc2bf07b3a550beb4aeefbbf87444256d4c0f9251eb` |
| `block_rate_stress_seed2_1792863049` | block_rate_stress | 2 | 2985 | 2983 | 2 | 8 | `307f9f1cd22d52080259778bcd1b8ae7dec3f962698fb1b521e10d30848ef3e5` |
| `block_rate_stress_seed3_1792863050` | block_rate_stress | 3 | 2905 | 2902 | 3 | 8 | `63f4cc1fc31a6782119ee2a8a83445c6d5e1be46bc3869cf58551978b223a5e4` |
| `tx_load_stress_seed1_1792863050` | tx_load_stress | 1 | 30021 | 2990 | 27031 | 27031 | `1258a9b72e983864d5be4ac03a3073ddb4dd5c7a75f627b85b9e3659c8e5a50f` |
| `tx_load_stress_seed2_1792863051` | tx_load_stress | 2 | 29972 | 3010 | 26962 | 26967 | `279cc742e7b077aee708074e5df7d1e0198bf2d0b190f0f220db933f089ea38e` |
| `tx_load_stress_seed3_1792863051` | tx_load_stress | 3 | 30052 | 2982 | 27070 | 27070 | `21f81e5e27b1e50a0f573fd4c6d900d601bed1eec61bf70d1b1fca1310d629d1` |
| `combined_stress_seed1_1792863051` | combined_stress | 1 | 3018 | 3015 | 3 | 9 | `570401c93f72c5e02ec8928213e8666fd0c33035fed5e840aee604ed3dad43b4` |
| `combined_stress_seed2_1792863052` | combined_stress | 2 | 2985 | 2983 | 2 | 10 | `22d6f5fd3b9eea21bf11998c881e34f889af1126d2c0bcc789fc9de6cdd858d4` |
| `combined_stress_seed3_1792863053` | combined_stress | 3 | 2905 | 2902 | 3 | 9 | `41f172550309da479a7eb9c1278f30929c99bc60b2ae20f138abe809e9c8d342` |
| `dynamic_network_seed1_1792863053` | dynamic_network | 1 | 3018 | 3009 | 9 | 19 | `3cadd01a1530279403f407c09b4f722a19e3ba2306cccf25b03a0e577233d3c9` |
| `dynamic_network_seed2_1792863054` | dynamic_network | 2 | 2985 | 2985 | 0 | 21 | `a2a06d62ae87f49d9259ab8d98ec6dc97f661b7884e7a65cdd539e53c828df04` |
| `dynamic_network_seed3_1792863054` | dynamic_network | 3 | 2905 | 2892 | 13 | 21 | `d1e4a5d3ad95317f87e65a8543bb92d3bc75dcd537882e8922b97ffd82d6f5e6` |

---

## Key Observations & Semantics Verification

### 1. Corrected Block Capacity Semantics (`block_capacity: 0` vs `block_capacity: 10`)
The engine now implements the documented specification:
- `block_capacity: 0` means **unlimited transactions per block**. Proposed blocks consume the entire pending mempool (`txPool`), so no artificial cap is imposed.
- `block_capacity > 0` enforces a strict ceiling on transactions included per proposed block.
- **Baseline Verification:** In `baseline` (`block_capacity: 0`), blocks take all available pending transactions. For Seed 1, 3,018 transactions were offered, 3,009 were processed into blocks, and only 9 remained pending at cutoff (arriving after the last block proposal). The maximum queue depth was only 19 transactions.
- **Transaction-Load Saturation Verification:** In `tx_load_stress` (`block_capacity: 10`, `tx_arrival_rate: 10.0`, `tx_per_block: 10`), the offered transaction arrival rate increased tenfold to ~100 offered tx/s (30,021 offered transactions across the run for Seed 1). The configured processing capacity of at most 10 transactions per block capped block inclusion, yielding an actual measured processed throughput of ~9.66 raw tx/s (2,990 transactions processed into 299 blocks), leaving a substantial mempool backlog of **27,031 transactions** (`final_pending_tx: 27031`, `max_pending_tx: 27031`). This demonstrates genuine mempool queue saturation while confirming that transaction load does not distort consensus fork dynamics.

### 2. Decoupled Transaction-Arrival Rate
Transaction arrivals are generated independently of block proposals via a dedicated random stream:
- **Offered Load Invariance:** For identical seeds, the offered transaction count is strictly independent of block proposal frequency. For Seed 1, exactly **3,018 transactions** were offered across `baseline` ($T=1.0\text{ s}$), `block_rate_stress` ($T=0.2\text{ s}$, 1,499 blocks), `combined_stress` ($T=0.3\text{ s}$, 997 blocks), `latency_stress`, `propagation_stress`, `churn_stress`, and `dynamic_network`.
- **Proposal Schedule Invariance:** High transaction arrival rates (`tx_load_stress`, rate 10.0) produce the exact same block proposal timestamps as rate 1.0, proving that transaction load does not perturb consensus scheduling.

### 3. Explicit Zero Transaction-Arrival Rate
Configuration loading was corrected so that an explicit YAML entry of `tx_arrival_rate: 0` (or `0.0`) remains 0.0 rather than being overwritten by the default of 1.0. Unit and integration tests (`TestLoadFromFile_TxArrivalRateSemantics` and `TestZeroArrivalRate_FromYAMLFile`) confirm that zero arrival rate results in exactly 0 offered transactions, 0 processed transactions, and 0 pending transactions, while negative rates are rejected during validation.

### 4. Stress Dynamics Comparison
- **Baseline (churn enabled, ~99% uptime):** Fork rate 1.0–4.3%, stale rate 31.8–64.2%, reorg count 3–37.
- **Latency Stress (300ms latency):** Fork rate increases to 15.2–20.4%, reorg count increases to 167–208. Shared churn seed confirmed by identical peer availability (Seed 1: 0.992695, Seed 2: 0.986759, Seed 3: 0.988807).
- **Propagation Stress (1000ms delay):** Fork rate 18.5–31.4%, stale rate 57.5–73.5%, reorg count 246–490, mean confirmation latency ~17–22s.
- **Churn Stress (~50% uptime):** Fork rate modest (3.3–6.0%) but stale rate severe (89.6–91.6%). Node-view consistency drops to 0.120–0.124 as offline nodes miss blocks.
- **Block-Rate Stress (0.2s interval):** Fork rate 10.4–19.3%, reorg count 598–1046 across ~1500 blocks. Due to decoupled arrivals, each block carries ~2 tx, keeping offered load at ~10 tx/s.
- **Transaction-Load Stress (~100 offered tx/s, capacity 10):** Transaction arrivals exceed the configured processing capacity (at most 10 transactions per block), producing a substantial mempool backlog (27,031 pending transactions in Seed 1). Fork rate matches baseline (1.0–4.3%), confirming consensus decoupling.
- **Combined Stress:** Compounded stress (latency 300ms, prop delay 600ms, block interval 0.3s) yields fork rate 10.5–17.5%, stale rate 77.9–82.0%, reorg count 525–843.
- **Dynamic Network:** Stationary baseline behavior up to block 150 transition, followed by elevated fork rates under 300ms latency, yielding an intermediate aggregate fork rate of 4.0–6.7%.

### 5. Transaction-Load Modeling & Experimental Limitations
- **Arrival Decoupling:** Transaction arrivals are modeled via an independent Poisson process ($\lambda = \text{tx\_arrival\_rate} \times \text{tx\_per\_block} = 10.0 \times 10 = 100\text{ offered tx/s}$) decoupled from block-production frequency.
- **Capacity Enforcement vs Processed Throughput:** `block_capacity: 0` provides unlimited capacity (draining the mempool), while `block_capacity > 0` enforces a per-block ceiling of at most 10 transactions included per block. The actual measured processed throughput (~9.66 raw tx/s in Seed 1) is determined by simulation execution rather than equaling the nominal capacity.
- **Queue Saturation:** In `tx_load_stress`, the ~100 offered tx/s arrival rate exceeds the per-block processing capacity, producing a persistent backlog of 27,031 unserviced transactions in the mempool (Seed 1: 30,021 offered, 2,990 processed, 27,031 final pending; conservation: $30,021 = 2,990 + 27,031$).
- **Causality Limitation (Explicit Requirement):** In the current simulation engine, transaction counts are **not** coupled to network block payload size or message transmission/propagation delays. Therefore, the experiment demonstrates queue saturation and mempool backlog, but does **not** establish that higher transaction arrival rate causes greater fork risk through enlarged block transmission delay. This is an explicit design boundary of Milestone 2.

---

## Dynamic Event Audit (`dynamic_network_seed1_1792863053`)

From `outputs/runs/dynamic_network_seed1_1792863053/events.jsonl`:
- Line 4610: `{"seq":7178,"sim_time":149.52176442912224,"event_type":"ConfigChange","node_id":"","config_field":"mean_latency_ms","config_old_value":"50.0000","config_new_value":"300.0000","at_block":150}`
- Line 4611: `{"seq":7179,"sim_time":149.52176442912224,"event_type":"ConfigChange","node_id":"","config_field":"latency_jitter_ms","config_old_value":"10.0000","config_new_value":"30.0000","at_block":150}`

From `outputs/runs/dynamic_network_seed1_1792863053/metadata.json`:
- `config.mean_latency_ms: 50` (initial pre-transition value recorded)
- `config.dynamic_events[0].at_block: 150`
- `config.dynamic_events[0].mean_latency_ms: 300`
- `config.dynamic_events[0].latency_jitter_ms: 30`
- `total_offered_tx: 3018`
- `total_processed_tx: 3009`
- `final_pending_tx: 9` (Invariant: $3018 = 3009 + 9$ satisfied)

---

## CSV Validation

- **Rows:** 24 data rows + 1 header row = 25 lines total
- **Columns per row:** Exactly 26 columns in all rows
- **Units:** Times in seconds, rates as fractions (0.0–1.0), TPS/Goodput as transactions per second, latency/delay in milliseconds
- **Run IDs:** Unique deterministic format `<scenario>_seed<seed>_<timestamp>`
- **Seeds:** Exactly seeds 1, 2, and 3 across all 8 scenarios
- **Initial config fidelity:** `dynamic_network` rows record `mean_latency_ms=50.00` (initial, not post-shift 300ms)

---

## Archival and Directory Distinction

Users and auditors can distinguish among three categories of run artifacts:
1. **Authoritative Final Pilot (`outputs/pilot_final/`):** Contains the official, verified Milestone 2 final pilot summary CSV (`pilot_summary.csv`), detailed report (`pilot_report.md`), and final audit report (`AUDIT_REPORT.md`). The corresponding 24 runs reside in `outputs/runs/`.
2. **Historical Pilot Evidence (`outputs/pilot/`):** Retains intermediate pilot runs and historical audit evidence from earlier milestones and calibration sweeps (`pilot_summary.csv`, `pilot_report.md`, `AUDIT_REPORT.md`, `historical_m2_pilot_*`).
3. **Incomplete Historical Runs (`outputs/runs/historical_incomplete/`):** Preserves 5 early prototype runs from 2026-10-21 (`baseline_seed7_*` and `two_node_test_seed1_*`) that lack matching `metadata.json` files because automated metadata generation had not yet been authored in `cmd/simulate`. Synthetic metadata was deliberately not fabricated to preserve evidence integrity.
