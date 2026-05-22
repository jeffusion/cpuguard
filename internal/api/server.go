package api

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"cpuguard/internal/engine"
	"cpuguard/internal/model"
)

type Server struct {
	engine     *engine.Engine
	configPath string
	socketPath string
	server     *http.Server
}

func NewServer(eng *engine.Engine, configPath, socketPath string) *Server {
	s := &Server{
		engine:     eng,
		configPath: configPath,
		socketPath: socketPath,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/rules", s.handleRules)
	mux.HandleFunc("/v1/subjects", s.handleSubjects)
	mux.HandleFunc("/v1/limits", s.handleLimits)
	mux.HandleFunc("/v1/events", s.handleEvents)
	mux.HandleFunc("/v1/protected", s.handleProtected)
	mux.HandleFunc("/v1/reload", s.handleReload)
	mux.HandleFunc("/v1/subjects/", s.handleSubjectAction)
	s.server = &http.Server{Handler: mux}
	return s
}

func (s *Server) ListenAndServe() error {
	_ = os.Remove(s.socketPath)
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.socketPath, 0o660); err != nil {
		return err
	}
	log.Printf("cpuguard api listening on unix://%s", s.socketPath)
	return s.server.Serve(listener)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	summary, err := s.engine.SubjectSummary()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, model.DaemonStatus{
		System:  s.engine.SystemMetrics(),
		Summary: convertSubjectSummaryToTotalCPUPercent(summary, s.engine.CPUCount()),
	})
}

func (s *Server) handleRules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.engine.Rules())
}

func (s *Server) handleSubjects(w http.ResponseWriter, r *http.Request) {
	query := parseSubjectQuery(r)
	page, err := s.engine.QuerySubjects(query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, convertSubjectPageToTotalCPUPercent(page, s.engine.CPUCount()))
}

func (s *Server) handleLimits(w http.ResponseWriter, _ *http.Request) {
	subjects, err := s.engine.ListLimits()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, convertSubjectsToTotalCPUPercent(subjects, s.engine.CPUCount()))
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	query, err := parseEventQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	events, err := s.engine.QueryEvents(query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, convertEventsToTotalCPUPercent(events, s.engine.CPUCount()))
}

func (s *Server) handleProtected(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.engine.ProtectedPolicy())
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, nil)
		return
	}
	if err := s.engine.Reload(r.Context(), s.configPath); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func parseEventQuery(r *http.Request) (model.EventQuery, error) {
	values := r.URL.Query()
	limit, _ := strconv.Atoi(values.Get("limit"))
	query := model.EventQuery{
		SubjectID: values.Get("subject_id"),
		Type:      values.Get("type"),
		Limit:     limit,
	}
	if raw := values.Get("since"); raw != "" {
		since, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return model.EventQuery{}, err
		}
		query.Since = since
	}
	return query, nil
}

func parseSubjectQuery(r *http.Request) model.SubjectQuery {
	values := r.URL.Query()
	offset, _ := strconv.Atoi(values.Get("offset"))
	limit, _ := strconv.Atoi(values.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	sortKey := values.Get("sort")
	switch sortKey {
	case "state", "cpu", "limit", "scope", "target", "rule":
	default:
		sortKey = "cpu"
	}
	dir := values.Get("dir")
	if dir != "asc" {
		dir = "desc"
	}
	return model.SubjectQuery{
		Offset:     offset,
		Limit:      limit,
		Sort:       sortKey,
		Dir:        dir,
		SelectedID: values.Get("selected_id"),
	}
}

func (s *Server) handleSubjectAction(w http.ResponseWriter, r *http.Request) {
	subjectID, action, ok := parseSubjectAction(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, nil)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var err error
	switch action {
	case "unthrottle":
		err = s.engine.Unthrottle(r.Context(), subjectID)
	case "hold":
		err = s.engine.Hold(subjectID)
	case "throttle":
		err = s.engine.ThrottleNow(r.Context(), subjectID)
	default:
		writeError(w, http.StatusNotFound, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func parseSubjectAction(path string) (string, string, bool) {
	const prefix = "/v1/subjects/"
	if len(path) <= len(prefix) || path[:len(prefix)] != prefix {
		return "", "", false
	}
	rest := path[len(prefix):]
	lastSlash := -1
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] == '/' {
			lastSlash = i
			break
		}
	}
	if lastSlash < 1 || lastSlash == len(rest)-1 {
		return "", "", false
	}
	return rest[:lastSlash], rest[lastSlash+1:], true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	if err == nil {
		_, _ = w.Write([]byte(`{"error":"request failed"}`))
		return
	}
	_, _ = w.Write([]byte(`{"error":` + strconv.Quote(err.Error()) + `}`))
}

func convertSubjectsToTotalCPUPercent(subjects []model.Subject, cpuCount int) []model.Subject {
	out := append([]model.Subject(nil), subjects...)
	for i := range out {
		out[i].LastCPUPct = corePercentToTotalPercent(out[i].LastCPUPct, cpuCount)
		out[i].CurrentLimitPct = corePercentToTotalPercent(out[i].CurrentLimitPct, cpuCount)
	}
	return out
}

func convertSubjectPageToTotalCPUPercent(page model.SubjectPage, cpuCount int) model.SubjectPage {
	page.Items = convertSubjectsToTotalCPUPercent(page.Items, cpuCount)
	if page.Selected != nil {
		selected := convertSubjectsToTotalCPUPercent([]model.Subject{*page.Selected}, cpuCount)[0]
		page.Selected = &selected
	}
	return page
}

func convertSubjectSummaryToTotalCPUPercent(summary model.SubjectSummary, cpuCount int) model.SubjectSummary {
	summary.MaxCPUPct = corePercentToTotalPercent(summary.MaxCPUPct, cpuCount)
	summary.AvgCPUPct = corePercentToTotalPercent(summary.AvgCPUPct, cpuCount)
	return summary
}

func convertEventsToTotalCPUPercent(events []model.Event, cpuCount int) []model.Event {
	out := append([]model.Event(nil), events...)
	for i := range out {
		out[i].CPUPct = corePercentToTotalPercent(out[i].CPUPct, cpuCount)
		out[i].LimitPct = corePercentToTotalPercent(out[i].LimitPct, cpuCount)
	}
	return out
}

func corePercentToTotalPercent(cpuPct float64, cpuCount int) float64 {
	if cpuCount < 1 {
		return cpuPct
	}
	return cpuPct / float64(cpuCount)
}
