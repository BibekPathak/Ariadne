package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"adriane/internal/agents"
	"adriane/internal/events"
)

type api struct {
	svc        *agents.AgentService
	bus        events.EventBus
	logger     *slog.Logger
	runTimeout time.Duration
	metrics    http.Handler
}

func routes(svc *agents.AgentService, bus events.EventBus, runTimeout time.Duration, logger *slog.Logger, metrics http.Handler) http.Handler {
	a := &api{svc: svc, bus: bus, logger: logger, runTimeout: runTimeout, metrics: metrics}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	if metrics != nil {
		mux.Handle("GET /metrics", metrics)
	}
	mux.HandleFunc("GET /templates", a.listTemplates)
	mux.HandleFunc("GET /agents", a.listAgents)
	mux.HandleFunc("POST /agents", a.createAgent)
	mux.HandleFunc("GET /agents/{id}", a.getAgent)
	mux.HandleFunc("POST /agents/{id}/run", a.rerunAgent)
	mux.HandleFunc("GET /agents/{id}/graph", a.getGraph)
	mux.HandleFunc("GET /agents/{id}/events", a.getEvents)
	mux.HandleFunc("GET /agents/{id}/events/stream", a.streamEvents)
	mux.HandleFunc("GET /agents/{id}/artifacts", a.getArtifacts)
	mux.HandleFunc("GET /agents/{id}/artifacts/{artifactID}/content", a.getArtifactContent)

	return corsMiddleware(logMiddleware(mux, logger))
}

// corsMiddleware permits the dashboard (browser origin) to call the API
// directly. Dev-permissive; tighten before exposing the API publicly.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).String())
	})
}

func (a *api) listTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"templates": a.svc.Templates()})
}

func (a *api) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := a.svc.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (a *api) createAgent(w http.ResponseWriter, r *http.Request) {
	var req agents.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
		return
	}
	agent, err := a.svc.Create(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Kick off the run asynchronously so the client can stream events.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.runTimeout)
		defer cancel()
		if err := a.svc.Run(ctx, agent.ID); err != nil {
			a.logger.Error("agent run failed", "agent", agent.ID, "err", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"agent_id": agent.ID,
		"status":   agent.Status,
	})
}

func (a *api) getAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := a.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

// rerunAgent resumes an existing agent as a new run. Task IDs are scoped per
// run, so runs never collide; memory stays keyed to the stable agent id.
func (a *api) rerunAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := a.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if agent.Status == agents.StatusRunning {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is already running"})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.runTimeout)
		defer cancel()
		if err := a.svc.Run(ctx, agent.ID); err != nil {
			a.logger.Error("agent re-run failed", "agent", agent.ID, "err", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"agent_id": agent.ID, "status": "running"})
}

func (a *api) getGraph(w http.ResponseWriter, r *http.Request) {
	tasks, err := a.svc.Graph(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (a *api) getEvents(w http.ResponseWriter, r *http.Request) {
	evs, err := a.svc.Events(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs})
}

func (a *api) getArtifacts(w http.ResponseWriter, r *http.Request) {
	arts, err := a.svc.Artifacts(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": arts})
}

func (a *api) getArtifactContent(w http.ResponseWriter, r *http.Request) {
	data, err := a.svc.ArtifactContent(r.Context(), r.PathValue("id"), r.PathValue("artifactID"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// streamEvents replays the full event log then tails live updates via SSE.
func (a *api) streamEvents(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Replay historical events.
	historical, err := a.svc.Events(r.Context(), agentID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	for _, e := range historical {
		sendEvent(w, flusher, e)
	}

	sub, cancel := a.bus.Subscribe()
	defer cancel()
	lastSeq := int64(0)
	if len(historical) > 0 {
		lastSeq = historical[len(historical)-1].Seq
	}

	ctx := r.Context()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case e := <-sub:
			if e.AgentID != agentID {
				continue
			}
			if e.Seq <= lastSeq {
				continue
			}
			sendEvent(w, flusher, e)
		case <-ticker.C:
			// Resync from the store in case an event was missed between the
			// replay and the subscription taking effect.
			evs, err := a.svc.StreamAfter(r.Context(), agentID, lastSeq)
			if err == nil && len(evs) > 0 {
				for _, e := range evs {
					if e.Seq > lastSeq {
						sendEvent(w, flusher, e)
						lastSeq = e.Seq
					}
				}
			} else {
				_, _ = fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
			}
		case <-ctx.Done():
			return
		}
	}
}

func sendEvent(w http.ResponseWriter, flusher http.Flusher, e events.Event) {
	raw, _ := json.Marshal(e)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	flusher.Flush()
}
