# ForkRisk Milestone 3 — Feature Distribution & Drift Analysis Report

> **Methodology:** Observational drift monitoring comparing feature distributions, target prevalence, and model performance across simulated operating regimes and dynamic temporal phases. Models are never retrained on test data or future outcomes.

## 1. Operating Regime Overview

| Scenario | Total Samples | Fork Prevalence (H=3) | Latency Mean (s) | Prop Delay Mean (s) | Peer Connectivity | Avg Block Interval (s) | Mempool Backlog |
|---|---|---|---|---|---|---|---|
| `baseline` | 476 | 10.71% | 0.3063 ± 0.0212 | 0.4841 ± 0.0478 | 0.99 | 1.0028 | 0.0 |
| `block_rate_stress` | 1486 | 71.60% | 0.2767 ± 0.0589 | 0.4056 ± 0.0990 | 0.99 | 0.2001 | 0.0 |
| `churn_stress` | 84 | 59.52% | 0.0125 ± 0.0537 | 0.0158 ± 0.0760 | 0.49 | 0.9993 | 0.0 |
| `combined_stress` | 570 | 85.26% | 1.2113 ± 0.1416 | 1.4308 ± 0.2220 | 0.99 | 0.3007 | 0.0 |
| `dynamic_network` | 440 | 23.41% | 0.4582 ± 0.2402 | 0.7081 ± 0.3527 | 0.99 | 1.0031 | 0.0 |
| `latency_stress` | 426 | 62.44% | 0.7970 ± 0.0460 | 1.2444 ± 0.0910 | 0.99 | 1.0024 | 0.0 |
| `propagation_stress` | 272 | 82.72% | 1.9812 ± 0.1223 | 2.8126 ± 0.2883 | 0.99 | 1.0050 | 0.0 |
| `tx_load_stress` | 504 | 9.92% | 0.3078 ± 0.0203 | 0.4873 ± 0.0432 | 0.99 | 1.0020 | 11959.2 |

## 2. Key Observations on Feature Shift

1. **Propagation Delay & Latency Shift:** Under `latency_stress` and `propagation_stress`, observed mean propagation time surges from baseline, directly escalating fork birth prevalence.
   - Baseline: 10.71% prevalence, 0.4841s mean prop time
   - Latency Stress: 62.44% prevalence, 1.2444s mean prop time
   - Propagation Stress: 82.72% prevalence, 2.8126s mean prop time
2. **Block Rate Compression:** Under `block_rate_stress`, proposal intervals drop from ~1.00s to ~0.20s while propagation delay remains at ~0.45s (exceeding the block interval), leading to 71.60% fork prevalence.
3. **Peer Churn Degradation:** Under `churn_stress`, peer connectivity drops to ~0.49, inducing localized network partitioning and 59.52% positive fork rate.
4. **Transaction Backlog Decoupling:** In `tx_load_stress`, mempool backlog grows substantially, while fork prevalence (9.92%) mirrors baseline (10.71%). This confirms the documented modeling constraint: mempool backlog does not causally amplify propagation delay in the current simulator.

## 3. Dynamic Network Phase-Shift Analysis (`dynamic_network`)

The `dynamic_network` scenario introduces an abrupt latency shift mid-simulation at block 150 (from 50 ms to 300 ms, with 100 ms propagation delay throughout). The simulation runs for 300 seconds (yielding ~200 blocks total, with warm-up skipping the first 10 blocks; no blocks exist above ~200 and no 1,000 ms propagation phase was configured):

| Phase | Block Range | Configured State | Observed Latency (s) | Observed Prop Time (s) | Fork Prevalence (H=3) |
|---|---|---|---|---|---|
| **Phase 1 (Pre-Transition)** | Blocks 11–149 | Baseline Latency (50ms lat, 100ms prop) | 0.3922 ± 0.1964 | 0.6135 ± 0.2892 | 19.52% (374 samples) |
| **Phase 2 (Post-Transition)** | Blocks 150+ | Shifted Latency (300ms lat, 100ms prop) | 0.8319 ± 0.0429 | 1.2443 ± 0.1319 | 45.45% (66 samples) |

## 4. Test Set Performance Across Operating Regimes

| Scenario | Test Samples | Test Prevalence | LR F1 | LR ROC-AUC | RF F1 | RF ROC-AUC |
|---|---|---|---|---|---|---|
| `baseline` | 191 | 7.33% | 0.0000 | 0.4253 | 0.0870 | 0.4104 |
| `block_rate_stress` | 579 | 71.33% | 0.8280 | 0.5640 | 0.8154 | 0.5818 |
| `churn_stress` | 26 | 100.00% | 1.0000 | N/A | 0.9388 | N/A |
| `combined_stress` | 188 | 79.26% | 0.8843 | 0.7859 | 0.8843 | 0.7981 |
| `dynamic_network` | 177 | 23.16% | 0.4396 | 0.7256 | 0.5077 | 0.7500 |
| `latency_stress` | 165 | 56.36% | 0.6612 | 0.5282 | 0.7209 | 0.5015 |
| `propagation_stress` | 91 | 74.73% | 0.8553 | 0.3606 | 0.8553 | 0.6387 |
| `tx_load_stress` | 191 | 7.33% | 0.0000 | 0.4520 | 0.0889 | 0.4566 |

## 5. Architectural & Monitoring Conclusions

- The engineered rolling-window features (`latency_mean_10`, `prop_time_mean_10`, `competing_tips`, `node_view_disagreement`) respond rapidly to dynamic environmental shifts without introducing future information leakage.
- **Evidence-Based Generalization Distinction:**
  1. **Pooled performance across the eight modeled scenarios:** In the pooled held-out test split (Seed 3, 1,608 samples across 8 scenarios), models achieve ROC-AUC > 0.81 (LR 0.8124, RF 0.8139), demonstrating strong macro-level risk stratification across operating regimes. However, this pooled performance is significantly driven by cross-scenario prevalence differences (calm regimes ~7.3% vs. stress regimes >70%). Crucially, pooled ROC-AUC around 0.81 does not imply reliable within-scenario prediction.
  2. **Performance within individual scenarios:** Within calm, low-prevalence regimes (`baseline` and `tx_load_stress`, both 7.33% prevalence), within-scenario discrimination is near or below chance: Logistic Regression achieves ROC-AUC values of 0.4253 on baseline and 0.4520 on transaction-load stress, with F1 of 0.0000 in both subsets at the selected threshold (RF achieves F1 of ~0.088 with ROC-AUC ~0.41–0.45). While dynamic and combined stress scenarios exhibit actionable within-scenario predictability (ROC-AUC 0.72–0.80), reliable within-scenario fork prediction is not achieved under calm operating conditions.
  3. **Evaluation on independent random seeds under known scenario settings:** Models reliably classify macro-level operational regimes across independent seeds (Seed 3) within known parameter envelopes, but this does not imply reliable within-scenario prediction under calm conditions.
  4. **Evaluation on entirely unseen scenarios through LOSO:** Leave-One-Scenario-Out evaluation shows significant performance drops and severe out-of-distribution feature shift when withholding entire operating regimes, demonstrating that zero-shot generalization to unobserved network regimes is not supported without domain-adaptive retraining.

