package features

import (
	"github.com/pranav-k/forkrisk/internal/fork"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

// Labeler computes the ground-truth prediction target for each block proposal.
type Labeler struct {
	horizon      int
	warmUpBlocks int
}

// NewLabeler creates a Labeler with the specified forecast horizon (default 3)
// and warm-up block cutoff.
func NewLabeler(horizon, warmUpBlocks int) *Labeler {
	if horizon <= 0 {
		horizon = DefaultForecastHorizon
	}
	return &Labeler{
		horizon:      horizon,
		warmUpBlocks: warmUpBlocks,
	}
}

// BlockProposalInfo captures timing and height for a proposed block.
type BlockProposalInfo struct {
	ProposalIndex int
	Height        int
	SimTime       float64
	Hash          string
	Proposer      string
	TxCount       int
	PoolSize      int
	OfferedTx     int
}

// ComputeLabels determines label_fork_h for each eligible block proposal.
// Returns a map from block height to binary label (0 or 1), and the list of eligible block heights.
//
// Eligibility rules:
//  1. Height > warmUpBlocks
//  2. Height <= maxBlockHeight - horizon (censoring boundary)
//
// Label definition:
//
//	label = 1 if at least one fork episode has its birth at height in (t, t+horizon]
//	        or birth time in (T_t, T_{t+horizon}].
//	label = 0 otherwise.
func (l *Labeler) ComputeLabels(
	proposals []BlockProposalInfo,
	detection fork.DetectionResult,
) (map[int]int, []int) {
	labels := make(map[int]int)
	eligibleHeights := make([]int, 0, len(proposals))

	if len(proposals) == 0 {
		return labels, eligibleHeights
	}

	maxHeight := 0
	proposalMap := make(map[int]BlockProposalInfo)
	for _, p := range proposals {
		if _, exists := proposalMap[p.Height]; !exists {
			proposalMap[p.Height] = p
		}
		if p.Height > maxHeight {
			maxHeight = p.Height
		}
	}

	for h := l.warmUpBlocks + 1; h <= maxHeight-l.horizon; h++ {
		if _, exists := proposalMap[h]; !exists {
			continue
		}

		eligibleHeights = append(eligibleHeights, h)
		currentProposal := proposalMap[h]

		isForkNextH := 0
		for _, ep := range detection.ForkEpisodes {
			// A positive label requires a NEW fork birth occurring in the forecast window:
			// 1. Fork must be born strictly after current prediction time T_h.
			// 2. Fork birth height must be strictly in (h, h + horizon].
			// 3. Forks born before or at T_h that merely persist into the window are NOT new births.
			if ep.BirthTime > currentProposal.SimTime && ep.BirthHeight > h && ep.BirthHeight <= h+l.horizon {
				isForkNextH = 1
				break
			}
		}

		labels[h] = isForkNextH
	}

	return labels, eligibleHeights
}

// ComputeLabelsByProposal determines label_fork_h for each eligible proposal index (0-based).
// Returns a map from proposal index to binary label (0 or 1), and the list of eligible proposal indices.
//
// Eligibility rules:
//  1. ProposalIndex >= warmUpBlocks
//  2. ProposalIndex < len(proposals) - horizon
//
// Label definition:
//
//	label = 1 if at least one fork episode has its birth strictly in (T_p, T_{p+horizon}].
//	label = 0 otherwise.
func (l *Labeler) ComputeLabelsByProposal(
	proposals []BlockProposalInfo,
	detection fork.DetectionResult,
) (map[int]int, []int) {
	labels := make(map[int]int)
	var eligibleIndices []int

	n := len(proposals)
	if n <= l.warmUpBlocks+l.horizon {
		return labels, eligibleIndices
	}

	startIdx := l.warmUpBlocks
	endIdx := n - l.horizon

	for idx := startIdx; idx < endIdx; idx++ {
		eligibleIndices = append(eligibleIndices, idx)
		cur := proposals[idx]
		horizonEnd := proposals[idx+l.horizon]

		isForkNextH := 0
		for _, ep := range detection.ForkEpisodes {
			if ep.BirthTime > cur.SimTime && ep.BirthTime <= horizonEnd.SimTime {
				isForkNextH = 1
				break
			}
		}
		labels[idx] = isForkNextH
	}

	return labels, eligibleIndices
}

// ExtractProposals extracts structured proposal records from raw log entries.
func ExtractProposals(entries []logstore.LogEntry) []BlockProposalInfo {
	var proposals []BlockProposalInfo
	for idx, e := range entries {
		if e.EventType == "BlockPropose" {
			pIdx := e.ProposalIndex
			if pIdx <= 0 {
				pIdx = idx + 1
			}
			proposals = append(proposals, BlockProposalInfo{
				ProposalIndex: pIdx,
				Height:        e.BlockHeight,
				SimTime:       e.SimTime,
				Hash:          e.BlockHash,
				Proposer:      e.ProposerID,
				TxCount:       e.TxCount,
				PoolSize:      e.PoolSize,
				OfferedTx:     e.OfferedTx,
			})
		}
	}
	return proposals
}
