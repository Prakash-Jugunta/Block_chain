package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

func setupTestServer(t *testing.T) *Server {
	srv := NewServer(ServerConfig{
		Host:    "127.0.0.1",
		Port:    8080,
		DataDir: "../../outputs",
		WebDir:  "../../web",
	})
	return srv
}

func TestHealthEndpoint(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	if res.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", res.Status)
	}
	if len(res.Milestones) < 6 {
		t.Errorf("expected milestones 1-6, got %v", res.Milestones)
	}
}

func TestMethodNotAllowedOnNonGet(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", w.Code)
	}
}

func TestPathTraversalPrevention(t *testing.T) {
	srv := setupTestServer(t)

	traversalPaths := []string{
		"/api/runs/../../windows/win.ini",
		"/api/runs/..%2F..%2Fetc/passwd",
		"/api/runs/invalid*name",
		"/api/runs/foo/bar/baz",
	}

	for _, p := range traversalPaths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			t.Errorf("path traversal vulnerability detected: path %q returned 200 OK", p)
		}
	}
}

func TestRunsListEndpoint(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if _, ok := res["runs"]; !ok {
		t.Errorf("expected 'runs' key in response")
	}
}

func TestScenariosSummaryEndpoint(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/scenarios/summary", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
}

func TestModelEvaluation_DualScope(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/models/evaluation", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res EvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	// 1. Verify Production Scope (Authoritative, 57,062 samples)
	if res.Production == nil {
		t.Fatalf("expected production evaluation scope to be present")
	}
	if res.Production.TestSamples != 57062 {
		t.Errorf("expected 57,062 production samples, got %d", res.Production.TestSamples)
	}
	lrProd, ok := res.Production.Models["logistic_regression"]
	if !ok || lrProd.ROCAUC == nil || *lrProd.ROCAUC < 0.74 || *lrProd.ROCAUC > 0.75 {
		t.Errorf("expected production LR ROC-AUC around 0.7435, got %v", lrProd.ROCAUC)
	}

	// 2. Verify Pilot Scope (Held-Out Seed 3, 1,608 samples)
	if res.Pilot == nil {
		t.Fatalf("expected pilot evaluation scope to be present")
	}
	if res.Pilot.TestSamples != 1608 {
		t.Errorf("expected 1,608 pilot samples, got %d", res.Pilot.TestSamples)
	}
	lrPilot, ok := res.Pilot.Models["logistic_regression"]
	if !ok || lrPilot.ROCAUC == nil || *lrPilot.ROCAUC < 0.81 || *lrPilot.ROCAUC > 0.82 {
		t.Errorf("expected pilot LR ROC-AUC around 0.8124, got %v", lrPilot.ROCAUC)
	}
}

func TestDriftSummary_ArtifactBacked(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/drift", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res DriftSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if res.WindowSize != 200 {
		t.Errorf("expected window size 200, got %d", res.WindowSize)
	}
	if res.ThresholdZ != 2.0 {
		t.Errorf("expected threshold 2.0, got %f", res.ThresholdZ)
	}
	if res.Feature != "latency_mean_10" {
		t.Errorf("expected monitored feature 'latency_mean_10', got %q", res.Feature)
	}
	if res.Scenario != "dynamic_network" {
		t.Errorf("expected scenario 'dynamic_network', got %q", res.Scenario)
	}
	if res.PreTransitionFalsePositiveRate < 0.04 || res.PreTransitionFalsePositiveRate > 0.05 {
		t.Errorf("expected pre-transition false positive rate ~0.0433, got %f", res.PreTransitionFalsePositiveRate)
	}
	if res.PostTransitionOngoingAlarmRate < 0.07 || res.PostTransitionOngoingAlarmRate > 0.09 {
		t.Errorf("expected post-transition ongoing alarm rate ~0.0786, got %f", res.PostTransitionOngoingAlarmRate)
	}
	// Crucial check: PostTransitionRecall / TransitionDetectionRate must be 1.0 (100%), NEVER 0.0786!
	if res.TransitionDetectionRate != 1.0 {
		t.Errorf("expected transition detection rate 1.0 (100%%), got %f", res.TransitionDetectionRate)
	}
	if res.Recall != 1.0 {
		t.Errorf("expected legacy recall alias to report 1.0 (100%% transition recall), got %f", res.Recall)
	}
	if res.DetectedSeedsCount != 27 {
		t.Errorf("expected 27 detected seeds, got %d", res.DetectedSeedsCount)
	}
	if res.UndetectedSeedsCount != 0 {
		t.Errorf("expected 0 undetected seeds, got %d", res.UndetectedSeedsCount)
	}
	if res.TestSeedsCount != 27 {
		t.Errorf("expected 27 test seeds, got %d", res.TestSeedsCount)
	}
	if res.MeanDetectionDelayProposals != 1.0 {
		t.Errorf("expected mean detection delay 1.0, got %f", res.MeanDetectionDelayProposals)
	}
	if res.DetectionDelayProposals == nil || res.DetectionDelayProposals.Median != 1.0 || res.DetectionDelayProposals.Min != 1 || res.DetectionDelayProposals.Max != 1 {
		t.Errorf("expected delay percentiles median=1.0, min=1, max=1, got %+v", res.DetectionDelayProposals)
	}
}

func TestRealLogFormat_NodeStatusAndTimeline(t *testing.T) {
	tempDir := t.TempDir()
	runID := "test_real_format_run"
	runDir := filepath.Join(tempDir, "runs", runID)
	os.MkdirAll(runDir, 0755)

	logPath := filepath.Join(runDir, "events.jsonl")
	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open test log: %v", err)
	}

	// Write real-format logstore.LogEntry records
	entries := []logstore.LogEntry{
		{Seq: 1, SimTime: 1.0, EventType: "BlockPropose", NodeID: "N0", BlockHash: "hash_block_1", BlockHeight: 1, TipHeight: 1},
		{Seq: 2, SimTime: 1.2, EventType: "BlockArrive", NodeID: "N1", BlockHash: "hash_block_1", BlockHeight: 1, TipHeight: 1, TipHash: "hash_block_1"},
		{Seq: 3, SimTime: 1.5, EventType: "ChurnToggle", NodeID: "N2", Reason: "offline"},
		{Seq: 4, SimTime: 2.0, EventType: "BlockPropose", NodeID: "N0", BlockHash: "hash_block_2a", BlockHeight: 2, TipHeight: 2},
		// Competing fork proposal at same height
		{Seq: 5, SimTime: 2.1, EventType: "BlockPropose", NodeID: "N1", BlockHash: "hash_block_2b", BlockHeight: 2, TipHeight: 2},
		// Orphan block
		{Seq: 6, SimTime: 2.3, EventType: "BlockOrphan", NodeID: "N2", BlockHash: "hash_orphan_3", BlockHeight: 3, Reason: "missing_parent"},
		// Reorganization
		{Seq: 7, SimTime: 2.5, EventType: "TipUpdate", NodeID: "N0", BlockHash: "hash_block_2b", TipHeight: 2, TipHash: "hash_block_2b", OldTipHash: "hash_block_2a", ParentHash: "hash_block_1", Reason: "reorg"},
		// Node churn back online
		{Seq: 8, SimTime: 3.0, EventType: "ChurnToggle", NodeID: "N2", Reason: "online"},
		// Config change
		{Seq: 9, SimTime: 3.5, EventType: "ConfigChange", NodeID: "N0", ConfigField: "mean_latency_ms", ConfigOldValue: "50", ConfigNewValue: "300", AtBlock: 5000},
	}

	for _, e := range entries {
		if err := logger.Write(e); err != nil {
			t.Fatalf("failed to write entry: %v", err)
		}
	}
	logger.Close()

	// Write metadata.json
	meta := logstore.RunMetadata{
		RunID:          runID,
		ScenarioName:   "dynamic_network",
		Seed:           42,
		ProposedBlocks: 2,
		Config: config.ScenarioConfig{
			Name:         "dynamic_network",
			NumNodes:     3,
			PeersPerNode: 2,
			ChurnEnabled: true,
		},
	}
	if err := logstore.WriteMetadata(runDir, meta); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	loader := NewDataLoader(tempDir)

	// 1. Test Node Status Parsing
	nodes, err := loader.GetRunNodes(runID)
	if err != nil {
		t.Fatalf("failed to get run nodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes (N0, N1, N2), got %d", len(nodes))
	}
	// Verify N0
	if nodes[0].NodeID != "N0" || nodes[0].Height != 2 || !nodes[0].Online {
		t.Errorf("unexpected N0 state: %+v", nodes[0])
	}
	// Verify N2 (was offline then back online)
	if nodes[2].NodeID != "N2" || !nodes[2].Online {
		t.Errorf("unexpected N2 state: %+v", nodes[2])
	}

	// 2. Test Timeline Event Parsing
	timeline, err := loader.GetRunTimeline(runID, TimelineFilter{Limit: 50})
	if err != nil {
		t.Fatalf("failed to get timeline: %v", err)
	}

	foundCompetingFork := false
	foundOrphan := false
	foundReorg := false
	foundConfigChange := false

	for _, ev := range timeline {
		if ev.EventType == "competing_fork" {
			foundCompetingFork = true
		}
		if ev.EventType == "orphan" {
			foundOrphan = true
		}
		if ev.EventType == "reorg" {
			foundReorg = true
		}
		if ev.EventType == "config_change" {
			foundConfigChange = true
		}
	}

	if !foundCompetingFork {
		t.Errorf("expected competing_fork event in timeline")
	}
	if !foundOrphan {
		t.Errorf("expected orphan event in timeline")
	}
	if !foundReorg {
		t.Errorf("expected reorg event in timeline")
	}
	if !foundConfigChange {
		t.Errorf("expected config_change event in timeline")
	}

	// 3. Test Safety Cap on Query Limit
	cappedTimeline, err := loader.GetRunTimeline(runID, TimelineFilter{Limit: 1000})
	if err != nil {
		t.Fatalf("failed timeline with large limit: %v", err)
	}
	if len(cappedTimeline) > 500 {
		t.Errorf("timeline exceeded safety cap of 500: got %d", len(cappedTimeline))
	}
}

func TestRunDetail_ChurnMetricsAgreement(t *testing.T) {
	loader := NewDataLoader("../../outputs")

	// 1. Baseline Run: 99% peer availability
	detailBase, err := loader.GetRunDetail("baseline_seed1")
	if err == nil && detailBase.Metrics != nil {
		if detailBase.Metrics.PeerAvailability < 0.95 {
			t.Errorf("expected baseline peer availability >= 0.95, got %f", detailBase.Metrics.PeerAvailability)
		}
	}

	// 2. Churn Stress Run: ~50% peer availability
	detailChurn, err := loader.GetRunDetail("churn_stress_seed1")
	if err == nil && detailChurn.Metrics != nil {
		if detailChurn.Metrics.PeerAvailability > 0.60 || detailChurn.Metrics.PeerAvailability < 0.40 {
			t.Errorf("expected churn_stress peer availability ~0.50, got %f", detailChurn.Metrics.PeerAvailability)
		}
	}
}

func TestDriftSummary_MissingOrCorruptArtifact(t *testing.T) {
	tempDir := t.TempDir()
	loader := NewDataLoader(tempDir)

	// 1. Missing artifact: returns truthful unavailable status
	res, err := loader.GetDriftSummary()
	if err != nil {
		t.Fatalf("unexpected error on missing artifact: %v", err)
	}
	if len(res.Status) < 3 || res.Status[:3] != "N/A" {
		t.Errorf("expected status to start with 'N/A', got %q", res.Status)
	}

	// 2. Corrupted artifact: returns truthful unavailable status without inventing verification
	m4Dir := filepath.Join(tempDir, "experiments", "milestone4")
	os.MkdirAll(m4Dir, 0755)
	corruptPath := filepath.Join(m4Dir, "drift_results.json")
	os.WriteFile(corruptPath, []byte(`{invalid json syntax...`), 0644)

	resCorrupt, err := loader.GetDriftSummary()
	if err != nil {
		t.Fatalf("unexpected error on corrupt artifact: %v", err)
	}
	if len(resCorrupt.Status) < 3 || resCorrupt.Status[:3] != "N/A" {
		t.Errorf("expected corrupt status to start with 'N/A', got %q", resCorrupt.Status)
	}
}

func TestTimeline_PaginationAndFilteringAfter500Events(t *testing.T) {
	tempDir := t.TempDir()
	runID := "test_pagination_run"
	runDir := filepath.Join(tempDir, "runs", runID)
	os.MkdirAll(runDir, 0755)

	logPath := filepath.Join(runDir, "events.jsonl")
	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}

	// Write 600 events. The dynamic transition occurs at proposal 5,000 (around event 550)
	for i := 1; i <= 600; i++ {
		ev := logstore.LogEntry{
			Seq:           int64(i),
			SimTime:       float64(i) * 0.1,
			EventType:     "BlockPropose",
			NodeID:        "N0",
			BlockHash:     "block_hash",
			BlockHeight:   i,
			ProposalIndex: i,
		}
		if i == 550 {
			ev.EventType = "ConfigChange"
			ev.ProposalIndex = 5000
			ev.ConfigField = "mean_latency_ms"
			ev.ConfigOldValue = "50"
			ev.ConfigNewValue = "300"
			ev.AtBlock = 5000
		}
		if err := logger.Write(ev); err != nil {
			t.Fatalf("error writing entry: %v", err)
		}
	}
	logger.Close()

	loader := NewDataLoader(tempDir)

	// 1. Without filter: default query returns first 100 events (cannot see event 550)
	evsStart, err := loader.GetRunTimeline(runID, TimelineFilter{Limit: 100})
	if err != nil || len(evsStart) != 100 {
		t.Fatalf("expected 100 events, got %d (err: %v)", len(evsStart), err)
	}
	if evsStart[0].ProposalIndex != 1 {
		t.Errorf("expected first event to be proposal #1, got %d", evsStart[0].ProposalIndex)
	}

	// 2. Query with from_proposal=5000: successfully retrieves the transition event occurring after the first 500 events
	evsFiltered, err := loader.GetRunTimeline(runID, TimelineFilter{Limit: 50, FromProposal: 5000})
	if err != nil {
		t.Fatalf("failed to query filtered timeline: %v", err)
	}
	if len(evsFiltered) == 0 {
		t.Fatalf("expected events at or after proposal 5000, got 0")
	}
	if evsFiltered[0].EventType != "config_change" || evsFiltered[0].ProposalIndex != 5000 {
		t.Errorf("expected first filtered event to be config_change at proposal 5000, got %+v", evsFiltered[0])
	}

	// 3. Offset pagination: offset=540 retrieves events starting from index 541
	evsOffset, err := loader.GetRunTimeline(runID, TimelineFilter{Limit: 20, Offset: 540})
	if err != nil || len(evsOffset) != 20 {
		t.Fatalf("expected 20 offset events, got %d (err: %v)", len(evsOffset), err)
	}
}

func TestNodeStatus_UnrecordedConfiguredNodes(t *testing.T) {
	tempDir := t.TempDir()
	runID := "test_unrecorded_nodes"
	runDir := filepath.Join(tempDir, "runs", runID)
	os.MkdirAll(runDir, 0755)

	// Metadata specifies 4 configured nodes (N0..N3)
	meta := logstore.RunMetadata{
		RunID: runID,
		Config: config.ScenarioConfig{
			NumNodes:     4,
			PeersPerNode: 3,
		},
	}
	logstore.WriteMetadata(runDir, meta)

	// Log contains events ONLY for N0 and N1. N2 and N3 have ZERO events.
	logPath := filepath.Join(runDir, "events.jsonl")
	logger, _ := logstore.Open(logPath)
	logger.Write(logstore.LogEntry{
		Seq: 1, SimTime: 1.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 1, BlockHash: "h0",
	})
	logger.Write(logstore.LogEntry{
		Seq: 2, SimTime: 1.1, EventType: "BlockArrive", NodeID: "N1", BlockHeight: 1, TipHeight: 1, TipHash: "h0",
	})
	logger.Close()

	loader := NewDataLoader(tempDir)
	nodes, err := loader.GetRunNodes(runID)
	if err != nil {
		t.Fatalf("failed to get nodes: %v", err)
	}

	if len(nodes) != 4 {
		t.Fatalf("expected all 4 configured nodes to be represented, got %d", len(nodes))
	}

	// N0 and N1 should be active
	if nodes[0].NodeID != "N0" || nodes[0].State != "active" {
		t.Errorf("unexpected N0 state: %+v", nodes[0])
	}
	if nodes[1].NodeID != "N1" || nodes[1].State != "active" {
		t.Errorf("unexpected N1 state: %+v", nodes[1])
	}

	// N2 and N3 must have clear unrecorded state without fabricating tip or availability
	if nodes[2].NodeID != "N2" || nodes[2].State != "unrecorded" || nodes[2].Online || nodes[2].TipHash != "unrecorded" {
		t.Errorf("expected N2 to be unrecorded/offline with unrecorded tip, got: %+v", nodes[2])
	}
	if nodes[3].NodeID != "N3" || nodes[3].State != "unrecorded" || nodes[3].Online || nodes[3].TipHash != "unrecorded" {
		t.Errorf("expected N3 to be unrecorded/offline with unrecorded tip, got: %+v", nodes[3])
	}
}

func TestNoWildcardCORSHeader(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	cors := w.Header().Get("Access-Control-Allow-Origin")
	if cors == "*" {
		t.Errorf("forbidden wildcard CORS header detected in response: Access-Control-Allow-Origin: %s", cors)
	}
}

func TestRunDetail_PackagedProductionRun_SummaryOnlyMode(t *testing.T) {
	tempDir := t.TempDir()

	// Recreate production directory structure matching packaged release:
	// Copy real production metadata.json for baseline_seed1 and canonical run_summary.csv,
	// but intentionally OMIT events.jsonl to test packaged production run inspection.
	realMetaPath := filepath.Join("..", "..", "outputs", "experiments", "milestone4", "runs", "baseline_seed1", "metadata.json")
	metaData, err := os.ReadFile(realMetaPath)
	if err != nil {
		t.Fatalf("failed reading real production metadata: %v", err)
	}

	realSummaryPath := filepath.Join("..", "..", "outputs", "experiments", "milestone4", "run_summary.csv")
	summaryData, err := os.ReadFile(realSummaryPath)
	if err != nil {
		t.Fatalf("failed reading real run_summary.csv: %v", err)
	}

	prodDir := filepath.Join(tempDir, "experiments", "milestone4")
	runDir := filepath.Join(prodDir, "runs", "baseline_seed1")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("failed mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "metadata.json"), metaData, 0644); err != nil {
		t.Fatalf("failed writing metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prodDir, "run_summary.csv"), summaryData, 0644); err != nil {
		t.Fatalf("failed writing run_summary.csv: %v", err)
	}

	// Also create a second run WITH event log to verify that event-backed node
	// and timeline functionality is retained for runs whose logs are present.
	runWithLogDir := filepath.Join(tempDir, "runs", "run_with_log")
	if err := os.MkdirAll(runWithLogDir, 0755); err != nil {
		t.Fatalf("failed mkdir: %v", err)
	}
	logWithLogPath := filepath.Join(runWithLogDir, "events.jsonl")
	logger, err := logstore.Open(logWithLogPath)
	if err != nil {
		t.Fatalf("failed opening logger: %v", err)
	}
	logger.Write(logstore.LogEntry{Seq: 1, SimTime: 1.0, EventType: "BlockPropose", NodeID: "N0", BlockHeight: 1, BlockHash: "hash1"})
	logger.Write(logstore.LogEntry{Seq: 2, SimTime: 1.1, EventType: "BlockArrive", NodeID: "N1", BlockHeight: 1, TipHeight: 1, TipHash: "hash1"})
	logger.Close()
	logstore.WriteMetadata(runWithLogDir, logstore.RunMetadata{
		RunID: "run_with_log",
		Config: config.ScenarioConfig{
			NumNodes: 2,
		},
	})

	srv := NewServer(ServerConfig{
		Host:    "127.0.0.1",
		Port:    8080,
		DataDir: tempDir,
		WebDir:  "../../web",
	})

	runID := "baseline_seed1"

	// 1. Direct Loader check for summary-only mode
	detail, err := srv.loader.GetRunDetail(runID)
	if err != nil {
		t.Fatalf("expected successful summary-only run detail, got error: %v", err)
	}
	if !detail.SummaryOnly {
		t.Errorf("expected summary_only=true for packaged production run without log, got false")
	}
	if detail.LogAvailable {
		t.Errorf("expected log_available=false for packaged production run without log, got true")
	}
	if detail.Metrics == nil {
		t.Fatalf("expected non-nil metrics loaded from canonical run_summary.csv")
	}
	if detail.Metrics.ForkRate <= 0 || detail.Metrics.StaleRate <= 0 {
		t.Errorf("expected populated fork_rate and stale_rate from CSV, got %+v", detail.Metrics)
	}
	if detail.Config == nil {
		t.Errorf("expected config loaded from metadata.json, got nil")
	}

	// 2. Nodes check: must return empty slice without fabricating nodes, and NO error
	nodes, err := srv.loader.GetRunNodes(runID)
	if err != nil {
		t.Fatalf("expected no error from GetRunNodes for omitted log, got: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 fabricated nodes when event log is absent, got %d", len(nodes))
	}

	// 3. Timeline check: must return empty slice without fabricating events, and NO error
	events, err := srv.loader.GetRunTimeline(runID, TimelineFilter{Limit: 50})
	if err != nil {
		t.Fatalf("expected no error from GetRunTimeline for omitted log, got: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 fabricated events when event log is absent, got %d", len(events))
	}

	// 4. HTTP API Endpoint Checks: all must return HTTP 200 OK (no 404/500 errors)
	reqDetail := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID, nil)
	wDetail := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Errorf("expected 200 OK from /api/runs/%s, got %d", runID, wDetail.Code)
	}

	reqNodes := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/nodes", nil)
	wNodes := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(wNodes, reqNodes)
	if wNodes.Code != http.StatusOK {
		t.Errorf("expected 200 OK from /api/runs/%s/nodes, got %d", runID, wNodes.Code)
	}
	var nodesResp map[string]interface{}
	if err := json.Unmarshal(wNodes.Body.Bytes(), &nodesResp); err == nil {
		if nodesResp["log_available"] != false {
			t.Errorf("expected log_available=false in nodes response, got %v", nodesResp["log_available"])
		}
	}

	reqTimeline := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/timeline", nil)
	wTimeline := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(wTimeline, reqTimeline)
	if wTimeline.Code != http.StatusOK {
		t.Errorf("expected 200 OK from /api/runs/%s/timeline, got %d", runID, wTimeline.Code)
	}
	var timelineResp map[string]interface{}
	if err := json.Unmarshal(wTimeline.Body.Bytes(), &timelineResp); err == nil {
		if timelineResp["log_available"] != false {
			t.Errorf("expected log_available=false in timeline response, got %v", timelineResp["log_available"])
		}
	}

	// 5. Verification of run with log present: event-backed nodes and timeline are retained!
	detailWithLog, err := srv.loader.GetRunDetail("run_with_log")
	if err != nil {
		t.Fatalf("expected run_with_log detail, got error: %v", err)
	}
	if detailWithLog.SummaryOnly || !detailWithLog.LogAvailable {
		t.Errorf("expected summary_only=false and log_available=true for run_with_log, got %+v", detailWithLog)
	}
	nodesWithLog, err := srv.loader.GetRunNodes("run_with_log")
	if err != nil || len(nodesWithLog) == 0 {
		t.Errorf("expected active nodes for run_with_log, got %d nodes (err: %v)", len(nodesWithLog), err)
	}
	timelineWithLog, err := srv.loader.GetRunTimeline("run_with_log", TimelineFilter{Limit: 10})
	if err != nil || len(timelineWithLog) == 0 {
		t.Errorf("expected timeline events for run_with_log, got %d events (err: %v)", len(timelineWithLog), err)
	}
}

func TestTimeline_EffectiveProposalIndexFilteringAndCompetingFork(t *testing.T) {
	tempDir := t.TempDir()
	runID := "test_effective_filtering_run"
	runDir := filepath.Join(tempDir, "runs", runID)
	os.MkdirAll(runDir, 0755)

	logPath := filepath.Join(runDir, "events.jsonl")
	logger, err := logstore.Open(logPath)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}

	// Construct a fixture with 650 events:
	// - Events 1..400: regular proposals at heights 1..400 (proposal 1..400)
	// - Height 250: main block proposed at proposal 250 (hash "block_250_main")
	// - Interspersed: churn transitions without proposal index at sim_time 50, 150
	// - Event 520: ConfigChange with AtBlock=5000 (explicit proposal coordinate 5000)
	// - Event 530: ChurnToggle occurring at effective proposal 5000
	// - Event 540: BlockArrive occurring at effective proposal 5000
	// - Event 550: Competing fork proposal at height 250 with ProposalIndex=5001 (hash "block_250_competing")
	// - Events 551..650: subsequent proposals
	for i := 1; i <= 400; i++ {
		ev := logstore.LogEntry{
			Seq:           int64(i),
			SimTime:       float64(i) * 0.1,
			EventType:     "BlockPropose",
			NodeID:        "N0",
			BlockHash:     "block_" + strconv.Itoa(i) + "_main",
			BlockHeight:   i,
			ProposalIndex: i,
		}
		logger.Write(ev)
		if i == 100 {
			logger.Write(logstore.LogEntry{
				Seq:       1001,
				SimTime:   10.05,
				EventType: "ChurnToggle",
				NodeID:    "N1",
				Reason:    "offline",
			})
		}
	}

	// Transition event at proposal 5000
	logger.Write(logstore.LogEntry{
		Seq:            5000,
		SimTime:        500.0,
		EventType:      "ConfigChange",
		NodeID:         "N0",
		ConfigField:    "mean_latency_ms",
		ConfigOldValue: "50",
		ConfigNewValue: "300",
		AtBlock:        5000,
	})

	// Churn event at effective proposal 5000 (omits ProposalIndex)
	logger.Write(logstore.LogEntry{
		Seq:       5001,
		SimTime:   500.1,
		EventType: "ChurnToggle",
		NodeID:    "N2",
		Reason:    "online",
	})

	// Competing fork proposal at height 250 (sibling to block_250_main) proposed at proposal 5001
	logger.Write(logstore.LogEntry{
		Seq:           5002,
		SimTime:       500.2,
		EventType:     "BlockPropose",
		NodeID:        "N1",
		BlockHash:     "block_250_competing",
		BlockHeight:   250,
		ProposalIndex: 5001,
	})

	// Subsequent events up to 650
	for i := 5002; i <= 5100; i++ {
		logger.Write(logstore.LogEntry{
			Seq:           int64(i),
			SimTime:       501.0 + float64(i-5002)*0.1,
			EventType:     "BlockPropose",
			NodeID:        "N0",
			BlockHash:     "block_" + strconv.Itoa(i),
			BlockHeight:   i,
			ProposalIndex: i,
		})
	}
	logger.Close()

	loader := NewDataLoader(tempDir)

	// Filter with FromProposal: 5000
	events, err := loader.GetRunTimeline(runID, TimelineFilter{Limit: 100, FromProposal: 5000})
	if err != nil {
		t.Fatalf("failed to get timeline: %v", err)
	}

	if len(events) == 0 {
		t.Fatalf("expected events at or after proposal 5000, got 0")
	}

	// Verify that NO pre-5000 events are returned
	for _, ev := range events {
		if ev.ProposalIndex < 5000 {
			t.Errorf("found pre-5000 event in filtered timeline: %+v", ev)
		}
	}

	// Verify first event is the ConfigChange at proposal 5000
	if events[0].EventType != "config_change" || events[0].ProposalIndex != 5000 {
		t.Errorf("expected first event to be config_change at proposal 5000, got %+v", events[0])
	}

	// Verify ChurnToggle event carried effective proposal 5000
	if events[1].EventType != "churn" || events[1].ProposalIndex != 5000 {
		t.Errorf("expected second event to be churn at effective proposal 5000, got %+v", events[1])
	}

	// Verify competing fork at height 250 was correctly classified as "competing_fork"
	// even though block_250_main was proposed back at proposal 250!
	if events[2].EventType != "competing_fork" || events[2].Height != 250 {
		t.Errorf("expected third event to be competing_fork at height 250, got %+v", events[2])
	}
}
