package dashboard

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Handler provides HTTP handlers for the ForkRisk dashboard API.
type Handler struct {
	Loader *DataLoader
}

// NewHandler creates a new Handler with the given DataLoader.
func NewHandler(loader *DataLoader) *Handler {
	return &Handler{Loader: loader}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{
		"error":  msg,
		"status": status,
	})
}

// EnsureGet ensures the request method is GET or HEAD. Returns true if valid, false if rejected.
func EnsureGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed. Only read-only GET requests are permitted.")
		return false
	}
	return true
}

// HandleHealth returns dashboard health and version information.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if !EnsureGet(w, r) {
		return
	}
	res := HealthResponse{
		Status:     "ok",
		Version:    "1.0.0",
		ServerTime: time.Now().UTC(),
		Milestones: []int{1, 2, 3, 4, 5, 6},
		GoVersion:  runtime.Version(),
	}
	writeJSON(w, http.StatusOK, res)
}

// HandleRuns returns a summary list of all available simulation runs.
func (h *Handler) HandleRuns(w http.ResponseWriter, r *http.Request) {
	if !EnsureGet(w, r) {
		return
	}
	runs, err := h.Loader.ListRuns()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total": len(runs),
		"runs":  runs,
	})
}

// HandleRunDetail routes requests for a run and its recorded telemetry.
func (h *Handler) HandleRunDetail(w http.ResponseWriter, r *http.Request) {
	if !EnsureGet(w, r) {
		return
	}

	// Path expected: /api/runs/<runID> or /api/runs/<runID>/<action>
	path := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "Missing run_id in request path")
		return
	}

	runID := parts[0]
	if err := ValidateRunID(runID); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid run_id format: path traversal or invalid characters detected")
		return
	}

	if len(parts) == 1 {
		detail, err := h.Loader.GetRunDetail(runID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, "Unknown run sub-resource")
		return
	}

	action := parts[1]
	switch action {
	case "logs", "replay":
		h.handleRecording(w, r, runID, action == "replay")
	case "replay-index":
		dir, err := h.Loader.findRunDir(runID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		index, err := indexRecording(r.Context(), dir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not index the run recording")
			return
		}
		writeJSON(w, http.StatusOK, index)
	case "forecast":
		dir, err := h.Loader.findRunDir(runID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		position, err := strconv.Atoi(r.URL.Query().Get("position"))
		if err != nil || position < 1 {
			writeError(w, http.StatusBadRequest, "Provide a positive recorded event position")
			return
		}
		forecast, err := h.Loader.getReplayForecast(r.Context(), dir, position)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not compute the replay forecast")
			return
		}
		writeJSON(w, http.StatusOK, forecast)
	case "nodes":
		nodes, err := h.Loader.GetRunNodes(runID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		logAvail := h.Loader.HasEventLog(runID)
		resp := map[string]interface{}{
			"run_id":        runID,
			"nodes":         nodes,
			"log_available": logAvail,
		}
		if !logAvail {
			resp["message"] = "Node status snapshots are unavailable because event log is omitted from this packaged run."
		}
		writeJSON(w, http.StatusOK, resp)
	case "timeline":
		filter := TimelineFilter{
			Limit: 100,
		}
		q := r.URL.Query()
		if qLimit := q.Get("limit"); qLimit != "" {
			if l, err := strconv.Atoi(qLimit); err == nil && l > 0 {
				filter.Limit = l
			}
		}
		if filter.Limit > 500 {
			filter.Limit = 500 // Documented maximum safety cap
		}
		if qOffset := q.Get("offset"); qOffset != "" {
			if off, err := strconv.Atoi(qOffset); err == nil && off >= 0 {
				filter.Offset = off
			}
		}
		if qFromProp := q.Get("from_proposal"); qFromProp != "" {
			if fp, err := strconv.Atoi(qFromProp); err == nil && fp >= 0 {
				filter.FromProposal = fp
			}
		}
		if qFromTime := q.Get("from_time"); qFromTime != "" {
			if ft, err := strconv.ParseFloat(qFromTime, 64); err == nil && ft >= 0 {
				filter.FromSimTime = ft
			}
		}
		if qFromSeq := q.Get("from_seq"); qFromSeq != "" {
			if fs, err := strconv.ParseInt(qFromSeq, 10, 64); err == nil && fs >= 0 {
				filter.FromSeq = fs
			}
		}

		events, err := h.Loader.GetRunTimeline(runID, filter)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		logAvail := h.Loader.HasEventLog(runID)
		resp := map[string]interface{}{
			"run_id":        runID,
			"events":        events,
			"limit":         filter.Limit,
			"offset":        filter.Offset,
			"from_proposal": filter.FromProposal,
			"log_available": logAvail,
		}
		if !logAvail {
			resp["message"] = "Timeline events are unavailable because event log is omitted from this packaged run."
		}
		writeJSON(w, http.StatusOK, resp)
	default:
		writeError(w, http.StatusNotFound, "Unknown run sub-resource")
	}
}

func (h *Handler) handleRecording(w http.ResponseWriter, r *http.Request, runID string, replay bool) {
	dir, err := h.Loader.findRunDir(runID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	offset, _ := strconv.Atoi(query.Get("offset"))
	filter := recordingFilter{limit: min(limit, 500), offset: max(offset, 0)}
	if !replay {
		filter.eventType = strings.TrimSpace(query.Get("type"))
		filter.node = strings.TrimSpace(query.Get("node"))
		filter.query = strings.TrimSpace(query.Get("q"))
	}
	page, err := readRecording(r.Context(), dir, filter, replay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read the run recording")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// HandleScenariosSummary returns aggregated experiment matrix results.
func (h *Handler) HandleScenariosSummary(w http.ResponseWriter, r *http.Request) {
	if !EnsureGet(w, r) {
		return
	}
	summary, err := h.Loader.GetScenariosSummary()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_scenarios": len(summary),
		"scenarios":       summary,
	})
}

// HandleModelEvaluation returns model evaluation statistics and confusion matrices.
func (h *Handler) HandleModelEvaluation(w http.ResponseWriter, r *http.Request) {
	if !EnsureGet(w, r) {
		return
	}
	eval, err := h.Loader.GetModelEvaluation()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, eval)
}

// HandleDrift returns drift detector configuration and alarms.
func (h *Handler) HandleDrift(w http.ResponseWriter, r *http.Request) {
	if !EnsureGet(w, r) {
		return
	}
	drift, err := h.Loader.GetDriftSummary()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, drift)
}

// HandleSensitivity returns the horizon sensitivity sweep ($H=1..5$).
func (h *Handler) HandleSensitivity(w http.ResponseWriter, r *http.Request) {
	if !EnsureGet(w, r) {
		return
	}
	pts, err := h.Loader.GetHorizonSensitivity()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"horizons": pts,
	})
}
