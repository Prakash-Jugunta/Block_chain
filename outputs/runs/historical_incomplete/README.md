# Historical Incomplete Simulation Runs

This directory preserves early prototype run artifacts from the initial implementation phase (2026-10-21) that lack `metadata.json` files.

## Preserved Run Directories

1. `baseline_seed7_1792563209/` — Initial `cmd/simulate` prototype run (105,074 events).
2. `baseline_seed7_1792563218/` — Repeated prototype test run (105,074 events).
3. `baseline_seed7_1792563740/` — Repeated prototype test run (105,074 events).
4. `baseline_seed7_1792563759/` — Repeated prototype test run (105,074 events).
5. `two_node_test_seed1_1792563889/` — Early 2-node convergence test (420 events).

## Audit & Status

- **Status:** `historical_incomplete`
- **Reason for Missing Metadata:** These runs were generated on 2026-10-21 before `logstore.RunMetadata` and automated `metadata.json` logging were introduced into `cmd/simulate`.
- **Integrity Decision:** Rather than fabricating synthetic `metadata.json` files for an unversioned prototype engine state, these raw event logs are retained unmodified as historical evidence and quarantined here so they do not conflict with full-provenance runs in `outputs/runs/`.
- **Active / Authoritative Pilot:** All authoritative Milestone 2 final pilot runs reside in `outputs/runs/` and are summarized in `outputs/pilot_final/pilot_summary.csv` and `outputs/pilot_final/pilot_report.md`.
