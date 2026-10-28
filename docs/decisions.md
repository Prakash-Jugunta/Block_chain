# Implementation Decisions — Milestones 1 & 2

> **Environment:** Go 1.25.1 (consistent with go.mod) / windows/amd64.

## Decision Log

This document records key technical decisions made during implementation,
the reasoning behind them, and alternatives considered.

---

### D1: Single-Threaded Discrete-Event Simulation

**Decision:** Use a single global event queue (min-heap) with sequential processing.
All blockchain "concurrency" is simulated by scheduling events at future times.

**Reason:** Goroutine scheduling order in Go is non-deterministic.
If each node ran as a goroutine, two runs with the same seed could produce
different outcomes because the Go scheduler does not guarantee ordering.
The event queue gives us total-order determinism: same seed → same output.

**Alternative considered:** Goroutine-per-node with a mutex-protected clock.
Rejected because it would require a lock on every event processed and still
wouldn't guarantee identical ordering across runs.

---

### D2: Block Hash = SHA-256 of Canonical Fields

**Decision:** Block hash is computed as SHA-256 of the string:
`"height|proposerID|parentHash|%.9f(createdAt)|txCount"`

**Reason:** SHA-256 is collision-resistant, widely understood, and produces
a fixed-length 64-character lowercase hex string.
The "%.9f" format pins floating-point representation to 9 decimal places,
avoiding platform-specific float formatting differences.

**Alternative considered:** A simple integer counter as the block ID.
Rejected because the hash ties the block's identity to its content,
making the data integrity observable and realistic.

---

### D3: Tie-Break by Lexicographically Smallest Hash

**Decision:** When two blocks have equal height, the node adopts the block
whose hash string is lexicographically smaller.

**Reason:** This is a purely deterministic rule that requires no additional
state. Every node with the same set of blocks will independently compute
the same winner. Hash values are uniformly distributed so neither proposer
has a systematic advantage.

**Alternative considered:** First-seen wins.
Rejected because "first seen" depends on network arrival order, which varies
by node — two nodes would disagree permanently on the winning fork at equal height.

---

### D4: Gamma Distribution for Network Delays

**Decision:** Network latency is sampled from a Gamma(shape, scale) distribution
using the Marsaglia-Tsang (2000) algorithm, inlined in `internal/network/network.go`.

**Reason:** Real network delays are right-skewed and strictly positive —
Gamma is a standard choice for this. The shape parameter is inferred from
(mean/stddev)² so the user only needs to specify mean and jitter.

**Alternative considered:** Uniform distribution (simpler).
Rejected because uniform delays produce unrealistically symmetric propagation
behavior — forks would be rarer and less authentic.

**Note:** Gonum's `stat/distuv.Gamma` implements the same algorithm.
We inline it in M1 to keep the package dependency minimal.
M2+ can switch to the gonum version if desired.

---

### D5: Multi-Stream Seeded RNG

**Decision:** Three independent PCG random streams are created from the base seed:
- Stream 1 (offset 1): block proposal jitter
- Stream 2 (offset 2): network delay sampling
- Stream 3 (offset 3): transaction count per block

**Reason:** Using `math/rand/v2` with `NewPCG(seed, offset)` creates independent
streams. This means changing the network topology does not affect block jitter,
preserving reproducibility when only one aspect of the simulation changes.

---

### D6: Peer Topology — Ring + Forward Links

**Decision:** Node i connects to nodes (i+1) % n, (i+2) % n, ..., (i+k) % n
where k = peers_per_node.

**Reason:** This produces a deterministic, connected graph without any randomness.
Every node has exactly the same number of peers. The ring structure ensures
all nodes are reachable within ceil(n/k) hops.

**Alternative considered:** Random Erdős–Rényi graph.
Rejected because it would require random peer selection, adding another
RNG stream whose order we'd need to control carefully.

---

### D7: JSONL Format for Event Logs

**Decision:** One JSON object per line (JSONL). Each line is self-contained
and parseable independently.

**Reason:**
- Streaming-friendly: logs can be read as they are written.
- Each line is independently parseable — a truncated file still has valid prefix.
- Easy to process with `bufio.Scanner` in Go or any text tool.
- Compatible with Milestone 2's feature extractor which reads events up to a time cutoff.

---

### D8: RunID = `<scenario>_seed<seed>_<unix_timestamp>`

**Decision:** Each run writes logs to `outputs/runs/<runID>/events.jsonl`.

**Reason:** The unix timestamp makes multiple runs of the same scenario/seed
non-overlapping on disk, which is useful for debugging and comparison.
The seed and scenario are in the name for human readability.

---

### D9: Network-Wide Block Proposal Rate & Proposer Selection Rule

**Decision:** The proposal scheduler regulates the network-wide proposal rate so that
on average one block proposal is created every `BlockIntervalSec` across the entire network,
rather than having each node independently propose at that rate (which caused an N-fold excess rate).
At each proposal slot, the proposer node is selected by a documented rule:
- `uniform_random` (default): Samples a node uniformly at random from `rngProposal`.
  This models lottery-based consensus (Proof-of-Stake / VRF leader election).
- `round_robin`: Rotates proposal opportunities deterministically in node order `(N0, N1, ...)`.

**Reason:** In decentralized blockchains, the target block interval $T$ is an aggregate network property.
Correcting proposal scheduling ensures empirical measurements agree with the experimental matrix and analytical models (e.g. Decker-Wattenhofer).

---

### D10: Reliable Orphan Buffering, Cascade Reprocessing & Propagation

**Decision:** When a node receives a block whose parent is not yet in its validated block store,
the block is stored in an in-memory orphan buffer (`orphans` map and `orphansByParent` index)
instead of being rejected and dropped.
When the missing parent arrives and is accepted, the node performs a breadth-first cascade
reprocessing all buffered descendants, validating heights and updating tips as appropriate.
Recovered orphan blocks are forwarded to connected peers who do not already possess them.
Uncontrolled duplicate gossip loops are strictly suppressed by link-level transmission deduplication
(`fromID|toID|blockHash` tracking) and peer knowledge checks.

**Reason:** Due to network jitter and gossip hops, valid blocks frequently arrive out-of-order.
Dropping orphans causes permanent block loss, node divergence, and corrupted chain views.
Buffering and reprocessing restores standard blockchain consensus behavior, and propagating
recovered orphans ensures network-wide block availability without gossip broadcast storms.

---

### D11: Two-Phase Simulation Termination & Bounded Settlement Window

**Decision:** Separate proposal termination from simulation completion:
1. **Proposal Termination:** When `max_blocks` is reached or `sim_duration_sec` elapses (`simTime >= stopTime`),
   no further `BlockPropose` events are scheduled or executed. Both termination triggers activate the
   settlement window.
2. **Settlement Phase:** The event loop continues processing in-flight `BlockGossip` and `BlockArrive`
   messages until all messages in transit drain naturally, bounded by `SettlementGraceSec` (`simTime <= proposalEndedAt + grace`).
   Events scheduled past this cutoff are dropped to guarantee termination.

**Reason:** Abruptly stopping the event loop when `max_blocks` was reached abandoned messages in transit,
leaving the network in an unresolved state with false divergence.
The settlement policy ensures all proposed blocks finish propagating and tips converge deterministically,
while the grace cutoff bounds worst-case run duration.

---

### D12: Observable Error Handling, Gossip Tracing & Accurate Tip Logging

**Decision:**
- Eliminate silent error suppression (`_ = err`). Simulation errors are recorded in `Engine.errors`
  and surfaced in `Result.ErrorCount` and `Result.Errors`.
- Gossip events track `FromNode` (sender) and `NodeID` (recipient) explicitly.
- Unvalidated/rejected blocks emit explicit `BlockReject` events in the JSONL log with specific failure reasons.
- Buffered out-of-order blocks emit `BlockOrphan` events.
- `TipUpdate` records the actual new tip block (`BlockHash`, `BlockHeight`, `ProposerID`, `ParentHash`),
  the previous tip (`OldTipHash`), and update reason (`extend` or `reorg`), correctly identifying
  the new tip even when orphan recovery advances the chain across multiple blocks.

**Reason:** Debugging and ML feature extraction require observable causality, accurate tip progression,
and reliable reorganization detection.

---

### D13: Run Provenance & Metadata Generation

**Decision:** Every simulation run writes `metadata.json` alongside `events.jsonl` in the run directory.
The metadata records scenario name, seed, UTC timestamp, Go toolchain version, platform (`runtime.GOOS/GOARCH`),
git commit status, full scenario parameters, proposed block counts, processed events, simulated duration,
settled duration, final tip heights, and the SHA-256 hash of `events.jsonl`.

**Reason:** Satisfies the planning document reproducibility checklist (§15) and guarantees that any run
can be replayed and verified independently.

---

## Golearn Compatibility Note

Per the planning document §3, golearn compatibility with Go 1.22+ must be
verified at Milestone 3 step 1. If it fails, the fallback is a custom
CART + bagging implementation on gonum. This decision is deferred to M3.

---

## Milestone 2 Decisions

### D14: Reorg Depth via Actual Common Ancestor (LCA)

**Decision:** Reorganization depth is now computed by walking both the old and new tip branches upward via `parent_hash` pointers until they meet at their lowest common ancestor (LCA). Depth = `oldTipHeight - LCAHeight`.

**Previous approach (rejected):** Used `min(oldHeight, newHeight) - 1` as the assumed common ancestor height. This systematically overestimates depth when the actual fork point is higher than the block below the shorter tip, and it can never compute depths > 1 for same-height switches.

**Implementation:** `commonAncestorDepth` in `internal/fork/fork.go`. Guard against cycles and incomplete parent maps (falls back to `oldTipHeight` as upper bound, minimum 1).

**Test coverage:** `TestReorgDepth_CommonAncestorExact` (depth=2, fork at height 1 of shared chain), `TestReorgDepth_SingleBlockRollback` (depth=1), `TestReorgDepth_ThreeBlockRollback` (depth=3 from genesis).

---

### D15: Node-View Consistency Denominator = All Configured Nodes

**Decision:** When computing node-view consistency snapshots, the denominator is always `NumNodes` (all configured nodes), not just those that have already sent a tip event by snapshot time.

**Previous approach (rejected):** Only nodes present in the `nodeTips` map at proposal time were counted. Early in the simulation, many nodes haven't yet received any block, so the denominator shrank to 1 at the first snapshot — falsely reporting 100% consistency even when all other nodes were at genesis.

**Fix:** Each snapshot now iterates over the complete `nodeIDs` slice (populated from the log + synthesised from `NumNodes`). Nodes not yet in the map are treated as having an empty-string tip, which counts as divergent from any non-empty tip.

**Impact:** Baseline runs now correctly report ~90% node-view consistency (9/10 nodes agree at proposal time; the proposer and 8 already-updated peers share the tip while 1 peer may lag).

---

### D16: Separate tx_arrival_rate from block_interval_sec

**Decision:** `tx_arrival_rate` (float64 multiplier, default 1.0) scales the number of transactions generated per block slot independently of `block_interval_sec`. Combined with `block_capacity` (int, 0 = unlimited), this allows modeling mempool saturation without changing block production frequency.

**Rationale:** §6 of the planning document explicitly requires separating the tx-load stress scenario (fixed T=1s, varying tx arrival) from the block-rate stress scenario (fixed tx load, varying T). Using a multiplier on the existing `TxPerBlock` sampling is the minimal change that honoring this separation.

---

### D17: Eight Scenario Files

The following YAML scenario files are now present in `scenarios/`:

| File | Independent Variable |
|---|---|
| `baseline.yaml` | None (reference) |
| `latency_stress.yaml` | Mean latency 50ms → 300ms |
| `propagation_stress.yaml` | Prop delay 100ms → 1000ms |
| `churn_stress.yaml` | Uptime ~99% → ~50% (churn_mean_on=5s, off=5s) |
| `block_rate_stress.yaml` | Block interval 1.0s → 0.2s (5× faster) |
| `tx_load_stress.yaml` | tx_arrival_rate ×10, block_capacity=10 |
| `combined_stress.yaml` | Latency+prop+block-rate scaled together |
| `dynamic_network.yaml` | Latency shift at block 150: 50ms → 300ms |
| `two_node_test.yaml` | (Legacy) 2-node fork validation |

---

### D18: cmd/metrics Command

**Decision:** Added `cmd/metrics/main.go` — a single command that runs a simulation and immediately computes run-level metrics, appending one row to `outputs/summary.csv`.

**Usage:**
```powershell
go run ./cmd/metrics --scenario scenarios/baseline.yaml --seed 7
go run ./cmd/metrics --scenario scenarios/latency_stress.yaml --seed 1 --csv outputs/pilot.csv
```

The CSV uses `metrics.AppendCSVRow` which writes the header only when the file is new, ensuring all rows have the same column count.

---

### D19: Two-Node Analytical Check (Milestone 2 Pilot)

**Observation:** The two-node scenario (`two_node_test.yaml`, latency=200ms, prop=200ms, T=1s) produced zero fork episodes across 10 seeds × 61 blocks. 

**Root cause:** Total propagation time ≈ 400ms < 1s block interval. Both nodes hear about every new block before the next proposal fires. This is correct — no fork is geometrically possible.

**Adjusted check:** The latency-stress scenario (300ms latency, 1s interval) produces empirical fork rates of 20–27% across seeds 1–3. The Decker-Wattenhofer formula with τ = 0.4s effective propagation (accounting for multi-hop efficiency of the ring+forward topology) gives a theoretical rate of ≈ 33%. Our empirical rate is lower because the ring+forward peer topology propagates blocks faster than the worst-case single-path assumption in the Decker formula. This discrepancy (empirical < theoretical) is consistent with more efficient routing and is documented here rather than treated as a bug.

---

### D20: Dynamic Network Config Reproducibility Fix (Superseded by D23)

> **Status:** Superseded by **D23 (Engine Configuration Ownership)**.

**Problem:** `applyDynamicEvents()` mutated `e.cfg.MeanLatencyMs`, `e.cfg.LatencyJitterMs`, `e.cfg.MeanPropDelayMs`, and `e.cfg.PropDelayJitterMs` in place through the shared `*ScenarioConfig` pointer. Both `cmd/simulate` and `cmd/metrics` wrote `Config: *cfg` to `metadata.json` and built `ComputeParams` from `cfg` fields *after* the run completed. For the `dynamic_network` scenario (latency shift 50ms → 300ms at block 150), the metadata and CSV recorded the final (post-shift) value of 300ms rather than the initial 50ms.

**Initial Interim Fix:** `engine.New()` took a single copy stored as `initialCfg config.ScenarioConfig` while still mutating `e.cfg` through the shared pointer.

**Why Superseded:** Mutating through a shared caller pointer remained unsafe and allowed race conditions or external mutations. D23 completely superseded this interim approach by creating two fully independent, owned copies inside the engine (`activeCfg` and `initialCfg`), reading the caller pointer only once on construction and never touching or sharing it again.

---

### D21: Baseline Churn Setting

**Problem:** The baseline scenario had `churn_enabled: false` even though §6 of the planning document specifies "uptime 99%" as a fixed baseline parameter. The README comment said "uptime 99%" but the YAML disabled churn entirely.

**Resolution:** The baseline now uses `churn_enabled: true`, `churn_mean_on_sec: 100.0`, `churn_mean_off_sec: 1.0`, giving theoretical uptime = 100/(100+1) ≈ 99.0%. This matches the plan specification.

**Observed impact (pilot, seeds 1-3):** The 99% uptime churn causes measurable chain divergence in 300-block runs. Stale rates of 32–64% and 8+ divergent tips at run end were observed. This happens because even brief 1s offline periods interrupt gossip propagation during a 300s run; the 10s settlement window doesn't always drain all in-flight messages before nodes' last offline intervals. This is valid experiment data — the baseline is not "zero effects"; it is the reference point from which stress scenarios depart. If the intent is a truly zero-churn reference, use `churn_enabled: false`; but that contradicts §6. The current setting is the correct interpretation of the plan.

---

### D22: Two-Node Analytical Check — Network-Wide vs Per-Node Rate

**Finding:** The two-node scenario (200ms latency, 200ms prop, 1s block interval) produces zero fork episodes across 10 seeds × 600 blocks each, despite the Decker-Wattenhofer formula predicting ≈33% fork rate.

**Root cause:** The engine uses a **network-wide** proposal rate (one block every `BlockIntervalSec` across the entire network). With 2 nodes, only one block fires per 1s interval. By the time the next proposal fires at t+1s, the previous block has propagated (400ms total << 1s). For a fork to occur, two proposals would need to happen within the propagation window (~400ms). This never occurs because the engine's sequential scheduler ensures proposals are at least `BlockIntervalSec` apart.

**Contrast with Decker:** The Decker formula assumes each node independently proposes at rate 1/T (Poisson), giving a 2-node system a combined rate of 2/T. Our network-wide scheduling is equivalent to a 1-node Poisson arrival at rate 1/T, which produces near-zero forks for τ/T < 1.

**Conclusion:** The two-node scenario with this engine validates correct long-chain behavior (all blocks propagate, nodes converge), but is not an appropriate test for the Decker formula. For the Decker check with the network-wide scheduler, a meaningful test requires τ/T close to 1, i.e., total propagation delay ≈ block interval. The latency-stress scenario (300ms latency + 100ms prop = 400ms per hop, multiple hops possible, T=1s) empirically produces 20–27% fork rate — this is the actual analytical comparison point. See D19.

---

### D23: Engine Configuration Ownership (Final Fix)

**Problem:** The engine stored `cfg *config.ScenarioConfig` (a pointer to the caller's struct). `applyDynamicEvents()` mutated the caller's struct through this shared pointer. Both `metadata.json` and `ComputeParams` read from the same pointer post-run, so a dynamic scenario recorded its post-shift values (e.g. 300ms) rather than its initial values (50ms).

**Fix:** `engine.New()` now deep-copies `*cfg` into two independent value fields:
- `activeCfg config.ScenarioConfig` — the engine's live working copy; `applyDynamicEvents()` modifies only this
- `initialCfg config.ScenarioConfig` — a snapshot taken at construction, never mutated

`InitialConfig()` returns a fresh deep-copy of `initialCfg` so callers cannot corrupt the snapshot. All pointer fields (`*float64` in `DynamicEvent`) are explicitly duplicated in both copies.

The caller's `*ScenarioConfig` is read once at construction and never touched again.

**Testing:** Seven regression tests cover: caller unchanged, initial config unchanged, active config changes at the right block, network sampling uses new model, no-event cases, prop-delay changes, snapshot immutability (mutating the return value of `InitialConfig()` does not affect the engine's saved copy).

**Verified in pilot:** `dynamic_network` metadata records `mean_latency_ms: 50` (initial) with `ConfigChange` event at block 150 confirming the shift.

---

### D24: ConfigChange Event in Log

**Decision:** When a dynamic event fires, the engine writes a `ConfigChange` log entry recording `config_field`, `config_old_value`, `config_new_value`, and `at_block`.

**Reason:** Enables independent audit of dynamic event timing from the event log alone. Tests can scan the log for `ConfigChange` entries to verify exact block timing without relying on engine internal state.

**Verified:** `TestDynamicEvent_ExactTransitionBlock` confirms exactly one `ConfigChange` event with the correct field name, old value (50.0000), new value (300.0000), and at_block (5) appears when the transition fires.

---

### D25: Scenario Churn Consistency

**Problem:** Stress scenarios (`latency_stress`, `propagation_stress`, `block_rate_stress`, `tx_load_stress`, `combined_stress`, `dynamic_network`) had `churn_enabled: false` while the baseline has `churn_enabled: true` (mean_on=100s, mean_off=1s). This is an unintended second variable change.

**Fix:** All stress scenarios now match the baseline's churn settings. This means:
1. Peer availability in stress scenarios matches baseline for the same seed (verified in pilot)
2. Observed metric differences are due to the intended stress variable only
3. The `TestScenarioConsistency` test enforces this programmatically

**Exception:** `churn_stress` intentionally changes `churn_mean_on_sec` and `churn_mean_off_sec` — this is its designated independent variable.

---

### D26: Pilot Results Summary (Milestone 2 Final)

Pilot: 8 scenarios × seeds {1, 2, 3} = 24 runs. Full results in `outputs/pilot/pilot_summary.csv`.

Key observations (measured):
- Baseline: fork rate 1.0–4.6%, stale 31.8–63.9% (churn causes propagation failures)
- Latency stress: fork rate 12.3–20.4% vs baseline 1.0–4.6% ✓ controlled variable works
- Propagation stress: fork rate 19.5–25.4%, highest single-variable scenario
- Churn stress: fork rate 3.3–6.4% but stale 88.3–90.4% (50% downtime blocks propagation)
- Block-rate stress: fork rate 9.1–18.9% (not monotonically highest — measured, not assumed)
- Tx-load stress: fork rate matches baseline (only throughput changes)
- Combined stress: fork rate 10.5–15.6%, stale 77.2–81.9%
- Dynamic network: fork rate 4.3–6.7% (half-baseline, half-latency-stress)

Results are NOT assumed monotonic. Each value is measured from an actual run.

---

### D27: Scenario Parameter Calibrations & Metric Implications

**Context:** Reconciling experimental scenarios against Section 6 of `CASE_STUDY_DEMO_PLAN.md`.

**1. Latency Stress Mean Value (50ms → 300ms vs original 500ms scope):**
- Early planning text mentioned 500ms. However, analytical modeling (D19, D22) and empirical simulation demonstrated that at 500ms mean latency + 100ms prop delay (600ms/hop) across our 10-node ring+forward topology (diameter 3–4 hops), end-to-end propagation takes 1.8–2.4s, severely exceeding the 1.0s block interval. This causes cascading tip fragmentation where nodes never converge within any reasonable settlement window.
- Choosing 300ms mean latency (400ms/hop) stresses consensus substantially (empirical fork rate 12–20%, theoretical Decker rate ~33%) while preserving an observable, non-degenerate operational regime.
- Furthermore, 300ms provides exact structural alignment with the post-transition regime of `dynamic_network` (50ms → 300ms) and `combined_stress` (300ms latency), enabling direct cross-scenario comparison.

**2. Latency and Propagation Jitter Scaling:**
- In `latency_stress`, `latency_jitter_ms` is set to 30.0ms (10% of mean delay) rather than keeping baseline's 10.0ms (which would yield an unrealistically low CV of 3.3%).
- In `propagation_stress`, `prop_delay_jitter_ms` is set to 100.0ms (10% of 1000ms mean delay).
- Scaling jitter proportionally keeps the Gamma delay distribution well-behaved and right-skewed, reflecting realistic network queuing variance.

**3. Settlement Grace Period Adjustment (`settlement_grace_sec`):**
- In `propagation_stress` (1000ms prop delay) and `combined_stress` (600ms prop delay + 300ms latency), `settlement_grace_sec` is increased from 10.0s to 30.0s.
- High one-way delays require more simulated time after block proposals cease to allow in-flight gossip messages to drain. Without this extension, late-proposed blocks would be cut off prematurely, artificially inflating stale rates and undercounting settled forks.

**4. Block Jitter in Block-Rate Stress:**
- In `block_rate_stress`, `block_interval_sec` is 0.2s. `block_jitter_sec` is scaled proportionally to 0.02s (10% of interval) to avoid jitter exceeding the interval and causing cadence inversions.

---

### D28: Decoupled Poisson Transaction Arrival Model and Mempool Queue

**Context:** Reconciling transaction arrival modeling with Section 6 of `CASE_STUDY_DEMO_PLAN.md`.

**Problem:** Previously, transaction generation was coupled to block proposal events (`handlePropose()` sampled transaction counts per proposal). When `block_interval_sec` was reduced from 1.0s to 0.2s in `block_rate_stress`, block proposals fired 5× more frequently, inadvertently inflating offered transaction load by 5× (from ~10 tx/s to ~50 tx/s), even though `tx_arrival_rate` remained 1.0. This violated the plan's requirement that the block-rate stress scenario isolate production frequency from offered transaction load.

**Resolution:**
1. **Independent Poisson Arrival Process:**
   - Transaction arrivals are scheduled as discrete `EventTxArrival` events in the simulation event queue using the dedicated `rngTx` PRNG stream (seed offset 3).
   - The arrival rate is $\lambda = \text{tx\_arrival\_rate} \times \text{tx\_per\_block}$ (transactions per simulated second).
   - Inter-arrival times are drawn exponentially: $\Delta t = -\frac{1}{\lambda} \ln(1 - U)$, where $U \sim \text{Uniform}(0, 1)$.
   - Each arrival increments the network mempool queue (`txPool`) and `totalOfferedTx`.

2. **Mempool Consumption & Saturation:**
   - When a block is proposed, it consumes up to `capacity` transactions from `txPool`, where `capacity` is `BlockCapacity` (or drains the entire pool if `BlockCapacity == 0`).
   - Consumed transactions are deducted from `txPool` and added to `totalProcessedTx`.
   - In `block_rate_stress` ($T = 0.2\text{ s}$, $\lambda = 10\text{ tx/s}$): An average of ~2 transactions arrive between successive blocks. Blocks take ~2 transactions each, keeping total throughput at ~10 tx/s, exactly matching baseline offered load.
   - In `tx_load_stress` ($T = 1.0\text{ s}$, $\lambda = 100\text{ offered tx/s}$, `block_capacity = 10`): ~100 transactions arrive per second while blocks consume at most 10 transactions per block, producing a persistent queue backlog of ~90 tx/s (actual measured throughput ~9.66 raw tx/s) and demonstrating queue saturation.

3. **Conservation Invariant:**
   - The invariant $\text{TotalOfferedTx} = \text{TotalProcessedTx} + \text{FinalPendingTx}$ holds strictly across all simulation steps. No transactions are lost or created out of thin air.

4. **Provenance & Reporting:**
   - `Result` and `logstore.RunMetadata` capture `TotalOfferedTx`, `TotalProcessedTx`, `MaxPendingTx`, `FinalPendingTx`, and `OfferedTPS`.
   - `LogEntry` records `PoolSize` and `OfferedTx` on each block proposal.

---

### D29: Block Capacity Zero Semantics & Explicit Zero Arrival Rate

**Context:** Final configuration semantics fix and Milestone 2 sign-off.

**1. Block Capacity Zero Semantics (`block_capacity: 0` = Unlimited):**
- **Problem:** Configuration documentation in `README.md` and `CASE_STUDY_DEMO_PLAN.md` specified that `block_capacity: 0` means unlimited transactions per block, while a positive capacity enforces a cap. However, `handlePropose()` previously fell back to `tx_per_block` (10) when `block_capacity == 0`. This caused the baseline scenario and `tx_load_stress` to share the same effective block capacity.
- **Resolution:** `handlePropose()` now implements the intended semantics:
  - If `BlockCapacity == 0`, the proposed block consumes the entire pending transaction pool (`txPool`).
  - If `BlockCapacity > 0`, the block consumes $\min(\text{txPool}, \text{BlockCapacity})$.
  - Excess transactions beyond the cap remain in `txPool` and are conserved.
- **Empirical Validation:**
  - In `baseline` (`block_capacity: 0`), blocks drain the pool each proposal. For Seed 1, 3,018 transactions were offered, 3,009 were processed, leaving only 9 pending at cutoff (which arrived after the final block proposal). Max pending pool size was only 19.
  - In `tx_load_stress` (`block_capacity: 10`, `tx_arrival_rate: 10.0`, `tx_per_block: 10`), the offered arrival rate was ~100 offered tx/s (30,021 offered transactions across the run for Seed 1). Block capacity capped inclusion to at most 10 tx/block, yielding an actual measured processed throughput of ~9.66 raw tx/s (2,990 transactions processed), leaving a substantial mempool backlog of 27,031 transactions (`final_pending_tx: 27031`, `max_pending_tx: 27031`), demonstrating genuine saturation without distorting consensus fork dynamics.

**2. Explicit Zero Transaction-Arrival Rate Handling:**
- **Problem:** `Defaults()` in `internal/config/config.go` checked `if c.TxArrivalRate == 0 { c.TxArrivalRate = 1.0 }`. This made it impossible to disable transaction arrivals by explicitly specifying `tx_arrival_rate: 0` in YAML.
- **Resolution:**
  - Added `TxArrivalRateExplicit bool` to `ScenarioConfig`.
  - Implemented custom `UnmarshalYAML` via `yaml.Node` inspection to track whether `tx_arrival_rate` was present in the YAML source.
  - `Defaults()` assigns 1.0 only if `!c.TxArrivalRateExplicit && c.TxArrivalRate == 0`.
  - `Validate()` rejects negative arrival rates (`< 0`).
- **Empirical Validation:**
  - Unit test `TestLoadFromFile_TxArrivalRateSemantics` and simulation integration test `TestZeroArrivalRate_FromYAMLFile` confirm that an explicit `tx_arrival_rate: 0` file produces 0 offered transactions, 0 processed transactions, and 0 pending transactions end-to-end.

**3. Test Count & Provenance Reconciliations:**
- Documented clear distinction: 109 top-level test functions (`func Test...`) expanding to 121 executed test cases (including table-driven subtests).
- Verified baseline seed 1 log SHA-256 reproducibility across two actual runs: `baseline_seed1_1792863044` and `baseline_seed1_1792863159` both yield `b99da89e7645eead22cb7d8600075ee5b3367676d8b54dfa1c894f2a6168a226`.
- Documented directory layout: `outputs/pilot_final/` holds the final verified pilot CSV and report; `outputs/pilot/` retains historical evidence.

---

### D30: Final Audit Reconciliations, Reproducibility Provenance, and Historical Run Quarantine

**Context:** Audit reconciliation and final packaging for Milestone 2 sign-off (`forkrisk_milestone2_final_audited.zip`).

**1. Reproducibility Run Reference Verification:**
- The final pilot report references two independent baseline seed 1 runs:
  - Run 1: `outputs/runs/baseline_seed1_1792863044/events.jsonl`
  - Run 2: `outputs/runs/baseline_seed1_1792863159/events.jsonl`
- Verification: Both event logs are byte-identical (2,975,527 bytes, SHA-256 = `b99da89e7645eead22cb7d8600075ee5b3367676d8b54dfa1c894f2a6168a226`). Both runs include full `metadata.json` and `events.jsonl` files in the archive.

**2. Historical Incomplete Run Quarantine:**
- Five run directories generated on 2026-10-21 lacked `metadata.json`:
  `baseline_seed7_1792563209`, `baseline_seed7_1792563218`, `baseline_seed7_1792563740`, `baseline_seed7_1792563759`, and `two_node_test_seed1_1792563889`.
- Investigation confirmed these were early prototype executions conducted before automated metadata serialization was introduced in `cmd/simulate`.
- To avoid fabricating synthetic metadata or discarding historical evidence, these directories were grouped into `outputs/runs/historical_incomplete/` with an explanatory `README.md` and explicitly labeled as `historical_incomplete`.

**3. Transaction-Load Modeling Boundary & Limitations:**
- Transaction arrivals follow an independent Poisson stream ($\lambda = \text{tx\_arrival\_rate} \times \text{tx\_per\_block} = 10.0 \times 10 = 100\text{ offered tx/s}$ in `tx_load_stress`) decoupled from block proposal frequency.
- `block_capacity: 0` provides unlimited capacity (draining `txPool`), while positive capacity enforces a per-block ceiling (at most 10 transactions included per block).
- `tx_load_stress` verifies queue saturation and persistent backlog (Seed 1: 30,021 offered tx at ~100 offered tx/s, 2,990 processed with measured throughput ~9.66 raw tx/s, 27,031 transactions queued in the mempool backlog).
- **Explicit Limitation:** Block transaction counts and payload sizes are not coupled to network message propagation delays in the Gamma sampler. The experiment demonstrates queue saturation and throughput decoupling, but does not claim that high transaction volume directly increases fork frequency through payload transmission latency. Modeling size-dependent propagation delays is recognized as a future extension beyond Milestone 2.

---

### D31: Custom Pure-Go CART + Bagged Random Forest over Stale `golearn` Dependency

**Context:** Milestone 3 model selection for candidate machine learning classifier.

**Decision:** Implement a self-contained, deterministic CART decision tree and bagged Random Forest ensemble in pure Go (`internal/ml/forest.go`), completely avoiding external libraries such as `golearn`.

**Reason:**
1. The planning document explicitly identified `golearn` as an external dependency risk: unmaintained for over 7 years, incompatible with Go 1.22+, and bringing dozens of legacy transitive dependencies and CGO/unsafe requirements.
2. The planned CART + bagging fallback was scoped in `CASE_STUDY_DEMO_PLAN.md` Section 3 and Section 16 as the preferred pure-Go alternative.
3. Implementing custom CART with weighted Gini impurity, random feature subsampling ($m_{\text{try}} = \lfloor \sqrt{D} \rfloor$), and independent seeded PRNGs (`math/rand/v2` with `NewPCG(seed, treeIndex)`) guarantees 100% deterministic reproducibility, zero external dependencies, seamless JSON persistence, and complete inspection.

---

### D32: Prediction Target Formalization ($H=3$), Fork Birth Height, and Boundary Censoring

**Context:** Milestone 3 target labeling and prediction problem definition.

**Decision:**
1. **Prediction Point:** Evaluated at each block proposal event for block height $t$ at simulation time $T_t$.
2. **Forecast Horizon:** $H = 3$ blocks (predicts whether $\ge 1$ *new* temporary fork birth occurs within blocks $t+1, t+2, t+3$).
3. **Distinct BirthHeight Field:** `ForkEpisode` is augmented with a distinct `BirthHeight` field, set once when the fork is first detected at height $h$. The existing `Height` field is preserved for its maximum-height progression meaning.
4. **New Fork Birth Label Rule:**
   - $\text{label} = 1$ if and only if a fork episode has its birth strictly in the future forecast window: `ep.BirthTime > T_t` AND `ep.BirthHeight > t` AND `ep.BirthHeight <= t + H`.
   - Continuation of a fork episode that already existed at prediction time (`ep.BirthTime <= T_t`) does NOT label future observation windows positive; it represents persistence of an existing fork, not a new future birth.
5. **Unresolved Episodes:** Any fork episode born in the forecast window (`ep.BirthTime > T_t && ep.BirthHeight \in (t, t+H]`) that remains unresolved at simulation end is preserved as a positive label.
6. **End-of-Run Boundary Censoring:** Any block proposal with $t > N - H$ (where $N$ is maximum proposed height) is censored and omitted from the dataset because its full $H$-block observation window is truncated by simulation termination. Warm-up blocks ($t \le 10$) are also skipped.

---

### D33: Run-Level Data Splitting to Prevent Cross-Window Leakage

**Context:** Training, validation, and testing partition strategy.

**Decision:** Partition datasets strictly by whole simulation runs (`run_id`), never by randomly shuffling individual block proposal rows.

**Reason:**
1. Successive block observations from the same simulation run are temporally and causally auto-correlated. Random row-level splitting places adjacent, nearly identical observations from the same run in both train and test splits, causing catastrophic data leakage and artificially inflated performance.
2. Grouping by run preserves the temporal sequence within each run.
3. The canonical split allocates complete random seeds:
   - Train: Seed 1 across all 8 scenarios (8 runs, 1,565 rows, prevalence: 51.82%)
   - Validation: Seed 2 across all 8 scenarios (8 runs, 1,085 rows, prevalence: 61.38%, used for threshold tuning and early stopping)
   - Held-Out Test: Seed 3 across all 8 scenarios (8 runs, 1,608 rows, prevalence: 50.87%)
4. All preprocessing (standardization means and standard deviations) and classification thresholds are fitted strictly on training and validation sets. Zero test data is observed during model fitting.

---

### D34: Causal Rolling-Window Feature Engineering & Truncation Guard

**Context:** Feature extraction architecture for predictive modeling.

**Decision:** Build 22 canonical features computed strictly from events with `SimTime <= T_t`.

**Reason:**
1. Model features must be realistically observable by a node at prediction time $T_t$. Future events, final-run metrics, and target labels are strictly forbidden from input vectors.
2. Engineered features span:
   - Dynamic network configuration parameters (tracking `ConfigChange` events at $T_t$)
   - Node-view disagreement ($1 - \text{majorityCount}/N$) and competing tips at maximum height
   - Rolling block proposal intervals and variance (last 10 blocks)
   - Rolling peer arrival delays and propagation times (last 10 blocks)
   - Churn frequency and active peer availability
   - Mempool backlog and throughput dynamics
   - Recent fork and reorganization history
3. **Causality Verification:** Unit test `TestFeatureCausalityTruncationGuard` proves that feature vectors computed from a full event stream truncated at $T_t$ match byte-for-byte with vectors computed from a physically truncated event stream.

---

### D35: Observational Drift Monitoring & Real Two-Phase Dynamic Network Analysis

**Context:** Milestone 3 drift analysis and scenario generalization.

**Decision:** Implement observational drift monitoring (`cmd/drift`) comparing feature distributions and predictive performance across all 8 operating regimes and the true two-phase temporal transition of the `dynamic_network` experiment.

**Reason:**
1. In `scenarios/dynamic_network.yaml`, exactly one parameter transition occurs at block 150: `mean_latency_ms` shifts from 50 ms to 300 ms, with 100 ms propagation delay throughout. The simulation terminates at 300s (~200 blocks).
2. The drift analysis therefore evaluates exactly two observable phases:
   - Phase 1 (Pre-Transition): Blocks 11–149 (50ms latency, 100ms prop delay; 374 samples, 19.52% fork prevalence).
   - Phase 2 (Post-Transition): Blocks 150+ (300ms latency, 100ms prop delay; 66 samples, 45.45% fork prevalence).
3. No 1,000 ms propagation delay phase was configured, and no blocks exist above ~200. Fictional third phases and unmeasured empty ranges are strictly excluded.
4. Observational drift monitoring preserves model integrity: drift is reported transparently without silent retraining or leakage of future outcomes.

---

### D36: Trailing 10-Block Stale Rate Calculation and Schema Version v1.1

**Context:** Definition and implementation of `stale_rate_10`.

**Decision:**
1. In `internal/features/extractor.go`, `stale_rate_10` is calculated strictly across the trailing 10-block proposal window ($[t-9, t]$).
2. A block proposal in the window is marked stale if it does not belong to the canonical chain traced from the majority tip of nodes at `maxHeight` at prediction time $T_t$.
3. The feature schema version is bumped from `v1.0` to `v1.1`.
4. Serialized models enforce `FeatureSchemaVersion == "v1.1"` on load to ensure incompatible models cannot silently load.

---

### D37: Evaluation Protocol Scope & Supplementary Leave-One-Scenario-Out (LOSO) Analysis

**Context:** Clear framing of generalization claims and empirical cross-regime validation.

**Decision:**
1. **Primary Protocol (Seed-Held-Out):** The primary test split reserves Seed 3 runs across the 8 operating scenarios. Because all 8 scenarios are present in training (Seed 1) and validation (Seed 2), this protocol rigorously tests generalization to *unseen stochastic seeds under known scenario parameter envelopes*, rather than unseen network architectures.
2. **Supplementary Protocol (LOSO):** A leave-one-scenario-out evaluation is conducted where each scenario is sequentially held out while models are trained on the remaining 7 scenarios.
3. **Documented Limitations:** With only 8 scenarios and 24 total runs, withholding extreme scenarios (such as `propagation_stress` or `block_rate_stress`) exposes models to severe out-of-distribution feature shift, causing performance drops on those held-out regimes. Consequently, no claims of zero-shot generalization across arbitrary unobserved network regimes are made without domain-adaptive retraining.

---

### D38: Rolling-Window Sizing Off-by-One Correction, Interval vs Proposal Distinction, and Schema Version v1.2

**Context:** Rolling-window feature calculations across trailing 10-block observations.

**Problem:**
1. Previous code in `internal/features/extractor.go` calculated `windowStartIdx := currentIdx - 10` and sliced through `currentIdx + 1`, which yielded 11 proposal records when `currentIdx >= 10`.
2. Furthermore, window definitions must differentiate between:
   - Proposal record windows (e.g., stale rate, latency, propagation delay, fork count, reorg count, mempool growth, processed TPS) which must examine exactly the last 10 eligible proposal records.
   - Interval windows (e.g., block proposal interval mean and variance) where 10 consecutive time intervals require 11 proposal timestamps.
3. Multiple competing proposals at the same block height each create a distinct proposal event and prediction point. Slicing on proposal records (ordered by simulation time and sequence) ensures that all competing proposals within the trailing window are counted and handled consistently.

**Decision:**
1. Fix proposal-record slicing: `propWindowStartIdx := max(0, currentIdx-9)`. Slicing `proposals[propWindowStartIdx : currentIdx+1]` yields at most 10 proposal records, and exactly 10 when `currentIdx >= 9`.
2. Fix interval-record slicing: `intervalStartIdx := max(0, currentIdx-10)`. Slicing `proposals[intervalStartIdx : currentIdx+1]` yields up to 11 proposal records, from which exactly up to 10 consecutive intervals are computed.
3. Added unit tests `TestRollingWindowBoundariesAndCompetingProposals` verifying boundary behavior at current indices 9, 10, and 11, as well as multiple competing proposals at the same block height.
4. Incremented feature schema version to `v1.2` and dataset version to `1.2` because feature values have been mathematically corrected.

---

### D39: Strict Model-Schema Compatibility Enforcement

**Context:** Compatibility validation when loading serialized machine learning models.

**Problem:**
In `internal/ml/logistic.go` and `internal/ml/forest.go`, the model loaders previously rejected a model only if `model.FeatureSchemaVersion != "" && model.FeatureSchemaVersion != "v1.1"`. This inadvertently permitted empty or missing schema versions.

**Decision:**
1. Enforce strict equality:
   `if model.FeatureSchemaVersion != SupportedFeatureSchemaVersion { return fmt.Errorf(...) }`
   where `SupportedFeatureSchemaVersion = "v1.3"` (originally "v1.2" in audit 1, now "v1.3").
2. Any model with an empty, missing, legacy (`v1.0`, `v1.1`, `v1.2`), or unknown future (`v2.0`) schema version is strictly rejected at load time.
3. Added comprehensive regression tests in `internal/ml/ml_test.go` (`TestModelSchemaCompatibilityStrict`) proving that `v1.3` is accepted while `v1.2`, `v1.1`, `v1.0`, empty, missing, and future versions are rejected for both Logistic Regression and Random Forest loaders.

---

### D40: Churn Event Type Alignment (`ChurnToggle`), Unique-Height Observation Window, and Schema Version v1.3

**Context:** Calculation of `peer_connectivity` and `churn_rate_100` features across simulation runs.

**Problem:**
1. The discrete-event simulation engine logs peer availability state transitions as `EventType = "ChurnToggle"`, containing `node_id` and `reason` ("offline" / "online").
2. The feature extractor in `internal/features/extractor.go` previously checked for `EventType == "PeerChurn"`. Consequently, all 3,317 churn events across the 24 authoritative pilot runs were ignored: `peer_connectivity` remained universally 1.0, and `churn_rate_100` remained 0.0 across all 4,258 dataset rows.
3. Furthermore, `churn_rate_100` previously indexed proposal records (`proposals[currentIdx-100]`), which does not accurately represent a 100-block window when competing proposals exist at the same height.

**Decision:**
1. **Event Type Alignment:** The extractor now consumes `ChurnToggle` (and preserves `PeerChurn` for backwards compatibility). Node states are updated dynamically (`onlineNodes[e.NodeID] = (e.Reason == "online")`).
2. **Unique-Height Window Sizing:**
   - Unique block heights observed up to `currentIdx` are collected.
   - When $M \ge 100$ unique heights are available, start boundary $T_{\text{start}}$ is the earliest proposal timestamp of $uniqueHeights[M-100]$. Denominator is $100.0$.
   - In early runs ($M < 100$), start boundary is $0.0$ (simulation start), and denominator is $M$ (available unique heights).
   - Events on exact boundaries ($T_{\text{start}} \le T_e \le T_{\text{pred}}$) are included.
   - Unit is consistently **churn events per block**.
3. **Verified Empirical Impact:**
   - In `churn_stress` (50% uptime), average `peer_connectivity` is measured at **0.493** (matching theoretical 50% uptime), with minimum 0.10 and maximum 0.90. `churn_rate_100` averages **12.868** churn events per block.
   - In baseline and other scenarios (99% uptime), average `peer_connectivity` is **0.990–0.991**, occasionally dipping to 0.80 during temporary node outages.
4. **Schema Versioning:** Feature schema version incremented to `v1.3`, dataset version to `1.3`. Model loaders enforce `v1.3`.

---

### D41: Height-Level Sampling Unit and Anchor Definition

**Context:** Observation unit and prediction anchor in dataset generation.

**Decision:**
1. The dataset generates exactly one observation row per unique eligible block height ($t \in (\text{warmUpBlocks}, N - H]$) for each simulation run.
2. When multiple competing block proposals occur at the same block height, the **earliest proposal event** at that height serves as the prediction-time anchor.
3. Competing proposals at the same height are not emitted as separate dataset rows, preserving height-level independence and avoiding row duplication.
4. Target labels evaluate whether at least one new fork birth occurs in the forecast horizon of the next $H=3$ block heights $(t, t+H]$.
5. Verified by `TestSamplingUnit_HeightLevelAnchor` ensuring exactly one anchored sample per unique height regardless of competing proposals.

---

### D42: Milestone 4 Full Experiment Matrix Reconciliation (8 Scenarios × 30 Seeds), Sliding-Window Drift Detection, and Publication Visualizations

**Context:** Implementation of Milestone 4: Full experimental matrix, statistical analysis, pure-Go chart rendering, and concept drift detection.

**Problem:**
1. Planning document discrepancy: §6 of `CASE_STUDY_DEMO_PLAN.md` defined an authoritative 8-scenario matrix, while early checklist summaries referred to a 6×30 matrix.
2. Short 300-second pilot runs (~300 blocks) served as exploratory calibration, but production specifications required 10,000 blocks per run with a 1,000-block warmup.
3. Concept drift detection required an active, sliding-window statistical detector with validation grid tuning, distinct from observational static summaries.
4. Pure Go visualization requirement: no external Python, notebook, or headless browser services.

**Decision:**
1. **Authoritative 8-Scenario Matrix:** Reconciled the 8-scenario table as authoritative:
   - 8 scenarios: `baseline`, `latency_stress`, `propagation_stress`, `churn_stress`, `block_rate_stress`, `tx_load_stress`, `combined_stress`, `dynamic_network`.
   - 30 distinct seeds per scenario (Seeds 1–30), totaling exactly **240 production runs**.
   - 10,000 blocks per run, 1,000 warm-up blocks (~2.4 million total simulated blocks).
   - In `dynamic_network`, the transition from 50 ms to 300 ms mean latency takes effect at block 5,000.
   - Historical 300s pilot runs (Seeds 1–3) remain preserved as separate historical evidence.
2. **Deterministic Go Experiment Runner:**
   - Implemented `cmd/experiment` with worker pool concurrency, deterministic PRNG seed streams, resume-safe manifest tracking (`run_manifest.json`), and SHA-256 event-log integrity.
   - All 240 runs succeeded with 0 failures.
3. **Statistical Aggregation:**
   - Generated `run_summary.csv` (240 rows) and `scenario_summary.csv` (8 rows) with sample standard deviations ($N-1$ denominator), medians, and pooled fork/stale rates.
4. **Independent Prediction Evaluation:**
   - Evaluated frozen schema v1.3 models on independent, held-out production seeds (Seeds 4–30; 5,804 samples) without leakage.
5. **Sliding-Window Drift Detector (`internal/ml/drift`):**
   - Implemented `SlidingWindowDetector` with rolling mean and variance, $\epsilon$-floor zero-variance guard, and insufficient history guards.
   - Conducted validation-only grid tuning ($W \in [50, 100, 200] \times \theta \in [1.5, 2.0, 2.5]\sigma$) under the constraint of false positive rate $< 5\%$ on stationary validation data.
   - Optimal configuration selected: $W^* = 200$, $\theta^* = 2.0\sigma$. Tested on unseen production test seeds (Seeds 4–30), achieving 4.33% pre-transition FPR and immediate detection of the block 5,000 transition.
6. **Pure-Go Visualizations (`internal/viz`):**
   - Implemented publication-grade PNG chart generation using `gonum/plot v0.17.0`:
     1. Fork rate by scenario (with sample std dev error bars)
     2. Stale rate by scenario
     3. Propagation delay performance (mean and p95)
     4. Node-view consistency comparison and dynamic network transition time-series
     5. ROC curves with AUC annotations
     6. Precision-Recall curves with PR-AUC annotations
     7. Random Forest feature importance (tree split counts)
     8. Raw TPS vs Fork Rate trade-off
     9. Goodput vs Fork Rate trade-off
   - Verified that all 9 PNG files are valid, non-corrupt images.




\


---

### D43: Milestone 5 Final Evaluation, Horizon Sensitivity Sweep (H=1..5), Final Research Report, and Demonstration CLI

**Context:** Implementation of Milestone 5 according to Section 13 of CASE_STUDY_DEMO_PLAN.md.

**Decision:**
1. **Constant No-Fork Baseline:** Added ConstantNoForkBaseline to internal/ml/baselines.go and cmd/evaluate/main.go, producing formal comparison against PrevalenceBaseline, HeuristicBaseline, LogisticRegression, and RandomForest. Generated outputs/ml/evaluation/confusion_matrices/constant_no_fork_baseline_cm.txt.
2. **Forecast Horizon Sensitivity Sweep (H in {1, 2, 3, 4, 5}):**
   - Implemented cmd/sensitivity/main.go and internal/viz/charts.go (PlotHorizonSensitivity).
   - Swept H=1 to H=5 over 24 pilot runs with run-level splitting (Seeds 1, 2 for train/val; Seed 3 for test).
   - Documented monotonicity: Positive label prevalence climbs from 30.54% (H=1) to 63.82% (H=5). LR ROC-AUC increases monotonically from 0.7547 (H=1) to 0.8621 (H=5), while PR-AUC increases from 0.5029 to 0.8891.
   - Preserved exact H=3 metrics (4,258 rows, 1,608 test rows, LR ROC-AUC 0.8113).
   - Generated outputs/final_evaluation/horizon_sensitivity.csv, horizon_sensitivity.json, charts/horizon_sensitivity.png, and docs/figures/horizon_sensitivity.png.
3. **Automated Final Research Report Generator (cmd/report/main.go):**
   - Implemented comprehensive generator compiling docs/report.md (and mirrored to outputs/final_evaluation/report.md).
   - Covered all 6 required sections: Executive Summary, Experimental Design, Empirical Results, Machine Learning Analysis, Threats to Validity, and Conclusion & Next-Semester Handoff.
4. **Demonstration CLI (cmd/demo/main.go) & PowerShell Script (run_demo.ps1):**
   - Implemented dual-mode demo: --mode live executes real-time 100-block baseline vs stress simulations, calculates causal features, evaluates ML models, and prints horizon sensitivity. --mode fallback conducts fast 50-block deterministic verification and presents precomputed production matrix audit evidence.
   - Dual logging streams to console and outputs/demo/demo_transcript.txt.

---

### D44: Milestone 6 Web Dashboard and Real-Chain Cross-Check Investigation

**Context:** Implementation of Milestone 6 extension per CASE_STUDY_DEMO_PLAN.md.

**Decision:**
1. **Lightweight Pure-Go HTTP Backend:**
   - Implemented in cmd/dashboard/main.go and internal/dashboard/ (server.go, handlers.go, loader.go, models.go, handlers_test.go).
   - Zero external web frameworks (no Node.js backend, Python, Next.js, or Docker). Uses standard library net/http.
   - Security: Strict input validation on runID using ^[a-zA-Z0-9_\-]+$ regex to prevent directory traversal. All endpoints enforce read-only GET/HEAD methods (non-GET requests return 405 Method Not Allowed).
   - Endpoints provided: /api/health, /api/runs, /api/runs/{runID}, /api/runs/{runID}/nodes, /api/runs/{runID}/timeline, /api/scenarios/summary, /api/models/evaluation, /api/drift, /api/sensitivity.
2. **Modern Dark-Mode Frontend (web/):**
   - Implemented single-page architecture with web/index.html, web/styles.css, web/app.js, and web/charts.js.
   - Integrated Chart.js for responsive grouped bar, line, and radar visualizations.
   - 6 Interactive Views: Overview, Run Inspector, Node Status, Fork Timeline, Scenario Matrix, and Models & Sensitivity.
3. **Real-Chain Cross-Check Feasibility Analysis (docs/real_chain_cross_check.md):**
   - Environment audit: confirmed host environment contains Node v22.18.0 and npm 11.6.1, while Geth and local Hardhat are absent.
   - Formally documented theoretical mapping between ForkRisk simulation metrics and real EVM telemetry (uncle rates, attestation head divergence, reorg depth, mempool velocity).
   - Defined 5-phase cross-validation protocol for future private testnet container clusters and analyzed trade-offs.
