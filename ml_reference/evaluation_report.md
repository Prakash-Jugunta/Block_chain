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
| **LogisticRegression** | 0.8124 | 0.7817 | 0.7888 | 0.6956 | 0.9108 | 0.1752 | 0.30 |
| **RandomForest** | 0.8139 | 0.7920 | 0.7676 | 0.6521 | 0.9328 | 0.1788 | 0.40 |

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

### LogisticRegression (Threshold = 0.30)

| Actual \ Predicted | Predicted Negative (0) | Predicted Positive (1) | Total |
|---|---|---|---|
| **Actual Negative (0)** | 464 (TN) | 326 (FP) | 790 |
| **Actual Positive (1)** | 73 (FN) | 745 (TP) | 818 |
| **Total** | 537 | 1071 | 1608 |

### RandomForest (Threshold = 0.40)

| Actual \ Predicted | Predicted Negative (0) | Predicted Positive (1) | Total |
|---|---|---|---|
| **Actual Negative (0)** | 383 (TN) | 407 (FP) | 790 |
| **Actual Positive (1)** | 55 (FN) | 763 (TP) | 818 |
| **Total** | 438 | 1170 | 1608 |

## 4. Why Accuracy Alone is Insufficient

In blockchain fork risk prediction, class prevalence varies dramatically between operating regimes: in baseline operation, forks occur in ~10% of observation windows, whereas under propagation or block-rate stress, forks occur in >70% of windows. A trivial classifier predicting the majority class can achieve high nominal accuracy while failing completely to alert node operators of imminent fork episodes. Hence, **ROC-AUC, PR-AUC, F1 score, and Brier score** are the primary evaluation criteria.

## 5. Scenario-Level Breakdown on Held-Out Test Split (Seed 3)

The primary evaluation protocol reserves Seed 3 across all 8 operating scenarios as the held-out test split (with Seeds 1 and 2 used for training and validation). Note that this protocol evaluates generalization across independent random seeds under *seen* scenario parameter regimes, rather than completely unobserved scenario architectures:

| Scenario | Test Samples | Prevalence | LR ROC-AUC | LR F1 | RF ROC-AUC | RF F1 |
|---|---|---|---|---|---|---|
| `baseline` | 191 | 7.33% | 0.4253 | 0.0000 | 0.4104 | 0.0870 |
| `block_rate_stress` | 579 | 71.33% | 0.5640 | 0.8280 | 0.5818 | 0.8154 |
| `churn_stress` | 26 | 100.00% | N/A | 1.0000 | N/A | 0.9388 |
| `combined_stress` | 188 | 79.26% | 0.7859 | 0.8843 | 0.7981 | 0.8843 |
| `dynamic_network` | 177 | 23.16% | 0.7256 | 0.4396 | 0.7500 | 0.5077 |
| `latency_stress` | 165 | 56.36% | 0.5282 | 0.6612 | 0.5015 | 0.7209 |
| `propagation_stress` | 91 | 74.73% | 0.3606 | 0.8553 | 0.6387 | 0.8553 |
| `tx_load_stress` | 191 | 7.33% | 0.4520 | 0.0000 | 0.4566 | 0.0889 |

> **Single-Class Subset Note (ROC-AUC Mathematical Definition):**
> For scenario `churn_stress`, the held-out test split (Seed 3) contains 26 samples with 100.00% positive prevalence (26 positive, 0 negative).
> ROC-AUC measures the probability that a randomly chosen positive instance is ranked higher than a randomly chosen negative instance.
> In the absence of negative examples, the false positive rate denominator is 0, making ROC-AUC mathematically undefined.
> Accordingly, ROC-AUC is formally marked `N/A` (and serialized as `null` in `metrics.json`), rather than assigned an arbitrary default value such as 0.5.

### Analysis of Within-Scenario vs. Pooled Aggregate Performance

A critical distinction must be drawn between **performance aggregated across the full held-out test split** and **performance within individual scenarios**, structured across four methodological evaluation dimensions:

1. **Pooled Performance Across the Eight Modeled Scenarios:**
   - In the pooled Seed 3 test set (1,608 samples across 8 scenarios), the models exhibit strong aggregate discrimination (LR ROC-AUC 0.8124, RF ROC-AUC 0.8139, PR-AUC 0.7817 and 0.7920).
   - However, this aggregate metric strongly benefits from **cross-scenario prevalence differences**. Because baseline feature distributions (e.g., low latency, normal block intervals) coincide with low fork prevalence (~7.3%) while stress scenarios coincide with high fork prevalence (>70%), the models easily separate calm operational regimes from stressed regimes.
   - **Crucial Limitation:** Pooled ROC-AUC around 0.81 does not imply reliable within-scenario prediction. While high pooled ROC-AUC demonstrates that the models effectively distinguish high-risk operating regimes from low-risk operating regimes across the network's parameter space, it must not be conflated with uniform predictive accuracy within individual regimes.

2. **Performance Within Individual Scenarios:**
   - When evaluating within individual scenarios under fixed parameter envelopes, predictive discrimination is uneven:
     - **Dynamic & combined stress:** On `dynamic_network` (LR ROC-AUC 0.7256, RF 0.7500) and `combined_stress` (LR 0.7859, RF 0.7981), causal features effectively anticipate upcoming fork births within the scenario.
     - **Moderate discrimination:** On `block_rate_stress` (LR 0.5640, RF 0.5818) and `latency_stress` (LR 0.5282, RF 0.5015), discrimination is modest.
     - **Sub-chance / poor discrimination on low-prevalence regimes:** Baseline and transaction-load stress have Logistic Regression ROC-AUC values of **0.4253** and **0.4520**, respectively, with F1 of **0.0000** in both subsets at the selected threshold (RF achieves F1 of 0.0870 and 0.0889, respectively, with ROC-AUC ~0.41–0.45). Under fixed baseline conditions, rare stochastic fork events are driven by micro-delays that cannot be reliably separated from non-fork proposals using global thresholds, resulting in zero positive predictions at the tuned decision threshold.
     - **Single-class subset:** On `churn_stress` (100.00% positive in Seed 3), ROC-AUC is mathematically undefined (`N/A`) due to the absence of negative instances.
   - Therefore, claims of reliable within-scenario fork prediction are **unsupported** for low-prevalence regimes like baseline and transaction load stress.

3. **Evaluation on Independent Random Seeds Under Known Scenario Settings:**
   - Reserving Seed 3 tests generalization to independent random realizations when operating scenario parameters are observed during training. Under this seed-held-out protocol, models reliably classify macro-level operational regimes across seeds (pooled ROC-AUC > 0.81). However, as shown above, this macro-level seed stability does not imply reliable within-scenario prediction under calm regimes.

4. **Evaluation on Entirely Unseen Scenarios (LOSO):**
   - When an entire scenario regime is withheld from training (Section 6), models experience severe out-of-distribution feature shifts, and performance drops significantly. Zero-shot generalization to unobserved scenario dynamics is not supported without domain-adaptive retraining.

## 6. Supplementary Leave-One-Scenario-Out (LOSO) Generalization Evaluation

To rigorously evaluate out-of-distribution generalization to completely unseen operating regimes, a supplementary Leave-One-Scenario-Out (LOSO) evaluation was performed using Logistic Regression. In each fold, all runs for one scenario are held out as the test set, while the remaining 7 scenarios are used for training and validation:

| Held-Out Scenario | Train Samples | Test Samples | Test Prevalence | ROC-AUC | PR-AUC | F1 Score | Brier Score |
|---|---|---|---|---|---|---|---|
| `baseline` | 3782 | 476 | 10.71% | 0.6669 | 0.2153 | 0.0000 | 0.0934 |
| `block_rate_stress` | 2772 | 1486 | 71.60% | 0.5956 | 0.7739 | 0.2678 | 0.5608 |
| `churn_stress` | 4174 | 84 | 59.52% | 0.6200 | 0.6794 | 0.7463 | 0.4048 |
| `combined_stress` | 3688 | 570 | 85.26% | 0.5840 | 0.8848 | 0.9205 | 0.1430 |
| `dynamic_network` | 3818 | 440 | 23.41% | 0.7478 | 0.4470 | 0.5847 | 0.1530 |
| `latency_stress` | 3832 | 426 | 62.44% | 0.4912 | 0.6312 | 0.7640 | 0.2819 |
| `propagation_stress` | 3986 | 272 | 82.72% | 0.5057 | 0.8210 | 0.8407 | 0.5313 |
| `tx_load_stress` | 3754 | 504 | 9.92% | 0.5420 | 0.1001 | 0.1805 | 0.8034 |

### Generalization Assessment & Methodological Limitations

1. **Protocol Distinction:** The primary seed-held-out protocol (Sections 2–5) assesses generalization across independent random seeds within known parameter regimes. The supplementary LOSO protocol assesses generalization across entirely unobserved operating dynamics.
2. **Aggregated vs. Within-Scenario Nuance:** High aggregate performance (pooled ROC-AUC > 0.81) across pooled test data largely reflects cross-regime risk stratification driven by prevalence differences rather than uniform within-scenario accuracy. Crucially, pooled ROC-AUC around 0.81 does not imply reliable within-scenario prediction: baseline and transaction-load stress have Logistic Regression ROC-AUC values of 0.4253 and 0.4520, respectively, with F1 of zero at the selected threshold.
3. **Distribution Shift:** With only 8 scenarios and 24 total runs, withholding an entire operating regime (e.g. `propagation_stress` or `block_rate_stress`) deprives the model of any training samples with extreme delays or tight block intervals. When tested on these regimes, the model must extrapolate far beyond its training support, leading to reduced discrimination or probability calibration degradation.
4. **Scientific Scope:** While the models provide a useful macro-level risk indicator across diverse network conditions within known operational envelopes, claims of reliable within-scenario prediction across all regimes or zero-shot generalization across novel blockchain architectures are not justified without domain-adaptive retraining.

