package features

import (
	"strconv"

	"github.com/pranav-k/forkrisk/internal/config"
	"github.com/pranav-k/forkrisk/internal/fork"
	"github.com/pranav-k/forkrisk/internal/logstore"
)

// Extractor extracts causal rolling-window features at each block proposal point.
type Extractor struct {
	nodeIDs      []string
	warmUpBlocks int
	initCfg      config.ScenarioConfig
}

// NewExtractor creates an Extractor initialized with the node list, warm-up blocks,
// and initial scenario configuration.
func NewExtractor(nodeIDs []string, warmUpBlocks int, initCfg config.ScenarioConfig) *Extractor {
	return &Extractor{
		nodeIDs:      nodeIDs,
		warmUpBlocks: warmUpBlocks,
		initCfg:      initCfg,
	}
}

// ExtractFeaturesAt computes the canonical feature vector at block proposal height `h`
// using strictly events with SimTime <= predTime (or seq <= predSeq).
func (ex *Extractor) ExtractFeaturesAt(
	events []logstore.LogEntry,
	proposals []BlockProposalInfo,
	currentIdx int,
) map[string]float64 {
	return ex.ExtractFeaturesAtWithDetection(events, proposals, currentIdx, nil)
}

// ExtractFeaturesAtWithDetection computes the canonical feature vector, optionally reusing
// a precomputed DetectionResult to avoid redundant event passes while strictly enforcing causality.
func (ex *Extractor) ExtractFeaturesAtWithDetection(
	events []logstore.LogEntry,
	proposals []BlockProposalInfo,
	currentIdx int,
	detection *fork.DetectionResult,
) map[string]float64 {
	currentProposal := proposals[currentIdx]
	predTime := currentProposal.SimTime
	predHeight := currentProposal.Height

	// Strictly filter events <= predTime to guarantee no future leakage.
	// In the event of identical SimTime, do not consume events after currentProposal's Seq.
	endIdx := 0
	for i := range events {
		e := &events[i]
		if e.SimTime > predTime {
			break
		}
		endIdx = i + 1
		if e.EventType == "BlockPropose" && e.BlockHeight == predHeight && e.SimTime == predTime {
			break
		}
	}
	pastEvents := events[:endIdx]

	features := make(map[string]float64, len(CanonicalFeatures))

	// 1. Dynamic Configuration Parameters (accounting for ConfigChange events <= predTime)
	activeMeanLatency := ex.initCfg.MeanLatencyMs
	activeMeanProp := ex.initCfg.MeanPropDelayMs
	activeBlockInterval := ex.initCfg.BlockIntervalSec
	activeTxArrival := ex.initCfg.TxArrivalRate
	activeBlockCap := float64(ex.initCfg.BlockCapacity)

	for _, e := range pastEvents {
		if e.EventType == "ConfigChange" {
			switch e.ConfigField {
			case "mean_latency_ms":
				if v, err := strconv.ParseFloat(e.ConfigNewValue, 64); err == nil {
					activeMeanLatency = v
				}
			case "mean_prop_delay_ms":
				if v, err := strconv.ParseFloat(e.ConfigNewValue, 64); err == nil {
					activeMeanProp = v
				}
			case "block_interval_sec":
				if v, err := strconv.ParseFloat(e.ConfigNewValue, 64); err == nil {
					activeBlockInterval = v
				}
			case "tx_arrival_rate":
				if v, err := strconv.ParseFloat(e.ConfigNewValue, 64); err == nil {
					activeTxArrival = v
				}
			case "block_capacity":
				if v, err := strconv.ParseFloat(e.ConfigNewValue, 64); err == nil {
					activeBlockCap = v
				}
			}
		}
	}

	features[FeatCfgMeanLatencyMs] = activeMeanLatency
	features[FeatCfgMeanPropDelayMs] = activeMeanProp
	features[FeatCfgBlockIntervalSec] = activeBlockInterval
	features[FeatCfgTxArrivalRate] = activeTxArrival
	features[FeatCfgBlockCapacity] = activeBlockCap

	// 2. Peer Availability / Connectivity & Online Nodes
	onlineNodes := make(map[string]bool)
	for _, nid := range ex.nodeIDs {
		onlineNodes[nid] = true // initial state: all online
	}

	// Sizing churn_rate_100 over unique block heights:
	// Find all unique block heights observed up to currentIdx and their earliest proposal SimTime.
	type uniqueHeightEntry struct {
		height    int
		firstTime float64
	}
	var uniqueHeights []uniqueHeightEntry
	seenHeights := make(map[int]bool)
	for i := 0; i <= currentIdx; i++ {
		h := proposals[i].Height
		if !seenHeights[h] {
			seenHeights[h] = true
			uniqueHeights = append(uniqueHeights, uniqueHeightEntry{
				height:    h,
				firstTime: proposals[i].SimTime,
			})
		}
	}

	mUnique := len(uniqueHeights)
	churnWindowStart := 0.0
	churnDenom := 100.0
	if mUnique >= 100 {
		// Window spans exactly the last 100 unique block heights.
		// Start boundary is the earliest proposal SimTime of uniqueHeights[mUnique - 100].
		churnWindowStart = uniqueHeights[mUnique-100].firstTime
		churnDenom = 100.0
	} else {
		// Early-run window before 100 unique block heights are available.
		// Start boundary is 0.0 (simulation start, capturing all initial churn events).
		// Denominator is the count of available unique block heights.
		churnWindowStart = 0.0
		if mUnique > 0 {
			churnDenom = float64(mUnique)
		} else {
			churnDenom = 1.0
		}
	}

	var churnEventsCount100 float64
	for _, e := range pastEvents {
		if e.EventType == "ChurnToggle" || e.EventType == "PeerChurn" {
			if e.Reason == "offline" {
				onlineNodes[e.NodeID] = false
			} else if e.Reason == "online" {
				onlineNodes[e.NodeID] = true
			}
			if e.SimTime >= churnWindowStart {
				churnEventsCount100++
			}
		}
	}

	onlineCount := 0
	for _, nid := range ex.nodeIDs {
		if onlineNodes[nid] {
			onlineCount++
		}
	}
	if len(ex.nodeIDs) > 0 {
		features[FeatPeerConnectivity] = float64(onlineCount) / float64(len(ex.nodeIDs))
	} else {
		features[FeatPeerConnectivity] = 1.0
	}
	features[FeatChurnRate100] = churnEventsCount100 / churnDenom

	// 3. Node-View Consensus / Disagreement & Competing Tips at prediction point
	nodeTips := make(map[string]string)
	nodeTipHeights := make(map[string]int)
	for _, nid := range ex.nodeIDs {
		nodeTips[nid] = ""
		nodeTipHeights[nid] = 0
	}

	for _, e := range pastEvents {
		switch e.EventType {
		case "BlockPropose", "TipUpdate", "BlockArrive":
			if e.TipHash != "" {
				nodeTips[e.NodeID] = e.TipHash
				nodeTipHeights[e.NodeID] = e.TipHeight
			} else if e.BlockHash != "" && (e.EventType == "BlockPropose" || e.EventType == "TipUpdate") {
				nodeTips[e.NodeID] = e.BlockHash
				nodeTipHeights[e.NodeID] = e.BlockHeight
			}
		}
	}

	maxHeight := 0
	var tipHeightVals []float64
	for _, h := range nodeTipHeights {
		tipHeightVals = append(tipHeightVals, float64(h))
		if h > maxHeight {
			maxHeight = h
		}
	}
	features[FeatTipHeightVar] = variance(tipHeightVals)

	tipsAtMax := make(map[string]int)
	tipCounts := make(map[string]int)
	for _, nid := range ex.nodeIDs {
		tHash := nodeTips[nid]
		if tHash != "" {
			tipCounts[tHash]++
			if nodeTipHeights[nid] == maxHeight {
				tipsAtMax[tHash]++
			}
		}
	}

	features[FeatCompetingTips] = float64(len(tipsAtMax))
	if features[FeatCompetingTips] == 0 {
		features[FeatCompetingTips] = 1.0
	}

	maxMajorityTipCount := 0
	for _, c := range tipCounts {
		if c > maxMajorityTipCount {
			maxMajorityTipCount = c
		}
	}
	if len(ex.nodeIDs) > 0 {
		features[FeatNodeViewDisagree] = 1.0 - (float64(maxMajorityTipCount) / float64(len(ex.nodeIDs)))
	} else {
		features[FeatNodeViewDisagree] = 0.0
	}

	// 4. Rolling Block Intervals & Delays
	// A. Block interval window: up to 10 consecutive intervals requires up to 11 proposal timestamps.
	intervalStartIdx := currentIdx - 10
	if intervalStartIdx < 0 {
		intervalStartIdx = 0
	}
	intervalProposals := proposals[intervalStartIdx : currentIdx+1]

	var blockIntervals []float64
	for i := 1; i < len(intervalProposals); i++ {
		interval := intervalProposals[i].SimTime - intervalProposals[i-1].SimTime
		if interval >= 0 {
			blockIntervals = append(blockIntervals, interval)
		}
	}
	features[FeatAvgBlockInterval10] = mean(blockIntervals)
	if features[FeatAvgBlockInterval10] == 0 {
		features[FeatAvgBlockInterval10] = activeBlockInterval
	}
	features[FeatBlockIntervalVar10] = variance(blockIntervals)

	// B. Proposal record window: exactly the last 10 proposal records ending at currentIdx.
	// When currentIdx >= 9, this slice contains exactly 10 proposals [currentIdx-9, currentIdx].
	propWindowStartIdx := currentIdx - 9
	if propWindowStartIdx < 0 {
		propWindowStartIdx = 0
	}
	recentProposals := proposals[propWindowStartIdx : currentIdx+1]

	// Collect arrivals for blocks in the 10-proposal window
	windowBlockHashes := make(map[string]float64)
	for _, wp := range recentProposals {
		windowBlockHashes[wp.Hash] = wp.SimTime
	}

	var delays []float64
	propMaxTimes := make(map[string]float64)
	for _, e := range pastEvents {
		if e.EventType == "BlockArrive" && e.BlockHash != "" {
			if propTime, ok := windowBlockHashes[e.BlockHash]; ok {
				d := e.SimTime - propTime
				if d >= 0 {
					delays = append(delays, d)
					if e.SimTime > propMaxTimes[e.BlockHash] {
						propMaxTimes[e.BlockHash] = e.SimTime
					}
				}
			}
		}
	}

	features[FeatLatencyMean10] = mean(delays)
	features[FeatLatencyVar10] = variance(delays)

	var propTimes []float64
	for bHash, pStart := range windowBlockHashes {
		if pMax, ok := propMaxTimes[bHash]; ok {
			propTimes = append(propTimes, pMax-pStart)
		}
	}
	features[FeatPropTimeMean10] = mean(propTimes)

	// 5. Fork and Reorganization History (strictly <= predTime)
	var detectionResult fork.DetectionResult
	if detection != nil {
		detectionResult = *detection
	} else {
		var forkEvents []fork.Event
		for _, pe := range pastEvents {
			forkEvents = append(forkEvents, fork.Event{
				Seq:         pe.Seq,
				SimTime:     pe.SimTime,
				EventType:   pe.EventType,
				NodeID:      pe.NodeID,
				BlockHash:   pe.BlockHash,
				BlockHeight: pe.BlockHeight,
				ProposerID:  pe.ProposerID,
				ParentHash:  pe.ParentHash,
				TipHash:     pe.TipHash,
				TipHeight:   pe.TipHeight,
				OldTipHash:  pe.OldTipHash,
				Reason:      pe.Reason,
				TxCount:     pe.TxCount,
			})
		}
		detector := fork.NewDetector(ex.nodeIDs, ex.warmUpBlocks)
		detectionResult = detector.Analyze(forkEvents)
	}

	tenBlocksAgoTime := recentProposals[0].SimTime
	reorgCount10 := 0
	for _, re := range detectionResult.Reorganizations {
		if re.Time >= tenBlocksAgoTime && re.Time <= predTime {
			reorgCount10++
		}
	}
	features[FeatReorgCount10] = float64(reorgCount10)

	forkCount10 := 0
	lastForkBirthTime := -1.0
	for _, ep := range detectionResult.ForkEpisodes {
		if ep.BirthTime <= predTime {
			if ep.BirthTime > lastForkBirthTime {
				lastForkBirthTime = ep.BirthTime
			}
			if ep.BirthTime >= tenBlocksAgoTime {
				forkCount10++
			}
		}
	}
	features[FeatForkCount10] = float64(forkCount10)

	if lastForkBirthTime >= 0 {
		features[FeatTimeSinceLastFork] = predTime - lastForkBirthTime
	} else {
		warmupTime := 0.0
		if len(proposals) > ex.warmUpBlocks {
			warmupTime = proposals[ex.warmUpBlocks].SimTime
		}
		features[FeatTimeSinceLastFork] = predTime - warmupTime
	}

	staleHashes := make(map[string]bool)
	for _, sb := range detectionResult.StaleBlocks {
		staleHashes[sb.Hash] = true
	}

	staleInWindow := 0
	for _, wp := range recentProposals {
		if staleHashes[wp.Hash] {
			staleInWindow++
		}
	}

	if len(recentProposals) > 0 {
		features[FeatStaleRate10] = float64(staleInWindow) / float64(len(recentProposals))
	} else {
		features[FeatStaleRate10] = 0.0
	}

	// 6. Mempool & Transaction Load Dynamics
	features[FeatPendingTxCount] = float64(currentProposal.PoolSize)
	pastWindowProposal := recentProposals[0]
	features[FeatTxBacklogGrowth10] = float64(currentProposal.PoolSize - pastWindowProposal.PoolSize)

	totalProcessedInWindow := 0
	for _, wp := range recentProposals {
		totalProcessedInWindow += wp.TxCount
	}
	windowDuration := currentProposal.SimTime - pastWindowProposal.SimTime
	if windowDuration > 0 {
		features[FeatProcessedTPS10] = float64(totalProcessedInWindow) / windowDuration
	} else {
		features[FeatProcessedTPS10] = 0.0
	}

	return features
}
