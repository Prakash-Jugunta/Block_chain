# Historical v1.2 Artifacts

This directory preserves the historical evidence from the feature schema `v1.2` iteration.
In `v1.2`, rolling windows for proposal records (10 records) and intervals (10 intervals from 11 timestamps) were corrected, but `peer_connectivity` and `churn_rate_100` checked for `PeerChurn` instead of the simulator's `ChurnToggle` event.

These artifacts are preserved for auditability and historical provenance:
- `dataset/fork_risk_dataset.csv` (SHA-256: `367262d37ba3c9184f091bf852b2b6b9dfdd7aa9b0c36ad8547811db781934f7`)
- `dataset/dataset_metadata.json`
- `splits/split_manifest.json`
- `models/logistic_regression/model.json`
- `models/random_forest/model.json`
- `evaluation/metrics.json`
- `evaluation/evaluation_report.md`
- `drift/drift_report.md`
- `AUDIT_REPORT.md`
