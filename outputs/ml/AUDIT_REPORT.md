# ForkRisk Milestone 3 — Final Audit Report: Reference Label Audit, Single-Class ROC-AUC, and Milestone 3 Sign-Off

**Date:** 2026-10-25  
**Toolchain:** Go 1.25.1 windows/amd64  
**Audit Status:** PASSED (Verified & Signed Off)

---

## 1. Executive Summary

This audit report documents the completion and verification of the final audit corrections for Milestone 3 of ForkRisk (*Predicting Network-Induced Fork Risk in Blockchain Networks*):
1. **Independent Fork-Label Reference Audit & Scope Clarification:**
   - Implemented an independent reference labeler function (`cmd/labelaudit/main.go`) directly from the documented $H=3$ specification without depending on `internal/features.Labeler`.
   - Corrected detector initialization in the audit from `warmUpBlocks = 0` to `warmUpBlocks = 10`, matching the authoritative dataset generation pipeline.
   - Tested on hand-constructed fork episodes (`cmd/labelaudit/main_test.go`), verifying fork births vs continuation rejection, boundary conditions, and unresolved episodes.
   - Audit over all 24 pilot runs proved **100.0% exact match** (4,258 / 4,258 rows, 0 mismatches) between reference, production, and dataset CSV labels.
   - **Audit Scope Clarification:** This audit independently validates the mathematical assignment of $H=3$ fork birth labels against the formal specification. Because both the reference labeler and the production labeler consume `ForkEpisode` records produced by `fork.Detector`, this audit validates label assignment, boundary censoring, and continuation rejection; it does *not* independently validate the internal event-to-fork-episode detection logic of `fork.Detector` itself.
2. **Correct Single-Class ROC-AUC Handling:**
   - The held-out `churn_stress` test subset has 26 samples and 100.00% positive prevalence. Because ROC-AUC requires both positive and negative instances, it is mathematically undefined for single-class subsets.
   - Replaced arbitrary 0.5 constant with a formal unavailable representation: `*float64(nil)` in Go, `"roc_auc": null` in `metrics.json`, and `"N/A"` in Markdown evaluation reports.
   - Added regression test suite (`TestSingleClassROCAUCReturnsNil`) for all-positive, all-negative, and empty subsets.
3. **Model Generalization Claims Reconciled (Evidence-Based Distinction):**
   - **Pooled performance across the eight modeled scenarios:** In the pooled held-out test split (Seed 3, 1,608 samples across 8 scenarios), models exhibit strong aggregate discrimination (LR ROC-AUC **0.8124**, RF ROC-AUC **0.8139**). High pooled ROC-AUC strongly benefits from cross-scenario prevalence differences (calm regimes ~7.3% vs. stress regimes >70%).
   - **Crucial limitation:** Pooled ROC-AUC around 0.81 does **not** imply reliable within-scenario prediction.
   - **Performance within individual scenarios:** Within calm, low-prevalence operating regimes (`baseline` and `tx_load_stress`, both 7.33% prevalence), within-scenario discrimination is near or below chance: Logistic Regression achieves ROC-AUC of **0.4253** on baseline and **0.4520** on transaction-load stress, with F1 of **0.0000** in both subsets at the selected threshold (RF achieves F1 of ~0.088 with ROC-AUC ~0.41–0.45). Reliable within-scenario fork prediction is not claimed where evidence does not support it.
   - **Evaluation on independent random seeds under known scenario settings:** Confirmed for Seed 3 within observed parameter envelopes; macro-level regime classification is stable across seeds, but this does not imply reliable within-scenario prediction under calm conditions.
   - **Evaluation on entirely unseen scenarios through LOSO:** Zero-shot generalization to unobserved scenario regimes is not supported without domain-adaptive retraining due to severe out-of-distribution feature shift.
4. **Reconciled Test Suite Counts:**
   - Source code scan: exactly **136 top-level `func Test...` declarations** across 13 test files in 10 test-bearing packages.
   - Execution count: `go test -v ./... -count=1` executes all **136 top-level tests** and **29 subtests**, totaling **165 executed test cases** with **0 failures**.
5. **Strict Model Schema Compatibility (`v1.3`):**
   - Both `LoadLogisticRegression` and `LoadRandomForest` enforce `FeatureSchemaVersion == "v1.3"`. Older, empty, or incompatible versions are rejected at load time.
6. **Release Packaging:**
   - Packaged and verified into `forkrisk_milestone3_final_release.zip`.

---

## 2. Dataset and Split Verification

| Metric | Value | Verification Notes |
|---|---|---|
| **Authoritative Runs** | 24 | Loaded from `outputs/pilot_final/pilot_summary.csv` |
| **Excluded Runs** | 0 | All 24 pilot runs have complete events.jsonl and metadata.json |
| **Total Rows** | 4,258 | Exactly one row per eligible unique block height ($t \in (10, N-3]$) |
| **Positive Labels** | 2,295 (53.90%) | Ground-truth new fork birth in $(t, t+H]$ |
| **Negative Labels** | 1,963 (46.10%) | No new fork birth in $(t, t+H]$ |
| **Dataset File** | `outputs/ml/dataset/fork_risk_dataset.csv` | Exactly **28 columns** (5 metadata, 22 features, 1 target label) |
| **Dataset SHA-256** | `62a1d17ae308ab7279343dd5f001329abae805a1d17503619b4660ca3251bee5` | Verified against CSV byte stream |
| **Feature Schema Version** | `v1.3` | Incremented following churn feature and window corrections |
| **Dataset Version** | `1.3` | Recorded in `outputs/ml/dataset/dataset_metadata.json` |
| **Split Strategy** | `seed_held_out` | Complete runs partitioned by seed |
| **Train Split** | 8 runs, 1,565 rows (51.82% pos) | Seed 1 across all 8 scenarios |
| **Validation Split** | 8 runs, 1,085 rows (61.38% pos) | Seed 2 across all 8 scenarios (threshold tuning) |
| **Test Split** | 8 runs, 1,608 rows (50.87% pos) | Seed 3 across all 8 scenarios (held-out test) |
| **Run-ID Overlap** | **0 runs (0.0%)** | Strict disjoint partition: $\text{Train} \cap \text{Val} = \emptyset$, $\text{Train} \cap \text{Test} = \emptyset$, $\text{Val} \cap \text{Test} = \emptyset$ |

---

## 3. Corrected Churn Feature Distributions Across Scenarios

| Scenario | Rows | `peer_connectivity` [min / avg / max] | `churn_rate_100` [min / avg / max] | Notes |
|---|---|---|---|---|
| `baseline` | 476 | [0.80 / 0.991 / 1.00] | [0.000 / 0.217 / 0.690] | 99% uptime; occasional 1-node drop |
| `block_rate_stress` | 1486 | [0.80 / 0.990 / 1.00] | [0.000 / 0.091 / 0.460] | 99% uptime; 0.2s interval compresses events/block |
| `churn_stress` | 84 | [0.10 / 0.493 / 0.90] | [5.182 / 12.868 / 16.606] | 50% uptime; intense churn toggling |
| `combined_stress` | 570 | [0.80 / 0.991 / 1.00] | [0.000 / 0.230 / 0.440] | 99% uptime |
| `dynamic_network` | 440 | [0.80 / 0.991 / 1.00] | [0.000 / 0.220 / 0.690] | 99% uptime |
| `latency_stress` | 426 | [0.80 / 0.990 / 1.00] | [0.000 / 0.282 / 0.680] | 99% uptime |
| `propagation_stress` | 272 | [0.80 / 0.991 / 1.00] | [0.000 / 0.436 / 0.922] | 99% uptime |
| `tx_load_stress` | 504 | [0.80 / 0.990 / 1.00] | [0.000 / 0.236 / 0.650] | 99% uptime |

---

## 4. Model Training & Evaluation Metrics (Schema v1.3)

Thresholds were tuned on the validation split ($\tau^*_{\text{LR}} = 0.30$, $\tau^*_{\text{RF}} = 0.40$) without observing any test split samples.

### Held-Out Test Set Performance (Seed 3 runs, 1,608 samples, Prevalence: 50.87%)

| Model | ROC-AUC | PR-AUC | F1 Score | Precision | Recall | Brier Score | Tuned Threshold |
|---|---|---|---|---|---|---|---|
| **Prevalence Baseline** | 0.5000 | 0.5087 | 0.6744 | 0.5087 | 1.0000 | 0.2500 | 0.50 |
| **Heuristic Baseline** | 0.5842 | 0.5546 | 0.7135 | 0.5546 | 1.0000 | 0.2851 | 0.50 |
| **Logistic Regression** | **0.8124** | **0.7817** | **0.7888** | 0.6956 | 0.9108 | **0.1752** | 0.30 |
| **Random Forest** | **0.8139** | **0.7920** | **0.7676** | 0.6521 | 0.9328 | **0.1788** | 0.40 |

### Scenario-Level Breakdown on Held-Out Test Split (Seed 3)

| Scenario | Test Samples | Prevalence | LR ROC-AUC | LR F1 | RF ROC-AUC | RF F1 |
|---|---|---|---|---|---|---|
| `baseline` | 191 | 7.33% | 0.4253 | 0.0000 | 0.4104 | 0.0870 |
| `block_rate_stress` | 579 | 71.33% | 0.5640 | 0.8280 | 0.5818 | 0.8154 |
| `churn_stress` | 26 | 100.00% | **N/A** | 1.0000 | **N/A** | 0.9388 |
| `combined_stress` | 188 | 79.26% | 0.7859 | 0.8843 | 0.7981 | 0.8843 |
| `dynamic_network` | 177 | 23.16% | 0.7256 | 0.4396 | 0.7500 | 0.5077 |
| `latency_stress` | 165 | 56.36% | 0.5282 | 0.6612 | 0.5015 | 0.7209 |
| `propagation_stress` | 91 | 74.73% | 0.3606 | 0.8553 | 0.6387 | 0.8553 |
| `tx_load_stress` | 191 | 7.33% | 0.4520 | 0.0000 | 0.4566 | 0.0889 |

> **Single-Class Subset Handling (ROC-AUC Mathematical Definition):**
> For scenario `churn_stress`, the held-out test split (Seed 3) contains 26 samples with 100.00% positive prevalence (26 positive, 0 negative).
> ROC-AUC measures the probability of correctly ranking a positive instance above a negative instance. Without negative instances, the denominator is zero, rendering ROC-AUC mathematically undefined.
> The evaluation framework explicitly marks ROC-AUC as `N/A` (and serializes `"roc_auc": null` in `metrics.json`), rejecting arbitrary numerical constants such as 0.5.

### Analysis of Within-Scenario vs. Pooled Aggregate Performance

A critical distinction must be drawn between **performance aggregated across the full held-out test split** and **performance within individual scenarios**:

1. **Pooled Performance Across the Eight Modeled Scenarios:**
   - In the pooled Seed 3 test set (1,608 samples across 8 scenarios), models exhibit strong aggregate discrimination (LR ROC-AUC **0.8124**, RF ROC-AUC **0.8139**).
   - However, this aggregate metric strongly benefits from **cross-scenario prevalence differences**. Because baseline feature distributions (e.g., low latency, normal block intervals) coincide with low fork prevalence (~7.3%) while stress scenarios coincide with high fork prevalence (>70%), the models easily separate calm operational regimes from stressed regimes.
   - **Crucial Limitation:** Pooled ROC-AUC around 0.81 does **not** imply reliable within-scenario prediction. While high pooled ROC-AUC demonstrates that the models effectively distinguish high-risk operating regimes from low-risk operating regimes across the network's parameter space, it must not be conflated with uniform predictive accuracy within individual regimes.

2. **Performance Within Individual Scenarios:**
   - When evaluating within individual scenarios under fixed parameter envelopes, predictive discrimination is uneven:
     - **Dynamic & combined stress:** On `dynamic_network` (LR ROC-AUC 0.7256, RF 0.7500) and `combined_stress` (LR 0.7859, RF 0.7981), causal features effectively anticipate upcoming fork births.
     - **Moderate discrimination:** On `block_rate_stress` (LR 0.5640, RF 0.5818) and `latency_stress` (LR 0.5282, RF 0.5015), discrimination is modest.
     - **Sub-chance / poor discrimination on low-prevalence regimes:** Baseline and transaction-load stress have Logistic Regression ROC-AUC values of **0.4253** and **0.4520**, respectively, with F1 of **0.0000** in both subsets at the selected threshold (RF achieves F1 of 0.0870 and 0.0889, respectively, with ROC-AUC ~0.41–0.45). Under fixed baseline conditions, rare stochastic fork events are driven by micro-delays that cannot be reliably separated from non-fork proposals using global thresholds, resulting in zero positive predictions at the tuned decision threshold.
     - **Single-class subset:** On `churn_stress` (100.00% positive in Seed 3), ROC-AUC is mathematically undefined (`N/A`) due to the absence of negative instances.
   - Therefore, claims of reliable within-scenario fork prediction are **unsupported** for low-prevalence regimes like baseline and transaction load stress.

### Confusion Matrices (Held-Out Test Set: 818 Positives, 790 Negatives)

- **Logistic Regression ($\tau = 0.30$):**
  $$\begin{pmatrix} \text{TN} = 464 & \text{FP} = 326 \\ \text{FN} = 73 & \text{TP} = 745 \end{pmatrix} \implies \text{Accuracy} = \frac{464 + 745}{1608} = 75.19\%$$

- **Random Forest ($\tau = 0.40$):**
  $$\begin{pmatrix} \text{TN} = 383 & \text{FP} = 407 \\ \text{FN} = 55 & \text{TP} = 763 \end{pmatrix} \implies \text{Accuracy} = \frac{383 + 763}{1608} = 71.27\%$$

---

## 5. Independent Fork-Label Reference Audit Findings (`cmd/labelaudit`)

The fork-label ground truth was subjected to a comprehensive independent reference audit:
1. **Independent Implementation:** An independent reference labeler function was developed strictly from the formal $H=3$ specification without depending on `internal/features.Labeler`.
2. **Equivalent Detector Configuration:** Both reference and production labelers evaluate detection results generated with `warmUpBlocks = 10`, matching the exact dataset generation pipeline settings.
3. **Formal Specification Constraints:**
   - Evaluates fork births: $ep.\text{BirthTime} > T_h$ and $ep.\text{BirthHeight} \in (h, h+H]$.
   - Rejects episode continuation: ongoing forks extending across blocks are not counted as new births.
   - Respects forecast boundaries: excludes warm-up ($h \le 10$) and right-censored intervals ($h > N - 3$).
   - Respects unresolved episodes: fork births unresolved at simulation end are validly counted.
   - Hand-constructed test episodes verified in `cmd/labelaudit/main_test.go` and `internal/features/features_test.go`.

### Audit Scope Clarification
> **Important Distinction:** This audit independently checks the proposal-level label calculation against the mathematical $H=3$ specification, given a set of detected fork episodes. Because both the reference labeler and the production labeler consume `ForkEpisode` records produced by `fork.Detector`, this audit validates label assignment logic, boundary censoring, continuation rejection, and unresolved episode handling. It does **not** independently validate the internal event-to-fork-episode detection logic of `fork.Detector` itself.

### Audit Results Across All 24 Authoritative Pilot Runs:
- **Total Post-Warmup Fork Episodes:** 1,498
- **Unresolved Fork Episodes:** 1
- **Episodes with BirthHeight != Height:** 0
- **Episodes with Duration > 2.0s:** 84
- **Total Prediction Points Audited:** 4,258
- **Reference vs Production Label Differences:** **0**
- **Reference vs Dataset CSV Matches:** **4,258 / 4,258 (100.0% exact match)**
- **Reference vs Dataset CSV Mismatches:** **0**
- **Production vs Dataset CSV Mismatches:** **0**
- **Old Naive Continuation Differences in Pilot:** **0**

### Scientific Explanation for Unchanged Dataset Labels:
In this discrete-event blockchain model, competing blocks are proposed at the same height $h$. The fork episodes in the 24 pilot runs resolved rapidly (typically within 1–2 block intervals) through tie-breaking and longest-chain adoption before reaching height $h+4$. Under severe stress, new competing forks were born at subsequent heights. Because fork episodes did not bridge across $>3$ block heights without new competing births at those heights, $ep.\text{Height}$ and $ep.\text{BirthHeight}$ mapped into identical $H=3$ forecast windows across all 4,258 rows. Therefore, zero label discrepancies exist between specification, implementation, and stored dataset.

---

## 6. Supplementary Leave-One-Scenario-Out (LOSO) Evaluation

| Held-Out Scenario | Train Samples | Test Samples | Test Prevalence | ROC-AUC | PR-AUC | F1 Score | Brier Score |
|---|---|---|---|---|---|---|---|
| `baseline` | 3,782 | 476 | 10.71% | 0.6669 | 0.2153 | 0.0000 | 0.0934 |
| `block_rate_stress` | 2,772 | 1,486 | 71.60% | 0.5956 | 0.7739 | 0.2678 | 0.5608 |
| `churn_stress` | 4,174 | 84 | 59.52% | 0.6200 | 0.6794 | 0.7463 | 0.4048 |
| `combined_stress` | 3,688 | 570 | 85.26% | 0.5840 | 0.8848 | 0.9205 | 0.1430 |
| `dynamic_network` | 3,818 | 440 | 23.41% | 0.7478 | 0.4470 | 0.5847 | 0.1530 |
| `latency_stress` | 3,832 | 426 | 62.44% | 0.4912 | 0.6312 | 0.7640 | 0.2819 |
| `propagation_stress` | 3,986 | 272 | 82.72% | 0.5057 | 0.8210 | 0.8407 | 0.5313 |
| `tx_load_stress` | 3,754 | 504 | 9.92% | 0.5420 | 0.1001 | 0.1805 | 0.8034 |

### Methodological Evaluation Dimensions

1. **Pooled Performance Across the Eight Modeled Scenarios:**
   - In pooled evaluation (Seed 3, 1,608 samples), models achieve ROC-AUC > 0.81 and PR-AUC > 0.78, indicating strong overall separation between high-risk and low-risk windows across the entire parameter envelope. However, pooled ROC-AUC around 0.81 does not imply reliable within-scenario prediction, as it strongly benefits from cross-scenario prevalence separation.
2. **Performance Within Individual Scenarios:**
   - Within dynamic and combined stress scenarios, predictive signals provide actionable advance warning (ROC-AUC 0.72–0.80).
   - Within calm, low-prevalence regimes (`baseline` and `tx_load_stress`, both 7.33% prevalence), within-scenario discrimination is poor: Logistic Regression achieves ROC-AUC of **0.4253** on baseline and **0.4520** on transaction-load stress, with F1 of **0.0000** in both subsets at the selected threshold (RF achieves F1 ~0.088 with ROC-AUC ~0.41–0.45). Reliable within-scenario fork prediction is not supported for these calm regimes.
3. **Evaluation on Independent Random Seeds Under Known Scenario Settings:**
   - Reserving Seed 3 tests generalization to independent stochastic realizations within known scenario envelopes; models reliably classify operational regimes across seeds, but this does not imply reliable within-scenario prediction under calm conditions.
4. **Evaluation on Entirely Unseen Scenarios Through LOSO:**
   - Withholding an entire operating regime exposes models to severe out-of-distribution feature shift. Zero-shot generalization to unobserved network regimes is not supported without domain-adaptive calibration.

---

## 7. Automated Test Suite Reconciliation

A comprehensive verification of all test files across the repository confirms:
- **Top-Level Test Functions in Source:** Exactly **136** `func Test...` declarations across 13 test files in 10 test-bearing packages:
  * `cmd/labelaudit/main_test.go`: 5 tests
  * `internal/config/config_test.go`: 14 tests
  * `internal/engine/churn_test.go`: 6 tests
  * `internal/engine/engine_test.go`: 12 tests
  * `internal/engine/regression_test.go`: 14 tests
  * `internal/features/features_test.go`: 14 tests
  * `internal/fork/fork_test.go`: 18 tests
  * `internal/logstore/logger_test.go`: 2 tests
  * `internal/metrics/metrics_test.go`: 13 tests
  * `internal/ml/ml_test.go`: 8 tests
  * `internal/node/block_test.go`: 6 tests
  * `internal/node/node_test.go`: 10 tests
  * `tests/simulate_test.go`: 14 tests
- **Executed Test Cases:** `go test -v ./... -count=1` executes all **136 top-level tests** plus **29 subtests**, totaling **165 executed test cases**.
- **Test Failures:** **0 failures** (all 165 test cases PASS cleanly).
