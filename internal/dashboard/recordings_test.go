package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordedRunsWithManifest(t *testing.T) {
	base := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		path = filepath.Join(base, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("experiments/milestone4/run_manifest.json", `{"runs":[{"run_id":"summary"}]}`)
	write("experiments/milestone4/runs/summary/metadata.json", `{"run_id":"summary"}`)
	write("runs/local/metadata.json", `{"run_id":"local","config":{"num_nodes":2}}`)
	write("runs/local/events.jsonl", `{"event_type":"BlockPropose","seq":8,"sim_time":1,"node_id":"N0","block_hash":"first","block_height":5,"parent_hash":"common","tip_hash":"first","tip_height":5,"proposal_index":10}
{"event_type":"ChurnToggle","seq":2,"sim_time":2,"node_id":"N1","reason":"offline"}
{"event_type":"TipUpdate","seq":8,"sim_time":3,"node_id":"N0","from_node":"N1","tip_hash":"second","tip_height":4,"reason":"reorg","custom":true}
{"event_type":"ChurnToggle","seq":9,"sim_time":4,"node_id":"N1","reason":"online"}
{"event_type":"BlockPropose","seq":10,"sim_time":5,"node_id":"N1","block_hash":"sibling","block_height":5,"parent_hash":"common","proposal_index":11}
{"event_type":"ConfigChange","seq":12,"sim_time":5.5,"config_field":"mean_latency_ms","config_new_value":"9"}
{"event_type":"BlockPropose","seq":11,"sim_time":6,"node_id":"N1","block_hash":"child","block_height":6,"parent_hash":"sibling","proposal_index":12}
`)
	write("ml/models/logistic_regression/model.json", `{"feature_schema_version":"v1.3","features":["cfg_mean_latency_ms"],"weights":[1],"threshold":0.6,"scaler":{"features":["cfg_mean_latency_ms"],"means":[0],"stds":[1]}}`)
	write("ml/models/random_forest/model.json", `{"feature_schema_version":"v1.3","features":["cfg_mean_latency_ms"],"trees":[{"root":{"is_leaf":true,"probability":0.82}}],"threshold":0.8}`)
	write("ml/dataset/dataset_metadata.json", `{"feature_schema_version":"v1.3","forecast_horizon":3,"warmup_blocks":0}`)
	server := NewServer(ServerConfig{DataDir: base})
	get := func(path string, target any) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d, body %s", path, response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
			t.Fatal(err)
		}
	}
	var listing struct{ Runs []RunSummaryItem }
	get("/api/runs", &listing)
	if len(listing.Runs) != 2 || listing.Runs[0].RunID != "local" || !listing.Runs[0].LogAvailable || listing.Runs[1].LogAvailable {
		t.Fatalf("unexpected run listing: %+v", listing.Runs)
	}

	var logs recordingPage
	get("/api/runs/local/logs?type=TipUpdate&node=N1&q=REORG&limit=1", &logs)
	if len(logs.Events) != 1 || logs.HasMore || logs.Events[0].Position != 3 || logs.Events[0].Proposal != 10 {
		t.Fatalf("unexpected filtered page: %+v", logs)
	}
	var original map[string]any
	if err := json.Unmarshal(logs.Events[0].Entry, &original); err != nil || original["custom"] != true {
		t.Fatalf("raw event field was lost: %s", logs.Events[0].Entry)
	}

	var replay recordingPage
	get("/api/runs/local/replay?offset=2&limit=1", &replay)
	if replay.StartTime != 2 || !replay.HasMore || len(replay.InitialNodes) != 2 || replay.InitialNodes[0].Height != 5 || replay.InitialNodes[1].State != "offline" {
		t.Fatalf("unexpected replay checkpoint: %+v", replay)
	}
	if len(replay.Events) != 1 || replay.Events[0].Position != 3 || replay.Events[0].Node == nil || replay.Events[0].Node.Height != 4 {
		t.Fatalf("unexpected replay update: %+v", replay.Events)
	}
	if replay.NextBlock != 5 || len(replay.InitialBlocks) != 1 || replay.InitialBlocks[0].Fork {
		t.Fatalf("replay checkpoint lost the first block or exposed a future fork: %+v", replay)
	}
	var branch recordingPage
	get("/api/runs/local/replay?offset=4&limit=3", &branch)
	if len(branch.Events) != 3 || branch.Events[0].Block == nil || branch.Events[2].Block == nil {
		t.Fatalf("expected two proposals after the replay checkpoint: %+v", branch)
	}
	fork, child := branch.Events[0].Block, branch.Events[2].Block
	if branch.PriorBlock != 1 || !fork.Fork || fork.Branch != 5 || child.Fork || child.Branch != fork.Branch {
		t.Fatalf("expected the second sibling to start a branch and its child to inherit it: %+v %+v", fork, child)
	}
	var early, later replayForecast
	get("/api/runs/local/forecast?position=1", &early)
	get("/api/runs/local/forecast?position=7", &later)
	if len(early.Scores) != 1 || len(later.Scores) != 2 || early.Scores[0] != later.Scores[0] ||
		later.Scores[0].Position != 1 || later.Scores[0].HighRisk || !later.Scores[1].HighRisk {
		t.Fatalf("forecast should score the first proposal from its own prefix: early=%+v later=%+v", early, later)
	}
	if later.Forest == nil || len(later.Forest.Scores) != 2 || later.Forest.Threshold != 0.8 || !later.Forest.Scores[0].HighRisk {
		t.Fatalf("expected comparison scores with the forest threshold: %+v", later.Forest)
	}
	var index replayIndex
	get("/api/runs/local/replay-index", &index)
	if !index.LogAvailable || index.TotalEvents != 7 || len(index.Forks) != 1 || index.Forks[0].Position != fork.Position {
		t.Fatalf("unexpected replay index: %+v", index)
	}
	var snapshot struct{ Nodes []NodeStatus }
	get("/api/runs/local/nodes", &snapshot)
	if len(snapshot.Nodes) != 2 || snapshot.Nodes[0].Height != 4 || snapshot.Nodes[1].State != "active" {
		t.Fatalf("unexpected final node snapshot: %+v", snapshot.Nodes)
	}

	var unavailable recordingPage
	get("/api/runs/summary/replay", &unavailable)
	if unavailable.LogAvailable || unavailable.Events == nil || len(unavailable.Events) != 0 {
		t.Fatalf("unexpected summary-only page: %+v", unavailable)
	}
}
