package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pranav-k/forkrisk/internal/logstore"
)

type recordingFilter struct {
	limit, offset int
	eventType     string
	node          string
	query         string
}

type recordedEvent struct {
	Position int             `json:"position"`
	Proposal int             `json:"proposal"`
	Entry    json.RawMessage `json:"entry"`
	Node     *NodeStatus     `json:"node,omitempty"`
	Block    *replayBlock    `json:"block,omitempty"`
}

type recordingPage struct {
	Events        []recordedEvent `json:"events"`
	Offset        int             `json:"offset"`
	HasMore       bool            `json:"has_more"`
	LogAvailable  bool            `json:"log_available"`
	InitialNodes  []NodeStatus    `json:"initial_nodes,omitempty"`
	StartTime     float64         `json:"start_time"`
	InitialBlocks []replayBlock   `json:"initial_blocks,omitempty"`
	PriorBlock    int             `json:"prior_block,omitempty"`
	NextBlock     int             `json:"next_block,omitempty"`
}

type replayBlock struct {
	Position int     `json:"position"`
	Height   int     `json:"height"`
	Hash     string  `json:"hash"`
	Parent   string  `json:"parent"`
	Node     string  `json:"node"`
	SimTime  float64 `json:"sim_time"`
	Branch   int     `json:"branch,omitempty"`
	Fork     bool    `json:"fork,omitempty"`
}

type replayBranches struct {
	branches   map[string]int
	firstChild map[string]string
}

func newReplayBranches() replayBranches {
	return replayBranches{branches: make(map[string]int), firstChild: make(map[string]string)}
}

func (history replayBranches) record(entry logstore.LogEntry, position int) *replayBlock {
	branch, seen := history.branches[entry.BlockHash]
	sibling := history.firstChild[entry.ParentHash]
	fork := !seen && entry.ParentHash != "" && sibling != "" && sibling != entry.BlockHash
	if !seen {
		branch = history.branches[entry.ParentHash]
		if fork {
			branch = position
		}
		history.branches[entry.BlockHash] = branch
	}
	if sibling == "" {
		history.firstChild[entry.ParentHash] = entry.BlockHash
	}
	return &replayBlock{Position: position, Height: entry.BlockHeight, Hash: entry.BlockHash, Parent: entry.ParentHash, Node: entry.NodeID, SimTime: entry.SimTime, Branch: branch, Fork: fork}
}

const replayBlockHistory = 32

// readRecording uses file order because event sequence numbers reflect scheduling,
// and can repeat or appear out of order when events are recorded.
func readRecording(ctx context.Context, dir string, filter recordingFilter, replay bool) (recordingPage, error) {
	page := recordingPage{Events: make([]recordedEvent, 0, filter.limit), Offset: filter.offset}
	file, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if os.IsNotExist(err) {
		return page, nil
	}
	if err != nil {
		return page, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return page, err
	}
	page.LogAvailable = true

	nodes := make(map[string]NodeStatus)
	branches := newReplayBranches()
	recentBlocks := make([]replayBlock, 0, replayBlockHistory)
	if replay {
		loadReplayNodes(dir, nodes)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var position, matched, proposal int
	search := strings.ToLower(filter.query)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		raw := scanner.Bytes()
		var entry logstore.LogEntry
		if json.Unmarshal(raw, &entry) != nil || entry.EventType == "" {
			continue
		}
		position++
		proposal = max(proposal, entry.ProposalIndex, entry.AtBlock)
		if filter.eventType != "" && !strings.EqualFold(entry.EventType, filter.eventType) {
			continue
		}
		if filter.node != "" && !strings.EqualFold(entry.NodeID, filter.node) && !strings.EqualFold(entry.FromNode, filter.node) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(string(raw)), search) {
			continue
		}
		isBlock := entry.EventType == "BlockPropose" && entry.BlockHash != ""
		if len(page.Events) == filter.limit {
			page.HasMore = true
			if !replay || isBlock {
				if replay {
					page.NextBlock = position
				}
				break
			}
			continue
		}
		var block *replayBlock
		if replay && isBlock {
			block = branches.record(entry, position)
		}
		if matched < filter.offset {
			matched++
			if replay {
				applyRecordedNodeEvent(nodes, entry)
				page.StartTime = entry.SimTime
				if block != nil {
					page.PriorBlock = position
					if len(recentBlocks) == replayBlockHistory {
						copy(recentBlocks, recentBlocks[1:])
						recentBlocks = recentBlocks[:replayBlockHistory-1]
					}
					recentBlocks = append(recentBlocks, *block)
				}
			}
			continue
		}
		if replay && len(page.Events) == 0 {
			page.InitialNodes = replayNodeSnapshot(nodes)
		}
		event := recordedEvent{Position: position, Proposal: proposal, Entry: append(json.RawMessage(nil), raw...)}
		if replay {
			event.Node = applyRecordedNodeEvent(nodes, entry)
			event.Block = block
		}
		page.Events = append(page.Events, event)
	}
	if replay && len(page.Events) == 0 {
		page.Offset = matched
		page.InitialNodes = replayNodeSnapshot(nodes)
	}
	page.InitialBlocks = recentBlocks
	return page, scanner.Err()
}

type replayIndex struct {
	LogAvailable bool          `json:"log_available"`
	TotalEvents  int           `json:"total_events"`
	Forks        []replayBlock `json:"forks"`
}

func indexRecording(ctx context.Context, dir string) (replayIndex, error) {
	index := replayIndex{Forks: []replayBlock{}}
	file, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if os.IsNotExist(err) {
		return index, nil
	}
	if err != nil {
		return index, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return index, err
	}
	index.LogAvailable = true
	branches := newReplayBranches()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return index, err
		}
		var entry logstore.LogEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.EventType == "" {
			continue
		}
		index.TotalEvents++
		if entry.EventType == "BlockPropose" && entry.BlockHash != "" {
			block := branches.record(entry, index.TotalEvents)
			if block.Fork {
				index.Forks = append(index.Forks, *block)
			}
		}
	}
	return index, scanner.Err()
}

func loadReplayNodes(dir string, nodes map[string]NodeStatus) {
	var meta struct {
		Config struct {
			NumNodes int `json:"num_nodes"`
		} `json:"config"`
		NodeTipHeights map[string]int `json:"node_tip_heights"`
	}
	if data, err := os.ReadFile(filepath.Join(dir, "metadata.json")); err != nil || json.Unmarshal(data, &meta) != nil {
		return
	}
	for id := range meta.NodeTipHeights {
		nodes[id] = NodeStatus{NodeID: id, State: "unrecorded"}
	}
	for i := 0; i < meta.Config.NumNodes; i++ {
		id := "N" + strconv.Itoa(i)
		nodes[id] = NodeStatus{NodeID: id, State: "unrecorded"}
	}
}

func applyRecordedNodeEvent(nodes map[string]NodeStatus, entry logstore.LogEntry) *NodeStatus {
	id := entry.NodeID
	if id == "" && entry.EventType == "BlockPropose" {
		id = entry.ProposerID
	}
	if id == "" {
		return nil
	}
	status := nodes[id]
	status.NodeID = id
	if status.State == "" || status.State == "unrecorded" {
		status.State, status.Online = "active", true
		status.TipHash = ""
	}
	switch entry.EventType {
	case "ChurnToggle":
		switch entry.Reason {
		case "offline":
			status.State, status.Online = "offline", false
		case "online":
			status.State, status.Online = "active", true
		}
	case "BlockPropose", "BlockArrive", "TipUpdate":
		if entry.TipHash != "" {
			status.TipHash, status.Height = entry.TipHash, entry.TipHeight
		} else if entry.EventType == "BlockPropose" && entry.BlockHash != "" {
			status.TipHash, status.Height = entry.BlockHash, entry.BlockHeight
		}
	}
	nodes[id] = status
	return &status
}

func replayNodeSnapshot(nodes map[string]NodeStatus) []NodeStatus {
	snapshot := make([]NodeStatus, 0, len(nodes))
	for _, state := range nodes {
		snapshot = append(snapshot, state)
	}
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].NodeID < snapshot[j].NodeID })
	return snapshot
}
