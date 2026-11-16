package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/eventpulse/eventpulse/internal/metrics"
	"github.com/eventpulse/eventpulse/internal/store"
)

func pathUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

func queryUUID(r *http.Request, key string) (uuid.UUID, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(v)
	return id, err == nil
}

func paging(r *http.Request) (limit, offset int32) {
	l, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	o, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if l <= 0 || l > 200 {
		l = 50
	}
	if o < 0 {
		o = 0
	}
	return int32(l), int32(o)
}

func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(b), nil
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(raw) <= 2048
}

func (s *Server) fail(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, what+" not found")
		return
	}
	s.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// ---- applications ----

func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string     `json:"name"`
		OwnerID *uuid.UUID `json:"owner_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 255 {
		writeError(w, http.StatusBadRequest, "name is required (max 255 chars)")
		return
	}
	owner := s.cfg.DefaultOwnerID
	if in.OwnerID != nil {
		owner = *in.OwnerID
	}
	app, err := s.q.CreateApplication(r.Context(), store.CreateApplicationParams{Name: in.Name, OwnerID: owner})
	if err != nil {
		s.fail(w, err, "create application")
		return
	}
	writeJSON(w, http.StatusCreated, app)
}

func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r)
	apps, err := s.q.ListApplications(r.Context(), store.ListApplicationsParams{Limit: limit, Offset: offset})
	if err != nil {
		s.fail(w, err, "list applications")
		return
	}
	if apps == nil {
		apps = []store.Application{}
	}
	writeJSON(w, http.StatusOK, apps)
}

func (s *Server) getApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	app, err := s.q.GetApplication(r.Context(), id)
	if err != nil {
		s.fail(w, err, "application")
		return
	}
	writeJSON(w, http.StatusOK, app)
}

// ---- endpoints ----

func (s *Server) createEndpoint(w http.ResponseWriter, r *http.Request) {
	appID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validURL(in.URL) {
		writeError(w, http.StatusBadRequest, "url must be an absolute http(s) URL")
		return
	}
	if _, err := s.q.GetApplication(r.Context(), appID); err != nil {
		s.fail(w, err, "application")
		return
	}
	secret, err := newSecret()
	if err != nil {
		s.fail(w, err, "generate secret")
		return
	}
	ep, err := s.q.CreateEndpoint(r.Context(), store.CreateEndpointParams{AppID: appID, Url: in.URL, Secret: secret})
	if err != nil {
		s.fail(w, err, "create endpoint")
		return
	}
	writeJSON(w, http.StatusCreated, ep)
}

func (s *Server) listEndpoints(w http.ResponseWriter, r *http.Request) {
	appID, ok := queryUUID(r, "app_id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid app_id")
		return
	}
	limit, offset := paging(r)
	eps, err := s.q.ListEndpoints(r.Context(), store.ListEndpointsParams{AppID: appID, RowLimit: limit, RowOffset: offset})
	if err != nil {
		s.fail(w, err, "list endpoints")
		return
	}
	if eps == nil {
		eps = []store.Endpoint{}
	}
	writeJSON(w, http.StatusOK, eps)
}

func (s *Server) updateEndpoint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	var in struct {
		URL      *string `json:"url"`
		IsActive *bool   `json:"is_active"`
	}
	if !decode(w, r, &in) {
		return
	}
	cur, err := s.q.GetEndpoint(r.Context(), id)
	if err != nil {
		s.fail(w, err, "endpoint")
		return
	}
	if in.URL != nil {
		if !validURL(*in.URL) {
			writeError(w, http.StatusBadRequest, "url must be an absolute http(s) URL")
			return
		}
		cur.Url = *in.URL
	}
	if in.IsActive != nil {
		cur.IsActive = *in.IsActive
	}
	ep, err := s.q.UpdateEndpoint(r.Context(), store.UpdateEndpointParams{ID: id, Url: cur.Url, IsActive: cur.IsActive})
	if err != nil {
		s.fail(w, err, "update endpoint")
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

func (s *Server) rotateSecret(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	secret, err := newSecret()
	if err != nil {
		s.fail(w, err, "generate secret")
		return
	}
	ep, err := s.q.RotateEndpointSecret(r.Context(), store.RotateEndpointSecretParams{ID: id, Secret: secret})
	if err != nil {
		s.fail(w, err, "endpoint")
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

func (s *Server) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	n, err := s.q.DeleteEndpoint(r.Context(), id)
	if err != nil {
		s.fail(w, err, "delete endpoint")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- events ----

func (s *Server) ingestEvent(w http.ResponseWriter, r *http.Request) {
	appID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	var in struct {
		EventType string          `json:"event_type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.EventType = strings.TrimSpace(in.EventType)
	if in.EventType == "" || len(in.EventType) > 255 {
		writeError(w, http.StatusBadRequest, "event_type is required (max 255 chars)")
		return
	}
	if len(in.Payload) == 0 || !json.Valid(in.Payload) {
		writeError(w, http.StatusBadRequest, "payload must be valid JSON")
		return
	}
	if !s.limiter.Allow(r.Context(), "app:"+appID.String(), s.cfg.IngestRatePerSec) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	// Event and its delivery fan-out commit atomically.
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.fail(w, err, "begin tx")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	q := s.q.WithTx(tx)

	if _, err := q.GetApplication(r.Context(), appID); err != nil {
		s.fail(w, err, "application")
		return
	}
	ev, err := q.CreateEvent(r.Context(), store.CreateEventParams{AppID: appID, EventType: in.EventType, Payload: in.Payload})
	if err != nil {
		s.fail(w, err, "create event")
		return
	}
	ids, err := q.FanOutEvent(r.Context(), store.FanOutEventParams{EventID: ev.ID, AppID: appID})
	if err != nil {
		s.fail(w, err, "fan out event")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.fail(w, err, "commit")
		return
	}
	metrics.EventsIngested.Inc()
	writeJSON(w, http.StatusAccepted, map[string]any{"event_id": ev.ID, "deliveries": len(ids)})
}

// ---- deliveries ----

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	appID, ok := queryUUID(r, "app_id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid app_id")
		return
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "", "pending", "success", "failed":
	default:
		writeError(w, http.StatusBadRequest, "status must be pending, success or failed")
		return
	}
	limit, offset := paging(r)
	rows, err := s.q.ListDeliveries(r.Context(), store.ListDeliveriesParams{AppID: appID, StatusFilter: status, RowLimit: limit, RowOffset: offset})
	if err != nil {
		s.fail(w, err, "list deliveries")
		return
	}
	if rows == nil {
		rows = []store.ListDeliveriesRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) redeliver(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	n, err := s.q.RedeliverDelivery(r.Context(), id)
	if err != nil {
		s.fail(w, err, "redeliver")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "delivery not found")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// ---- stats ----

type seriesPoint struct {
	Time    time.Time `json:"time"`
	Success int64     `json:"success"`
	Failed  int64     `json:"failed"`
	Pending int64     `json:"pending"`
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	mins, _ := strconv.Atoi(r.URL.Query().Get("window"))
	if mins <= 0 || mins > 1440 {
		mins = 60
	}
	now := time.Now().UTC().Truncate(time.Minute)
	since := now.Add(-time.Duration(mins-1) * time.Minute)

	rows, err := s.q.DeliveryStatsSince(r.Context(), since)
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	series := make([]seriesPoint, mins)
	idx := make(map[int64]int, mins)
	for i := range series {
		t := since.Add(time.Duration(i) * time.Minute)
		series[i].Time = t
		idx[t.Unix()] = i
	}
	var totals seriesPoint
	for _, row := range rows {
		i, ok := idx[row.Bucket.UTC().Unix()]
		if !ok {
			continue
		}
		switch row.Status {
		case store.DeliveryStatusSuccess:
			series[i].Success += row.Total
			totals.Success += row.Total
		case store.DeliveryStatusFailed:
			series[i].Failed += row.Total
			totals.Failed += row.Total
		default:
			series[i].Pending += row.Total
			totals.Pending += row.Total
		}
	}
	active, err := s.q.CountActiveEndpoints(r.Context())
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	backlog, err := s.q.CountPendingDeliveries(r.Context())
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	var rate float64
	if done := totals.Success + totals.Failed; done > 0 {
		rate = float64(totals.Success) / float64(done)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window_minutes":   mins,
		"active_endpoints": active,
		"backlog":          backlog,
		"totals":           map[string]int64{"success": totals.Success, "failed": totals.Failed, "pending": totals.Pending},
		"success_rate":     rate,
		"series":           series,
	})
}
