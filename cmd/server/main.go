// Command server exposes the flakiness tracker over a JSON HTTP API.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/quarantine"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/service"
)

type api struct {
	svc *service.Service
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *api) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/batches", a.ingest)
	mux.HandleFunc("GET /v1/flakiness", a.reports)
	mux.HandleFunc("POST /v1/quarantine/reevaluate", a.reevaluate)
	mux.HandleFunc("GET /v1/quarantine", a.quarantine)
	mux.HandleFunc("GET /v1/quarantine/audit", a.quarantineAudit)
	mux.HandleFunc("PUT /v1/baselines/{name}", a.setBaseline)
	mux.HandleFunc("GET /v1/baselines/{name}/compare", a.compareBaseline)
	mux.HandleFunc("POST /v1/gate", a.gate)
	mux.HandleFunc("POST /v1/retention/prune", a.prune)
	return mux
}

func (a *api) ingest(w http.ResponseWriter, r *http.Request) {
	var b model.Batch
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if err := a.svc.Ingest(b); err != nil {
		code := http.StatusBadRequest
		if reason, ok := model.IsReject(err); ok {
			if reason == model.ReasonConflictingEntry {
				code = http.StatusConflict
			}
			writeJSON(w, code, map[string]string{"error": err.Error(), "reason": string(reason)})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (a *api) reports(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.svc.Reports())
}

func (a *api) reevaluate(w http.ResponseWriter, r *http.Request) {
	var p quarantine.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, a.svc.ReevaluateQuarantine(p))
}

func (a *api) quarantine(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"members": a.svc.QuarantineMembers()})
}

func (a *api) quarantineAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.svc.QuarantineAudit())
}

func (a *api) setBaseline(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a.svc.SetBaseline(name)
	writeJSON(w, http.StatusOK, map[string]string{"baseline": name})
}

func (a *api) compareBaseline(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	comp, ok := a.svc.CompareBaseline(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown baseline " + name})
		return
	}
	writeJSON(w, http.StatusOK, comp)
}

func (a *api) gate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BatchIDs []string `json:"batch_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	d, err := a.svc.Gate(req.BatchIDs)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *api) prune(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int{"removed": a.svc.Prune()})
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	retain := flag.Int("retain-runs", 100, "number of run batches to retain (0 = unlimited)")
	window := flag.Int("window-runs", 0, "analysis window in runs (0 = all retained)")
	minSamples := flag.Int("min-samples", 3, "minimum pass/fail samples for a deterministic verdict")
	flag.Parse()

	cfg := service.Config{
		Flakiness: flakiness.Config{
			WindowRuns:          *window,
			MinSamples:          *minSamples,
			FlakyThreshold:      0.0,
			PersistentThreshold: 1.0,
		},
		Policy: quarantine.Policy{
			MinSamples:     *minSamples,
			MaxFailureRate: 1.0,
			Version:        "v1",
		},
		RetainRuns: *retain,
	}
	a := &api{svc: service.New(cfg)}
	log.Printf("flakiness tracker listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, a.routes()))
}
