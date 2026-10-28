package dashboard

import (
	"time"

	"github.com/pranav-k/forkrisk/internal/metrics"
)

// HealthResponse represents the API status and version.
type HealthResponse struct {
	Status     string    `json:"status"`
	Version    string    `json:"version"`
	ServerTime time.Time `json:"server_time"`
	Milestones []int     `json:"milestones"`
	GoVersion  string    `json:"go_version"`
}

// RunSummaryItem represents a summary of a single simulation run.
type RunSummaryItem struct {
	RunID          string  `json:"run_id"`
	Scenario       string  `json:"scenario"`
	Seed           int64   `json:"seed"`
	ProposedBlocks int     `json:"proposed_blocks"`
	Status         string  `json:"status"`
	ForkRate       float64 `json:"fork_rate,omitempty"`
	StaleRate      float64 `json:"stale_rate,omitempty"`
	LogPath        string  `json:"log_path"`
}

// RunDetail contains the complete configuration, metrics, and summary for a run.
type RunDetail struct {
	RunID          string              `json:"run_id"`
	Scenario       string              `json:"scenario"`
	Seed           int64               `json:"seed"`
	ProposedBlocks int                 `json:"proposed_blocks"`
	Config         interface{}         `json:"config"`
	Metrics        *metrics.RunMetrics `json:"metrics,omitempty"`
	NodeCount      int                 `json:"node_count"`
	TotalEvents    int                 `json:"total_events"`
	LogAvailable   bool                `json:"log_available"`
	SummaryOnly    bool                `json:"summary_only"`
	Message        string              `json:"message,omitempty"`
}

// NodeStatus represents the observed state of an individual node in a completed run.
type NodeStatus struct {
	NodeID  string `json:"node_id"`
	Height  int    `json:"height"`
	TipHash string `json:"tip_hash"`
	Online  bool   `json:"online"`
	Peers   int    `json:"peers"`
	State   string `json:"state,omitempty"` // "active", "offline", "unrecorded"
}

// TimelineFilter defines pagination and query filtering parameters for timeline events.
type TimelineFilter struct {
	Limit        int     `json:"limit"`
	Offset       int     `json:"offset"`
	FromProposal int     `json:"from_proposal"`
	FromSimTime  float64 `json:"from_sim_time"`
	FromSeq      int64   `json:"from_seq"`
}

// TimelineEvent represents a key discrete simulation event for the timeline view.
type TimelineEvent struct {
	Seq           int64   `json:"seq,omitempty"`
	ProposalIndex int     `json:"proposal_index,omitempty"`
	EventType     string  `json:"event_type"` // proposal, competing_fork, reorg, orphan, churn, config_change
	SimTime       float64 `json:"sim_time"`
	NodeID        string  `json:"node_id"`
	Height        int     `json:"height"`
	BlockHash     string  `json:"block_hash"`
	Details       string  `json:"details"`
}

// ScenarioSummaryItem represents aggregated metrics for an experiment scenario.
type ScenarioSummaryItem struct {
	ScenarioName  string  `json:"scenario_name"`
	RunCount      int     `json:"run_count"`
	ForkRateMean  float64 `json:"fork_rate_mean"`
	ForkRateStd   float64 `json:"fork_rate_std"`
	StaleRateMean float64 `json:"stale_rate_mean"`
	StaleRateStd  float64 `json:"stale_rate_std"`
	PropDelayMean float64 `json:"prop_delay_mean"`
	PropDelayP95  float64 `json:"prop_delay_p95"`
	GoodputMean   float64 `json:"goodput_mean"`
	NodeConsMean  float64 `json:"node_consistency_mean"`
}

// ModelEvaluationMetrics represents evaluation metrics for a single model.
type ModelEvaluationMetrics struct {
	ModelName string   `json:"model_name"`
	ROCAUC    *float64 `json:"roc_auc"`
	PRAUC     *float64 `json:"pr_auc"`
	F1Score   float64  `json:"f1_score"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
	Brier     float64  `json:"brier_score"`
	Threshold float64  `json:"threshold"`
	TP        int      `json:"tp"`
	FP        int      `json:"fp"`
	FN        int      `json:"fn"`
	TN        int      `json:"tn"`
}

// EvaluationScope contains evaluation metrics for a specific evaluation protocol (e.g. Production Matrix vs Pilot).
type EvaluationScope struct {
	ScopeID     string                            `json:"scope_id"`
	ScopeName   string                            `json:"scope_name"`
	SourceFile  string                            `json:"source_file"`
	Description string                            `json:"description"`
	TestRuns    int                               `json:"test_runs"`
	TestSamples int                               `json:"test_samples"`
	Prevalence  float64                           `json:"prevalence"`
	Horizon     int                               `json:"horizon"`
	Models      map[string]ModelEvaluationMetrics `json:"models"`
}

// EvaluationResponse aggregates model evaluation across production and pilot scopes.
type EvaluationResponse struct {
	ActiveScope string                     `json:"active_scope"`
	Production  *EvaluationScope           `json:"production"`
	Pilot       *EvaluationScope           `json:"pilot"`
	Scopes      map[string]EvaluationScope `json:"scopes"`
	// Root-level fields default to the authoritative Production scope for backwards compatibility
	TestSamples int                               `json:"test_samples"`
	Prevalence  float64                           `json:"prevalence"`
	Horizon     int                               `json:"horizon"`
	Models      map[string]ModelEvaluationMetrics `json:"models"`
}

// DriftDelayPercentiles holds distribution statistics of detection delay among detected seeds.
type DriftDelayPercentiles struct {
	Min    int     `json:"min"`
	P25    float64 `json:"p25"`
	Median float64 `json:"median"`
	Mean   float64 `json:"mean"`
	P75    float64 `json:"p75"`
	P95    float64 `json:"p95"`
	Max    int     `json:"max"`
}

// DriftSummaryResponse represents sliding-window drift detector outputs derived from verified experiment artifacts.
type DriftSummaryResponse struct {
	WindowSize                     int                    `json:"window_size"`
	ThresholdZ                     float64                `json:"threshold_z"`
	Feature                        string                 `json:"monitored_feature"`
	Scenario                       string                 `json:"scenario"`
	TransitionProposal             int                    `json:"transition_proposal"`
	TestSeedsCount                 int                    `json:"test_seeds_count"`
	DetectedSeedsCount             int                    `json:"detected_seeds_count"`
	UndetectedSeedsCount           int                    `json:"undetected_seeds_count"`
	TransitionDetectionRate        float64                `json:"transition_detection_rate"`
	PreTransitionFalsePositiveRate float64                `json:"pre_transition_false_positive_rate"`
	PostTransitionOngoingAlarmRate float64                `json:"post_transition_ongoing_alarm_rate"`
	MeanDetectionDelayProposals    float64                `json:"mean_detection_delay_proposals"`
	DetectionDelayProposals        *DriftDelayPercentiles `json:"detection_delay_proposals,omitempty"`
	// Truthful legacy aliases for backward compatibility:
	FalsePositive float64 `json:"false_positive_rate_baseline"` // Pre-transition FPR
	Recall        float64 `json:"post_transition_recall"`       // Transition detection rate (1.0 = 100%)
	MeanDelay     float64 `json:"mean_detection_delay"`         // Mean detection delay
	Status        string  `json:"status"`
	Notes         string  `json:"notes,omitempty"`
}

// HorizonPoint represents sensitivity data for a single horizon H.
type HorizonPoint struct {
	Horizon    int      `json:"horizon"`
	TotalRows  int      `json:"total_rows"`
	TestRows   int      `json:"test_rows"`
	Prevalence float64  `json:"prevalence"`
	LR_ROCAUC  *float64 `json:"lr_roc_auc"`
	LR_PRAUC   *float64 `json:"lr_pr_auc"`
	LR_F1      float64  `json:"lr_f1"`
	RF_ROCAUC  *float64 `json:"rf_roc_auc"`
	RF_PRAUC   *float64 `json:"rf_pr_auc"`
	RF_F1      float64  `json:"rf_f1"`
}
