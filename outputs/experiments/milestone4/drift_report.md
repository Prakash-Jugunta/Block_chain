# ForkRisk Milestone 4 — Sliding-Window Concept Drift Detector Report

> **Specification:** §6 of CASE_STUDY_DEMO_PLAN.md  
> **Evaluation Protocol:** Strictly validation-only parameter grid tuning ($W \in [50, 100, 200] \times \theta \in [1.5, 2.0, 2.5]\sigma$) ensuring stationary false positive rate $< 5\%$. Evaluated on independent test seeds (Seeds 4–30) for the dynamic network latency transition at global proposal index 5,000.

## 1. Selected Optimal Drift Detector Configuration

- **Selected Window Size ($W^*$):** 200 observations
- **Selected Threshold ($\theta^*$):** 2.0 standard deviations
- **Variance Floor ($\epsilon$):** 1.0e-06 (zero-variance guard)
- **Primary Monitored Feature:** `latency_mean_10` (rolling mean block propagation delay)

## 2. Validation Grid Tuning Results

| Window Size ($W$) | Threshold ($\theta$) | Stationary FPR | Transition Precision | Transition Recall | Detection F1 | Eligible (FPR < 5%) |
|---|---|---|---|---|---|---|
| 50 | 1.5σ | 15.03% | 0.5365 | 0.1470 | 0.2308 | No |
| 50 | 2.0σ | 5.85% | 0.5333 | 0.0640 | 0.1143 | No |
| 50 | 2.5σ | 2.00% | 0.6038 | 0.0320 | 0.0608 | Yes |
| 100 | 1.5σ | 12.95% | 0.6084 | 0.1600 | 0.2534 | No |
| 100 | 2.0σ | 4.89% | 0.6226 | 0.0660 | 0.1193 | Yes |
| 100 | 2.5σ | 1.53% | 0.7174 | 0.0330 | 0.0631 | Yes |
| 200 | 1.5σ | 11.17% | 0.7436 | 0.1740 | 0.2820 | No |
| 200 | 2.0σ | 3.33% | 0.8571 | 0.0780 | 0.1430 | Yes |
| 200 | 2.5σ | 1.06% | 0.9200 | 0.0460 | 0.0876 | Yes |

*Selection Rationale:* The $W=200, \theta=2.0\sigma$ configuration maximizes F1-score (0.1430) while strictly maintaining a stationary false positive rate below 5% (3.33% on validation data).

## 3. Independent Production Test Performance (Seeds 4–30)

### 3.1 Transition Detection and Delay Breakdown
- **Total Dynamic-Network Test Seeds:** 27 runs (Seeds 4–30)
- **Seeds Detecting Transition:** **27 / 27 (100.0%)**
- **Seeds Never Detecting Transition:** **0 / 27 (0.0%)**
- **Detection Point:** Every single seed triggered its first post-transition alarm at proposal **5,001** (exactly 1 proposal after the step change at proposal 5,000).
- **Delay Statistics (across 27 detected runs):**
  - Minimum Delay: 1 proposal
  - 25th Percentile ($P_{25}$): 1.0 proposals
  - Median Delay ($P_{50}$): 1.0 proposals
  - Mean Delay: **1.00 proposals**
  - 75th Percentile ($P_{75}$): 1.0 proposals
  - 95th Percentile ($P_{95}$): 1.0 proposals
  - Maximum Delay: 1 proposal

### 3.2 False-Positive Rate and Post-Transition Alarm Dynamics
- **Pre-Transition False Positive Rate (Proposals 1,000–4,999):** **4.33%** (935 false alarms across 21,600 stationary evaluation points, satisfying the $<5\%$ stationary bound).
- **Post-Transition Ongoing Alarm Rate (Proposals 5,000–9,999):** **7.86%** (2,122 alarms across 27,000 evaluation points).

### 3.3 Mechanistic Explanation of the 7.86% Post-Transition Alarm Rate
The post-transition detection rate of 7.86% across the entire 5,000 post-transition proposals must not be confused with missed detection of the transition itself. The sliding-window detector operates as an adaptive rolling estimator:
1. At proposal 5,000, mean network latency steps abruptly from 50 ms to 300 ms.
2. At proposal 5,001, the incoming observation ($~300$ ms) is compared against the pre-transition reference window ($~50$ ms, $\sigma \approx 10$ ms), producing a Z-score $\approx 25\sigma$. This immediately triggers a drift alarm on **100% of runs** (mean delay = 1.0 proposal).
3. Over the subsequent $W=200$ proposals, as more 300 ms observations enter the sliding window, the reference window mean adapts upward toward 300 ms.
4. Once the sliding window is fully populated with 300 ms observations, the detector establishes a new baseline mean of 300 ms. In this new stationary regime, alarms are only triggered during normal statistical tail excursions exceeding $2.0\sigma$, which occurs at an empirical rate of 7.86%.
5. The sliding window is designed for continuous online monitoring rather than latching latch alarms; therefore, it resets its baseline once adapted to the new environment.

### 3.4 Per-Seed Detection Log (Seeds 4–30)

| Seed | Transition Detected | First Alarm Proposal | Detection Delay | Pre-Shift FPR (1k–5k) | Post-Shift Alarms (5k–10k) |
|:---:|:---:|:---:|:---:|:---:|:---:|
| 4 | Yes | 5,001 | 1 proposal | 24 / 800 (3.00%) | 82 / 1000 (8.20%) |
| 5 | Yes | 5,001 | 1 proposal | 52 / 800 (6.50%) | 73 / 1000 (7.30%) |
| 6 | Yes | 5,001 | 1 proposal | 43 / 800 (5.38%) | 81 / 1000 (8.10%) |
| 7 | Yes | 5,001 | 1 proposal | 54 / 800 (6.75%) | 82 / 1000 (8.20%) |
| 8 | Yes | 5,001 | 1 proposal | 19 / 800 (2.38%) | 83 / 1000 (8.30%) |
| 9 | Yes | 5,001 | 1 proposal | 39 / 800 (4.88%) | 78 / 1000 (7.80%) |
| 10 | Yes | 5,001 | 1 proposal | 17 / 800 (2.12%) | 80 / 1000 (8.00%) |
| 11 | Yes | 5,001 | 1 proposal | 36 / 800 (4.50%) | 73 / 1000 (7.30%) |
| 12 | Yes | 5,001 | 1 proposal | 50 / 800 (6.25%) | 70 / 1000 (7.00%) |
| 13 | Yes | 5,001 | 1 proposal | 29 / 800 (3.62%) | 80 / 1000 (8.00%) |
| 14 | Yes | 5,001 | 1 proposal | 53 / 800 (6.62%) | 78 / 1000 (7.80%) |
| 15 | Yes | 5,001 | 1 proposal | 18 / 800 (2.25%) | 76 / 1000 (7.60%) |
| 16 | Yes | 5,001 | 1 proposal | 38 / 800 (4.75%) | 75 / 1000 (7.50%) |
| 17 | Yes | 5,001 | 1 proposal | 34 / 800 (4.25%) | 76 / 1000 (7.60%) |
| 18 | Yes | 5,001 | 1 proposal | 26 / 800 (3.25%) | 72 / 1000 (7.20%) |
| 19 | Yes | 5,001 | 1 proposal | 27 / 800 (3.38%) | 79 / 1000 (7.90%) |
| 20 | Yes | 5,001 | 1 proposal | 25 / 800 (3.12%) | 79 / 1000 (7.90%) |
| 21 | Yes | 5,001 | 1 proposal | 27 / 800 (3.38%) | 88 / 1000 (8.80%) |
| 22 | Yes | 5,001 | 1 proposal | 22 / 800 (2.75%) | 76 / 1000 (7.60%) |
| 23 | Yes | 5,001 | 1 proposal | 28 / 800 (3.50%) | 74 / 1000 (7.40%) |
| 24 | Yes | 5,001 | 1 proposal | 58 / 800 (7.25%) | 80 / 1000 (8.00%) |
| 25 | Yes | 5,001 | 1 proposal | 18 / 800 (2.25%) | 83 / 1000 (8.30%) |
| 26 | Yes | 5,001 | 1 proposal | 59 / 800 (7.38%) | 72 / 1000 (7.20%) |
| 27 | Yes | 5,001 | 1 proposal | 32 / 800 (4.00%) | 80 / 1000 (8.00%) |
| 28 | Yes | 5,001 | 1 proposal | 49 / 800 (6.12%) | 88 / 1000 (8.80%) |
| 29 | Yes | 5,001 | 1 proposal | 15 / 800 (1.88%) | 81 / 1000 (8.10%) |
| 30 | Yes | 5,001 | 1 proposal | 43 / 800 (5.38%) | 83 / 1000 (8.30%) |

## 4. Methodological Distinction: Drift Detection vs. Fork Prediction

- **Drift Detection:** Identifies a retrospective change in the underlying network environment (e.g. latency shifting from 50 ms to 300 ms at proposal 5,000) by comparing rolling statistics against reference history.
- **Fork Prediction ($H=3$):** Predicts whether a new fork birth will occur in the upcoming 3 proposals before it manifests on the network.
- **Conclusion:** The sliding-window detector reliably detects the macro-level parameter shift within an average of 1.0 proposals across 100% of tested runs, enabling node operators to flag when a trained model is operating out-of-distribution.
