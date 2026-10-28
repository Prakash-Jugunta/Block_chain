# ForkRisk Milestone 3 — Correctness Fixes and Re-Evaluation Audit Report

**Date:** 2026-10-25  
**Toolchain:** Go 1.25.1 windows/amd64  
**Audit Status:** PASSED (Verified & Reconciled)

---

## 1. Executive Summary

This audit report verifies the Milestone 3 correctness fixes, dataset regeneration, model retraining, and final evaluation for ForkRisk (*Predicting Network-Induced Fork Risk in Blockchain Networks*).

All six audit requirements have been resolved:
1. **Ground-Truth Fork Birth Labeling Fixed:** `ForkEpisode` augmented with a distinct `BirthHeight` field, set once at birth. Target label represents a *new* fork birth in $(t, t+H]$ where `BirthTime > T_t` and `BirthHeight \in (t, t+H]$. Ongoing forks born prior to $T_t$ do not produce false positive labels for future prediction points.
2. **Trailing Stale Rate (`stale_rate_10`) Corrected:** Replaced prefix-wide stale rate with the true trailing 10-block window calculation relative to the canonical chain. Feature schema version bumped to `v1.1`, with model loading guarded against incompatible schemas.
3. **Dataset Regenerated & Models Retrained:** Dataset regenerated from authoritative pilot logs (4,258 rows, SHA-256 `c4965445ecf55225a6b555ffdd7368e5a4afb7c719d1b21492f251731c9f2f85`). Zero run overlap across splits. Models retrained with thresholds tuned on validation split ($\tau_{\text{LR}} = 0.25$, $\tau_{\text{RF}} = 0.20$).
4. **Dynamic Network Drift Report Corrected:** Analyzes the actual two configured phases (Blocks 11–149 at 50ms latency, Blocks 150+ at 300ms latency), removing fictional Phase 3 (1000ms delay) and empty block ranges.
5. **Reconciliation & Generalization Honesty:** Protocols clearly distinguished: Seed-Held-Out evaluates unseen random seeds under known scenario operating envelopes; supplementary Leave-One-Scenario-Out (LOSO) evaluation documents out-of-distribution performance drops. All metrics, confusion matrices, and narratives agree exactly.
6. **Toolchain & Release Verification:** Code formatted (`gofmt -l .` clean), clean build (`go build ./...` exit 0), complete test suite passing (`go test ./... -count=1` exit 0), packaged as `forkrisk_milestone3_corrected.zip`.

---

## 2. Dataset and Split Verification

| Metric | Value | Verification Notes |
|---|---|---|
| **Authoritative Runs** | 24 | Loaded from `outputs/pilot_final/pilot_summary.csv` |
| **Excluded Runs** | 0 | All 24 pilot runs have complete events.jsonl and metadata.json |
| **Total Rows** | 4,258 | Warmup (first 10 blocks) and end-of-run horizon boundary ($t > N-H$) censored |
| **Positive Labels** | 2,295 (53.90%) | Ground-truth new fork birth in $(t, t+H]$ |
| **Negative Labels** | 1,963 (46.10%) | No new fork birth in $(t, t+H]$ |
| **Dataset File** | `outputs/ml/dataset/fork_risk_dataset.csv` | 24 columns (run_id, scenario, seed, block_height, sim_time, 22 features, label_fork_h3) |
| **Dataset SHA-256** | `c4965445ecf55225a6b555ffdd7368e5a4afb7c719d1b21492f251731c9f2f85` | Verified against CSV byte stream |
| **Feature Schema Version** | `v1.1` | Incremented to guard against v1.0 models |
| **Split Strategy** | `seed_held_out` | Complete runs partitioned by seed |
| **Train Split** | 8 runs, 1,565 rows (51.82% pos) | Seed 1 across all 8 scenarios |
| **Validation Split** | 8 runs, 1,085 rows (61.38% pos) | Seed 2 across all 8 scenarios (threshold tuning) |
| **Test Split** | 8 runs, 1,608 rows (50.87% pos) | Seed 3 across all 8 scenarios (held-out test) |
| **Run-ID Overlap** | **0 runs (0.0%)** | Strict disjoint partition: $\text{Train} \cap \text{Val} = \emptyset$, $\text{Train} \cap \text{Test} = \emptyset$, $\text{Val} \cap \text{Test} = \emptyset$ |

---

## 3. Model Training & Evaluation Metrics

Thresholds were tuned on the validation split ($\tau_{\text{LR}} = 0.25$, $\tau_{\text{RF}} = 0.20$) without observing any test split samples.

### Held-Out Test Set Performance (Seed 3 runs, 1,608 samples, Prevalence: 50.87%)

| Model | ROC-AUC | PR-AUC | F1 Score | Precision | Recall | Brier Score | Tuned Threshold |
|---|---|---|---|---|---|---|---|
| **Prevalence Baseline** | 0.5000 | 0.5087 | 0.6744 | 0.5087 | 1.0000 | 0.2500 | 0.50 |
| **Heuristic Baseline** | 0.5842 | 0.5546 | 0.7135 | 0.5546 | 1.0000 | 0.2851 | 0.50 |
| **Logistic Regression** | **0.8018** | **0.7610** | **0.7882** | 0.6825 | 0.9328 | **0.1766** | 0.25 |
| **Random Forest** | **0.8236** | **0.7895** | **0.7568** | 0.6212 | 0.9682 | **0.1651** | 0.20 |

### Confusion Matrices (Held-Out Test Set: 818 Positives, 790 Negatives)

- **Logistic Regression ($\tau = 0.25$):**
  - True Positives (TP): 763
  - False Positives (FP): 355
  - True Negatives (TN): 435
  - False Negatives (FN): 55
  - Total: $763 + 355 + 435 + 55 = 1,608$ (Accuracy: 74.50%)

- **Random Forest ($\tau = 0.20$):**
  - True Positives (TP): 792
  - False Positives (FP): 483
  - True Negatives (TN): 307
  - False Negatives (FN): 26
  - Total: $792 + 483 + 307 + 26 = 1,608$ (Accuracy: 68.35%)

---

## 4. Supplementary Leave-One-Scenario-Out (LOSO) Evaluation

To evaluate generalization to unseen operating regimes, a supplementary Leave-One-Scenario-Out cross-evaluation was executed across the 8 scenarios:

| Held-Out Scenario | Train Samples | Test Samples | Test Prevalence | ROC-AUC | PR-AUC | F1 Score | Brier Score |
|---|---|---|---|---|---|---|---|
| `baseline` | 3,782 | 476 | 10.71% | 0.7294 | 0.3429 | 0.0370 | 0.0867 |
| `block_rate_stress` | 2,772 | 1,486 | 71.60% | 0.5765 | 0.7602 | 0.0000 | 0.6055 |
| `churn_stress` | 4,174 | 84 | 59.52% | 0.7876 | 0.8594 | 0.0000 | 0.4712 |
| `combined_stress` | 3,688 | 570 | 85.26% | 0.4802 | 0.8364 | 0.9205 | 0.1438 |
| `dynamic_network` | 3,818 | 440 | 23.41% | 0.7331 | 0.4148 | 0.4766 | 0.1614 |
| `latency_stress` | 3,832 | 426 | 62.44% | 0.4799 | 0.6155 | 0.7432 | 0.3118 |
| `propagation_stress` | 3,986 | 272 | 82.72% | 0.3226 | 0.7593 | 0.9010 | 0.2423 |
| `tx_load_stress` | 3,754 | 504 | 9.92% | 0.5434 | 0.1004 | 0.1805 | 0.8005 |

**Methodological Findings:**
- When models are evaluated on unseen random seeds under known scenario parameter spaces (primary seed-held-out protocol), both Logistic Regression (0.8018) and Random Forest (0.8236) achieve strong discrimination.
- When an entire operating regime is held out (e.g. `propagation_stress` or `latency_stress`), the model encounters severe out-of-distribution feature shift and extreme prevalence swings, reducing discrimination.
- Strong claims of zero-shot generalization across novel, unobserved blockchain network topologies are therefore scientifically unfounded and explicitly disclaimed.

---

## 5. Dynamic Network Temporal Drift Analysis

The `dynamic_network` scenario (`scenarios/dynamic_network.yaml`) shifts `mean_latency_ms` from 50 ms to 300 ms at block 150 (with 100 ms propagation delay throughout). The simulation runs for 300 seconds (~200 blocks total):

| Phase | Block Range | Observed Latency (s) | Observed Prop Time (s) | Fork Prevalence (H=3) | Samples |
|---|---|---|---|---|---|
| **Phase 1 (Pre-Transition)** | Blocks 11–149 | 0.3916 ± 0.1959 | 0.6126 ± 0.2891 | 19.52% | 374 |
| **Phase 2 (Post-Transition)** | Blocks 150+ | 0.8336 ± 0.0398 | 1.2492 ± 0.1258 | 45.45% | 66 |

No blocks exist above ~200, and no 1,000 ms propagation delay transition was configured. The two-phase analysis accurately reflects the physical simulation logs.
