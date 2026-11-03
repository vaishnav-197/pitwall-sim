// Package api serves the optimizer and simulator over HTTP.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/vaishnav-197/pitwall-sim/internal/sim"
	"github.com/vaishnav-197/pitwall-sim/internal/strategy"
	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

const maxBody = 64 << 10

// StrategyRequest is the body for POST /strategy. Leave Sim out to skip the
// Monte Carlo step.
type StrategyRequest struct {
	tyre.Model
	Sim *sim.Config `json:"sim,omitempty"`
}

// StrategyResponse lists the best plan per stop count, fastest first, and
// the simulation results if requested.
type StrategyResponse struct {
	Plans      []strategy.Plan `json:"plans"`
	Simulation []sim.Stats     `json:"simulation,omitempty"`
}

// New returns the HTTP handler.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /strategy", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		var req StrategyRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if err := req.Model.Validate(); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		plans, err := strategy.Optimize(&req.Model)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if len(plans) == 0 {
			writeErr(w, http.StatusUnprocessableEntity, "no legal plan: raise max_stops or lower min_compounds")
			return
		}
		resp := StrategyResponse{Plans: plans}
		if req.Sim != nil {
			if req.Sim.Runs > 20000 {
				req.Sim.Runs = 20000 // keep requests fast
			}
			stats, err := sim.Run(&req.Model, plans, *req.Sim)
			if err != nil {
				writeErr(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			resp.Simulation = stats
		}
		writeJSON(w, http.StatusOK, resp)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
