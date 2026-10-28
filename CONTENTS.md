# ForkRisk — Combined Milestones 5 & 6 Final Release Contents

Archive: `forkrisk_milestone6_final_release.zip`  
Authoritative Checksum Sidecar: `forkrisk_milestone6_final_release.zip.sha256`  
Target: Comprehensive Final Milestone 5 & 6 Peer Review & System Audit  
Generated: 2026-10-26T12:29:43Z  
Toolchain: Go 1.25.1 windows/amd64 (Pure Go standard library, zero external runtime dependencies)

---

## 1. Directory Layout

```text
forkrisk_milestone6/
├── BUNDLE_MANIFEST.json             # Machine-readable inventory, SHA-256 hashes, and audit scope
├── CONTENTS.md                      # This directory map and audit verification guide
├── README.md                        # Project documentation and architectural overview
├── CASE_STUDY_DEMO_PLAN.md          # Complete project specification and Milestone 1-6 roadmap
├── go.mod / go.sum                  # Pure Go dependency specifications (Go 1.25.1, zero Python, zero Docker)
│
├── cmd/                             # Complete Go CLI entrypoints (15 subcommands)
│   ├── dashboard/                   # Pure Go HTTP dashboard server CLI (Milestone 6)
│   ├── dataset/                     # Feature extraction & dataset generation CLI
│   ├── demo/                        # Dual-mode end-to-end demonstration CLI (Milestone 5)
│   ├── drift/                       # Sliding-window concept drift runner & evaluator CLI
│   ├── evaluate/                    # ML model prediction & ROC/PR metric evaluator CLI
│   ├── experiment/                  # Full 240-run production experiment runner CLI
│   ├── labelaudit/                  # H=3 causal fork-birth labeling verifier CLI
│   ├── metrics/                     # Simulation run metric aggregator CLI
│   ├── predict/                     # Standalone model inference CLI
│   ├── report/                      # Automated markdown research report generator CLI (Milestone 5)
│   ├── sensitivity/                 # Forecast horizon sensitivity sweep CLI (H=1..5, Milestone 5)
│   ├── simulate/                    # Single-run discrete-event simulator CLI
│   ├── split/                       # Seed-isolated train/val/test data splitting CLI
│   ├── train/                       # Logistic Regression & Random Forest training CLI
│   └── verify_audit/                # Independent provenance, SHA-256 rehashing & audit CLI
│
├── internal/                        # Core simulation, ML, dashboard, and visualization packages
│   ├── config/                      # Scenario YAML parsing, validation, and override engine
│   ├── dashboard/                   # Dashboard HTTP server, real LogEntry loader, dual evaluation, tests
│   ├── engine/                      # Deterministic priority-queue discrete-event simulator engine
│   ├── features/                    # 22 causal network features & streaming extractors (schema v1.3)
│   ├── fork/                        # Real-time multi-node fork detector & ancestor tracker
│   ├── logstore/                    # Deterministic JSONL event logger and run metadata manager
│   ├── metrics/                     # Fork rate, stale rate, goodput, TPS, and consistency metrics
│   ├── ml/                          # Pure-Go ML models (Logistic Regression, Random Forest)
│   │   └── drift/                   # Sliding-window drift detector (W=200, theta=2.0σ, validation-tuned)
│   ├── network/                     # Network topology, propagation delays, and churn modeling
│   ├── node/                        # Blockchain validator node state, mempool & chain state
│   └── viz/                         # Pure-Go chart rendering engine (gonum/plot) for all 10 charts
│
├── web/                             # Lightweight, zero-build dashboard frontend (Milestone 6)
│   ├── index.html                   # Semantic HTML5 dashboard single-page interface
│   ├── styles.css                   # Responsive dark-mode styling with glassmorphism & KPI cards
│   └── app.js                       # Vanilla JavaScript dashboard state manager & API client
│
├── scenarios/                       # Authoritative scenario YAML configurations
│   ├── production/                  # 8 production scenarios (10,000 blocks, 30 seeds each)
│   ├── pilot/                       # 8 pilot scenarios (1,000 blocks, 3 seeds each)
│   └── test/                        # Lightweight regression test scenarios
│
├── docs/                            # Comprehensive research reports, figures, and architectural records
│   ├── decisions.md                 # Architecture Decision Records (ADRs 0001–0010)
│   ├── report.md                    # Research report with methodology, horizon sweep, drift results
│   ├── real_chain_cross_check.md    # Host environment audit & real-chain cross-validation feasibility
│   └── figures/                     # 10 publication-quality PNG charts rendered in pure Go
│
├── verification/                    # Independent verification evidence and audit logs
│   ├── matrix_audit.json            # 240/240 run completion and integrity record
│   ├── audit_report.md              # Re-hashed SHA-256 provenance audit report
│   ├── run_summary.csv              # Run-level metrics across all 240 production runs
│   ├── scenario_summary.csv         # Scenario-aggregated means and standard deviations
│   └── verification_commands.txt    # Verbatim command execution logs on Go 1.25.1
│
├── outputs/                         # Structured simulation and evaluation outputs
│   ├── demo/                        # Demonstration run logs and transcripts
│   │   ├── live_demo_transcript.txt     # Complete transcript from live execution mode
│   │   ├── fallback_demo_transcript.txt # Complete transcript from fallback verification mode
│   │   └── demo_transcript.txt          # Active demo transcript log
│   ├── experiments/milestone4/      # Production experiment outputs and machine-readable artifacts
│   │   ├── drift_results.json           # Machine-readable drift detection parameters & test metrics
│   │   ├── drift_report.md              # Detailed concept drift detector analysis report
│   │   ├── production_eval_metrics.json # Held-out production matrix evaluation (57,062 samples)
│   │   └── runs/                        # Run metadata and metrics across 240 production runs
│   └── final_evaluation/            # Milestone 5 final model evaluations
│       ├── horizon_sensitivity.csv  # Performance metrics for H=1, 2, 3, 4, 5
│       └── horizon_sensitivity.json # Structured horizon sensitivity data
│
└── tests/                           # Complete end-to-end integration test suite
    ├── simulate_test.go             # Simulation engine determinism, gossip, and convergence tests
    ├── milestone4_test.go           # 240-run matrix completeness & aggregation tests
    ├── drift_audit_test.go          # Per-seed drift detection dynamics & delay verification
    └── baseline_divergence_test.go  # Push-only gossip orphan & baseline divergence investigation
```

---

## 2. Key Corrections Applied

1. **Production Runs Dashboard Inspection (Summary-Only Mode):**
   - Packaged production runs without 'events.jsonl' are inspected cleanly via an explicit summary-only mode.
   - Run metrics are loaded from canonical 'run_summary.csv', and configuration from 'metadata.json'.
   - Node snapshots and timeline views return explicit unavailable status without fabricating node states or timeline events, and without returning 404/500 errors.
   - Runs with event logs present retain full event-backed node status snapshots and granular event timelines.
2. **Correct Drift API Semantics:**
   - Exposes separate, truthful values:
     - 'transition_detection_rate': 1.0 (100.0%, 27/27 test seeds detected)
     - 'post_transition_ongoing_alarm_rate': 0.0786 (7.86% ongoing alarms after sliding-window reference reset)
     - 'pre_transition_false_positive_rate': 0.0433 (4.33% false alarm rate before shift)
     - 'mean_detection_delay_proposals': 1.0 proposal
     - 'detection_delay_proposals': min 1, p25 1.0, median 1.0, mean 1.0, p75 1.0, p95 1.0, max 1
     - 'detected_seeds_count': 27, 'undetected_seeds_count': 0, 'test_seeds_count': 27
   - The 7.86% ongoing alarm rate is never presented as transition recall.
3. **Proposal-Index Timeline Filtering:**
   - Scans events in original sequence to reconstruct the monotonic global proposal index.
   - Retains preceding proposal context so competing branch classification remains accurate across filter boundaries.
   - Treats 'ConfigChange.AtBlock' as its explicit trigger proposal coordinate.
   - Preserves distinct coordinates: global proposal index, local block height, event sequence, and simulation time.
4. **Regenerated Compact Verification Manifest:**
   - 'forkrisk_milestone6_verification_bundle.zip' contains its own tailored manifest describing actual included files only.
   - Intentionally omitted files (e.g., raw pilot event logs) are listed in 'excluded_files' with documented rationales.
5. **Self-Referential Hash Prevention:**
   - 'BUNDLE_MANIFEST.json' is excluded from its own internal files hash list. Authoritative archive checksums are preserved in external '.sha256' sidecars.

## 3. Bundle Manifest Verification

- **Filename:** `BUNDLE_MANIFEST.json`
- **Size:** 216018 bytes
- **SHA-256:** `52bf73675fd1c921012d4cb018e9b51ac2d8db941743572bbfa3e21beea21169`
