# ForkRisk Milestone 2 — Historical Audit Report (Archived Intermediate State)

> **Notice:** This document is retained for historical audit provenance. It records the state of Milestone 2 prior to the final block capacity zero semantics fix and explicit zero transaction-arrival rate configuration handling.  
> **Authoritative Final Sign-Off:** For the final, verified Milestone 2 sign-off report, see [`outputs/pilot_final/AUDIT_REPORT.md`](file:///e:/sem7/blockchain/casestudy/outputs/pilot_final/AUDIT_REPORT.md) and [`outputs/pilot_final/pilot_report.md`](file:///e:/sem7/blockchain/casestudy/outputs/pilot_final/pilot_report.md).

**Historical Date:** 2026-10-24  
**Go version:** go1.25.1 windows/amd64  
**Historical Pilot Directory:** `outputs/pilot/`  
**Historical Archive:** `forkrisk_milestone2_verified.zip`

---

## Historical Verification Summary

### Commands & Results at Historical Checkpoint
- **Build (`go build ./...`):** Exit code 0 (zero errors).
- **Test Suite (`go test ./... -count=1`):** 106 tests executed, 0 failures.
- **Formatting (`gofmt -l .`):** Clean, 0 unformatted files.

### Historical Pilot Artifacts (`outputs/pilot/`)
- **CSV:** `outputs/pilot/pilot_summary.csv` (24 rows, 26 columns, seeds 1–3 across 8 scenarios).
- **Historical Baseline Seed 1 Hash:** `d6297046703791e9bc6079f583612e85009d620034bb86b61ba676b7bd61d9f6`.
- **Dynamic Network Metadata:** Recorded initial `mean_latency_ms: 50` with transition at block 150.

---

## Evolution to Final Sign-Off

Following this historical checkpoint, two key configuration semantics were identified and corrected:
1. `block_capacity: 0` semantics were updated to mean truly unlimited capacity (consuming the entire pending pool) rather than falling back to `tx_per_block`.
2. Explicit `tx_arrival_rate: 0` handling in YAML unmarshaling was implemented to preserve zero arrival rates instead of defaulting to 1.0.
3. Test suite was expanded from 106 to 109 top-level functions (121 executed test cases including subtests).
4. The authoritative final pilot was regenerated in `outputs/pilot_final/`, where baseline seed 1 reproducibility hash is `b99da89e7645eead22cb7d8600075ee5b3367676d8b54dfa1c894f2a6168a226`.

These historical artifacts are preserved in `outputs/pilot/` to maintain a complete, unbroken experimental audit trail.
