# ForkRisk Milestone 3 — Final Audit Report: Feature Windows, Label Verification, and Model Schema

**Date:** 2026-10-25  
**Toolchain:** Go 1.25.1 windows/amd64  
**Audit Status:** PASSED (Verified & Reconciled)

---

## 1. Executive Summary

This audit report records the final Milestone 3 correctness verification covering rolling-window feature boundaries, ground-truth label audit, strict model-schema enforcement, and complete artifact regeneration for ForkRisk (*Predicting Network-Induced Fork Risk in Blockchain Networks*).

All audit items have been resolved and verified:
1. **Rolling-Window Feature Definitions (`_10` Features):**
   - Fixed the off-by-one window slicing: exactly 10 proposal records are sliced for proposal-based metrics (`currentIdx - 9` to `currentIdx + 1`).
   - Distinguished 10 block intervals from 10 proposal records: interval features (`avg_block_interval_10`, `block_interval_var_10`) collect up to 10 intervals from up to 11 timestamps (`currentIdx - 10` to `currentIdx + 1`).
   - Boundary tests added and verified at current indices 9, 10, and 11, including competing proposals at the same block height.
   - Schema version incremented to `v1.2`.
2. **Fork-Birth Label Audit (`cmd/labelaudit`):**
   - Audited all 24 authoritative pilot runs (4,258 prediction points).
   - Confirmed 0 old-to-new label differences on the pilot runs because in this model, temporary fork episodes resolve quickly within 1–2 block intervals without bridging across > 3 block heights without new births.
   - Verified that all 4,258 rows in `fork_risk_dataset.csv` match the recomputed labels with zero mismatches.
   - Regression tests prove that in scenarios where a fork persists into future windows without new births, the corrected labeler strictly rejects ongoing continuation, preventing false positives.
3. **Strict Model Schema Compatibility:**
   - Both `LoadLogisticRegression` and `LoadRandomForest` now require `model.FeatureSchemaVersion == SupportedFeatureSchemaVersion ("v1.2")`.
   - `v1.0`, `v1.1`, empty, missing, and unknown future versions are strictly rejected with an explicit error.
   - Regression tests in `internal/ml/ml_test.go` prove all rejection cases pass.
4. **Artifacts Regenerated:**
   - Dataset SHA-256 updated to `367262d37ba3c9184f091bf852b2b6b9dfdd7aa9b0c36ad8547811db781934f7`.
   - Prior `v1.1` artifacts preserved in `outputs/ml/historical_v1_1/`.
   - Retrained Logistic Regression ($\tau^* = 0.25$, ROC-AUC 0.8020) and Random Forest ($\tau^* = 0.25$, ROC-AUC 0.8206).
   - Recomputed confusion matrices, evaluation report, and two-phase drift report.
5. **Release Packaging:**
   - Clean formatting (`gofmt -l .` clean), clean compilation (`go build ./...` exit 0), full test suite passing (`go test ./... -count=1` exit 0).
   - Packaged and verified as `forkrisk_milestone3_final_audited.zip`.

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
| **Dataset SHA-256** | `367262d37ba3c9184f091bf852b2b6b9dfdd7aa9b0c36ad8547811db781934f7` | Verified against CSV byte stream |
| **Feature Schema Version** | `v1.2` | Bumped following rolling-window semantic correction |
| **Split Strategy** | `seed_held_out` | Complete runs partitioned by seed |
| **Train Split** | 8 runs, 1,565 rows (51.82% pos) | Seed 1 across all 8 scenarios |
| **Validation Split** | 8 runs, 1,085 rows (61.38% pos) | Seed 2 across all 8 scenarios (threshold tuning) |
| **Test Split** | 8 runs, 1,608 rows (50.87% pos) | Seed 3 across all 8 scenarios (held-out test) |
| **Run-ID Overlap** | **0 runs (0.0%)** | Strict disjoint partition: $\text{Train} \cap \text{Val} = \emptyset$, $\text{Train} \cap \text{Test} = \emptyset$, $\text{Val} \cap \text{Test} = \emptyset$ |

---

## 3. Model Training & Evaluation Metrics

Thresholds were tuned on the validation split ($\tau^*_{\text{LR}} = 0.25$, $\tau^*_{\text{RF}} = 0.25$) without observing any test split samples.

### Held-Out Test Set Performance (Seed 3 runs, 1,608 samples, Prevalence: 50.87%)

| Model | ROC-AUC | PR-AUC | F1 Score | Precision | Recall | Brier Score | Tuned Threshold |
|---|---|---|---|---|---|---|---|
| **Prevalence Baseline** | 0.5000 | 0.5087 | 0.6744 | 0.5087 | 1.0000 | 0.2500 | 0.50 |
| **Heuristic Baseline** | 0.5842 | 0.5546 | 0.7135 | 0.5546 | 1.0000 | 0.2851 | 0.50 |
| **Logistic Regression** | **0.8020** | **0.7545** | **0.7901** | 0.6846 | 0.9340 | **0.1771** | 0.25 |
| **Random Forest** | **0.8206** | **0.7892** | **0.7539** | 0.6173 | 0.9682 | **0.1702** | 0.25 |

### Confusion Matrices (Held-Out Test Set: 818 Positives, 790 Negatives)

- **Logistic Regression ($\tau = 0.25$):**
  $$\begin{pmatrix} \text{TN} = 438 & \text{FP} = 352 \\ \text{FN} = 54 & \text{TP} = 764 \end{pmatrix} \implies \text{Accuracy} = \frac{438 + 764}{1608} = 74.75\%$$

- **Random Forest ($\tau = 0.25$):**
  $$\begin{pmatrix} \text{TN} = 299 & \text{FP} = 491 \\ \text{FN} = 26 & \text{TP} = 792 \end{pmatrix} \implies \text{Accuracy} = \frac{299 + 792}{1608} = 67.85\%$$

---

## 4. Label Audit Findings (`cmd/labelaudit`)

* **Total Fork Episodes in Pilot:** 1,595
* **Episodes with BirthHeight != Height:** 0
* **Total Prediction Points Audited:** 4,258
* **Old-to-New Label Changes in Pilot:** 0
* **CSV vs Recomputed Label Matches:** 4,258 / 4,258 (100% agreement, 0 mismatches)

**Explanation:** In the 24 pilot runs, competing blocks occurred within narrow propagation delays at identical heights. Because competing blocks converged quickly (typically within 1–2 block intervals), competing branches did not extend across $>3$ block heights without new births. Consequently, `ep.BirthHeight` and `ep.Height` evaluated to the same forecast windows across all pilot rows. The semantic correction remains crucial and is verified by regression tests (`TestLabeler_ForkBornBeforeWindowPersists`), which prove that an ongoing fork extending across heights is correctly rejected as a future birth.

---

## 5. Supplementary Leave-One-Scenario-Out (LOSO) Evaluation

| Held-Out Scenario | Train Samples | Test Samples | Test Prevalence | ROC-AUC | PR-AUC | F1 Score | Brier Score |
|---|---|---|---|---|---|---|---|
| `baseline` | 3,782 | 476 | 10.71% | 0.6994 | 0.2651 | 0.0000 | 0.0901 |
| `block_rate_stress` | 2,772 | 1,486 | 71.60% | 0.5779 | 0.7614 | 0.0075 | 0.6019 |
| `churn_stress` | 4,174 | 84 | 59.52% | 0.7894 | 0.8610 | 0.0000 | 0.4664 |
| `combined_stress` | 3,688 | 570 | 85.26% | 0.5580 | 0.8783 | 0.9205 | 0.1443 |
| `dynamic_network` | 3,818 | 440 | 23.41% | 0.7394 | 0.4445 | 0.5478 | 0.1564 |
| `latency_stress` | 3,832 | 426 | 62.44% | 0.4836 | 0.6211 | 0.7190 | 0.3057 |
| `propagation_stress` | 3,986 | 272 | 82.72% | 0.3371 | 0.7690 | 0.9054 | 0.2029 |
| `tx_load_stress` | 3,754 | 504 | 9.92% | 0.5450 | 0.1007 | 0.1805 | 0.8055 |

**Generalization Boundaries:**
- The primary protocol (Seed-Held-Out) demonstrates robustness across independent stochastic random seeds within known scenario parameter regimes.
- When an entire operating regime is withheld (LOSO), out-of-distribution feature shift and radical prevalence swings lead to degraded discrimination. Zero-shot generalization across unobserved network regimes is therefore not claimed.
