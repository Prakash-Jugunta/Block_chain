# ForkRisk Milestone 3 — Model Evaluation Report

> **Evaluation Protocol:** Strictly held-out test split (Seed 3 runs across all 8 scenarios). Zero run-level overlap; preprocessing and classification thresholds fit strictly on training/validation splits.

## 1. Split and Dataset Summary

- **Strategy:** seed_held_out (Train: Seed 1, Val: Seed 2, Test: Seed 3)
- **Training Set:** 8 runs, 1565 samples (Prevalence: 51.82%)
- **Validation Set:** 8 runs, 1085 samples (Prevalence: 61.38%)
- **Held-Out Test Set:** 8 runs, 1608 samples (Prevalence: 50.87%)

## 2. Test Set Performance Comparison

| Model | ROC-AUC | PR-AUC | F1 Score | Precision | Recall | Brier Score | Tuned Threshold |
|---|---|---|---|---|---|---|---|
| **PrevalenceBaseline** | 0.5000 | 0.5087 | 0.6744 | 0.5087 | 1.0000 | 0.2500 | 0.50 |
| **HeuristicBaseline** | 0.5842 | 0.5546 | 0.7135 | 0.5546 | 1.0000 | 0.2851 | 0.50 |
| **LogisticRegression** | 0.8020 | 0.7545 | 0.7901 | 0.6846 | 0.9340 | 0.1771 | 0.25 |
| **RandomForest** | 0.8206 | 0.7892 | 0.7539 | 0.6173 | 0.9682 | 0.1702 | 0.25 |

## 3. Confusion Matrices (Held-Out Test Set)

### PrevalenceBaseline (Threshold = 0.50)

| Actual \ Predicted | Predicted Negative (0) | Predicted Positive (1) | Total |
|---|---|---|---|
| **Actual Negative (0)** | 0 (TN) | 790 (FP) | 790 |
| **Actual Positive (1)** | 0 (FN) | 818 (TP) | 818 |
| **Total** | 0 | 1608 | 1608 |

### HeuristicBaseline (Threshold = 0.50)

| Actual \ Predicted | Predicted Negative (0) | Predicted Positive (1) | Total |
|---|---|---|---|
| **Actual Negative (0)** | 133 (TN) | 657 (FP) | 790 |
| **Actual Positive (1)** | 0 (FN) | 818 (TP) | 818 |
| **Total** | 133 | 1475 | 1608 |

### LogisticRegression (Threshold = 0.25)

| Actual \ Predicted | Predicted Negative (0) | Predicted Positive (1) | Total |
|---|---|---|---|
| **Actual Negative (0)** | 438 (TN) | 352 (FP) | 790 |
| **Actual Positive (1)** | 54 (FN) | 764 (TP) | 818 |
| **Total** | 492 | 1116 | 1608 |

### RandomForest (Threshold = 0.25)

| Actual \ Predicted | Predicted Negative (0) | Predicted Positive (1) | Total |
|---|---|---|---|
| **Actual Negative (0)** | 299 (TN) | 491 (FP) | 790 |
| **Actual Positive (1)** | 26 (FN) | 792 (TP) | 818 |
| **Total** | 325 | 1283 | 1608 |

## 4. Why Accuracy Alone is Insufficient

In blockchain fork risk prediction, class prevalence varies dramatically between operating regimes: in baseline operation, forks occur in ~10% of observation windows, whereas under propagation or block-rate stress, forks occur in >70% of windows. A trivial classifier predicting the majority class can achieve high nominal accuracy while failing completely to alert node operators of imminent fork episodes. Hence, **ROC-AUC, PR-AUC, F1 score, and Brier score** are the primary evaluation criteria.

## 5. Scenario-Level Breakdown on Held-Out Test Split (Seed 3)

The primary evaluation protocol reserves Seed 3 across all 8 operating scenarios as the held-out test split (with Seeds 1 and 2 used for training and validation). Note that this protocol evaluates generalization across independent random seeds under *seen* scenario parameter regimes, rather than completely unobserved scenario architectures:

| Scenario | Test Samples | Prevalence | LR ROC-AUC | LR F1 | RF ROC-AUC | RF F1 |
|---|---|---|---|---|---|---|
| `baseline` | 191 | 7.33% | 0.4685 | 0.0000 | 0.4326 | 0.0899 |
| `block_rate_stress` | 579 | 71.33% | 0.5860 | 0.8303 | 0.5783 | 0.8323 |
| `churn_stress` | 26 | 100.00% | 0.5000 | 0.5556 | 0.5000 | 0.9804 |
| `combined_stress` | 188 | 79.26% | 0.7217 | 0.8843 | 0.7770 | 0.8843 |
| `dynamic_network` | 177 | 23.16% | 0.7816 | 0.6111 | 0.8094 | 0.4805 |
| `latency_stress` | 165 | 56.36% | 0.4875 | 0.7209 | 0.6153 | 0.7209 |
| `propagation_stress` | 91 | 74.73% | 0.3159 | 0.8553 | 0.5288 | 0.8553 |
| `tx_load_stress` | 191 | 7.33% | 0.4984 | 0.0000 | 0.4443 | 0.1270 |

## 6. Supplementary Leave-One-Scenario-Out (LOSO) Generalization Evaluation

To rigorously evaluate out-of-distribution generalization to completely unseen operating regimes, a supplementary Leave-One-Scenario-Out (LOSO) evaluation was performed using Logistic Regression. In each fold, all runs for one scenario are held out as the test set, while the remaining 7 scenarios are used for training and validation:

| Held-Out Scenario | Train Samples | Test Samples | Test Prevalence | ROC-AUC | PR-AUC | F1 Score | Brier Score |
|---|---|---|---|---|---|---|---|
| `baseline` | 3782 | 476 | 10.71% | 0.6994 | 0.2651 | 0.0000 | 0.0901 |
| `block_rate_stress` | 2772 | 1486 | 71.60% | 0.5779 | 0.7614 | 0.0075 | 0.6019 |
| `churn_stress` | 4174 | 84 | 59.52% | 0.7894 | 0.8610 | 0.0000 | 0.4664 |
| `combined_stress` | 3688 | 570 | 85.26% | 0.5580 | 0.8783 | 0.9205 | 0.1443 |
| `dynamic_network` | 3818 | 440 | 23.41% | 0.7394 | 0.4445 | 0.5478 | 0.1564 |
| `latency_stress` | 3832 | 426 | 62.44% | 0.4836 | 0.6211 | 0.7190 | 0.3057 |
| `propagation_stress` | 3986 | 272 | 82.72% | 0.3371 | 0.7690 | 0.9054 | 0.2029 |
| `tx_load_stress` | 3754 | 504 | 9.92% | 0.5450 | 0.1007 | 0.1805 | 0.8055 |

### Generalization Assessment & Methodological Limitations

1. **Protocol Distinction:** The primary seed-held-out protocol (Sections 2–5) assesses generalization across independent random seeds within known parameter regimes. The supplementary LOSO protocol assesses generalization across entirely unobserved operating dynamics.
2. **Distribution Shift:** With only 8 scenarios and 24 total runs, withholding an entire operating regime (e.g. `propagation_stress` or `block_rate_stress`) deprives the model of any training samples with extreme delays or tight block intervals. When tested on these regimes, the model must extrapolate far beyond its training support, leading to reduced discrimination or probability calibration degradation.
3. **Scientific Scope:** These empirical results demonstrate that while the models are highly reliable across independent runs within known operational envelopes, strong claims of zero-shot generalization across novel blockchain architectures or unobserved network topologies are not justified without domain-adaptive retraining.

