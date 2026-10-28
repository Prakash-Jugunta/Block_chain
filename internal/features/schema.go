package features

// FeatureName represents the canonical identifier of an engineered feature.
type FeatureName string

const (
	FeatPeerConnectivity    = "peer_connectivity"
	FeatLatencyMean10       = "latency_mean_10"
	FeatLatencyVar10        = "latency_var_10"
	FeatPropTimeMean10      = "prop_time_mean_10"
	FeatChurnRate100        = "churn_rate_100"
	FeatTipHeightVar        = "tip_height_var"
	FeatCompetingTips       = "competing_tips"
	FeatNodeViewDisagree    = "node_view_disagreement"
	FeatAvgBlockInterval10  = "avg_block_interval_10"
	FeatBlockIntervalVar10  = "block_interval_var_10"
	FeatStaleRate10         = "stale_rate_10"
	FeatTimeSinceLastFork   = "time_since_last_fork"
	FeatForkCount10         = "fork_count_10"
	FeatReorgCount10        = "reorg_count_10"
	FeatPendingTxCount      = "pending_tx_count"
	FeatTxBacklogGrowth10   = "tx_backlog_growth_10"
	FeatProcessedTPS10      = "processed_tps_10"
	FeatCfgMeanLatencyMs    = "cfg_mean_latency_ms"
	FeatCfgMeanPropDelayMs  = "cfg_mean_prop_delay_ms"
	FeatCfgBlockIntervalSec = "cfg_block_interval_sec"
	FeatCfgTxArrivalRate    = "cfg_tx_arrival_rate"
	FeatCfgBlockCapacity    = "cfg_block_capacity"
)

// FeatureSchemaVersion is the version identifier for the engineered feature schema.
// Incremented to v1.3 following:
// 1. Churn event consumption fix: consuming engine ChurnToggle events (with node_id and reason) to correctly compute peer_connectivity and churn_rate_100.
// 2. Churn-rate observation window correction: sizing churn_rate_100 over unique block heights (with denominator = min(100, M) where M is available unique block heights).
// 3. Clarification of height-level sampling unit: one row per eligible unique run/block-height anchored to the first proposal event at that height.
const FeatureSchemaVersion = "v1.3"

// CanonicalFeatures defines the strict ordering and names of model input features.
var CanonicalFeatures = []string{
	FeatPeerConnectivity,
	FeatLatencyMean10,
	FeatLatencyVar10,
	FeatPropTimeMean10,
	FeatChurnRate100,
	FeatTipHeightVar,
	FeatCompetingTips,
	FeatNodeViewDisagree,
	FeatAvgBlockInterval10,
	FeatBlockIntervalVar10,
	FeatStaleRate10,
	FeatTimeSinceLastFork,
	FeatForkCount10,
	FeatReorgCount10,
	FeatPendingTxCount,
	FeatTxBacklogGrowth10,
	FeatProcessedTPS10,
	FeatCfgMeanLatencyMs,
	FeatCfgMeanPropDelayMs,
	FeatCfgBlockIntervalSec,
	FeatCfgTxArrivalRate,
	FeatCfgBlockCapacity,
}

// MetaColumns defines the identifier and metadata columns in dataset.csv.
var MetaColumns = []string{
	"run_id",
	"scenario",
	"seed",
	"block_height",
	"sim_time",
}

// TargetColumn is the binary target variable for next-H-blocks prediction.
const TargetColumn = "label_fork_h3"

// DefaultForecastHorizon is H=3 blocks as specified in the case study plan.
const DefaultForecastHorizon = 3

// DatasetRow represents one row in the prediction dataset.
type DatasetRow struct {
	RunID       string             `json:"run_id"`
	Scenario    string             `json:"scenario"`
	Seed        int64              `json:"seed"`
	BlockHeight int                `json:"block_height"`
	SimTime     float64            `json:"sim_time"`
	Features    map[string]float64 `json:"features"`
	LabelForkH  int                `json:"label_fork_h3"`
}

// ToCSVRow converts the dataset row into ordered string values matching the CSV header.
func (r *DatasetRow) ToCSVRow() []string {
	row := make([]string, 0, len(MetaColumns)+len(CanonicalFeatures)+1)
	row = append(row, r.RunID)
	row = append(row, r.Scenario)
	row = append(row, formatInt64(r.Seed))
	row = append(row, formatInt(r.BlockHeight))
	row = append(row, formatFloat(r.SimTime, 4))

	for _, feat := range CanonicalFeatures {
		val := r.Features[feat]
		row = append(row, formatFloat(val, 6))
	}
	row = append(row, formatInt(r.LabelForkH))
	return row
}

// CSVHeader returns the complete list of column names for dataset.csv.
func CSVHeader() []string {
	header := make([]string, 0, len(MetaColumns)+len(CanonicalFeatures)+1)
	header = append(header, MetaColumns...)
	header = append(header, CanonicalFeatures...)
	header = append(header, TargetColumn)
	return header
}
