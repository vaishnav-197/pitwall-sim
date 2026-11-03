package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const raceJSON = `"race":{"laps":20,"fuel_per_lap":0.03,"pit_loss":20,"min_compounds":2,"max_stops":2},
"compounds":{"soft":{"base":90,"lin":0.1,"quad":0.004},"hard":{"base":91,"lin":0.04,"quad":0.001}}`

func post(body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/strategy", strings.NewReader(body)))
	return rec
}

func TestStrategyWithSimulation(t *testing.T) {
	rec := post(`{` + raceJSON + `,"sim":{"sc_prob_per_lap":0.05,"sc_min_laps":2,"sc_max_laps":4,
		"sc_pit_factor":0.5,"sc_lap_factor":1.4,"lap_noise":0.2,"runs":200,"seed":1,"react_window":3}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp StrategyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Plans) == 0 || len(resp.Simulation) != len(resp.Plans) {
		t.Fatalf("plans=%d sims=%d", len(resp.Plans), len(resp.Simulation))
	}
}

func TestStrategyWithoutSimulation(t *testing.T) {
	rec := post(`{` + raceJSON + `}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "simulation") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestStrategyErrors(t *testing.T) {
	cases := map[string]struct {
		body string
		want int
	}{
		"bad json":       {`{`, http.StatusBadRequest},
		"unknown field":  {`{"lapz":3}`, http.StatusBadRequest},
		"invalid race":   {`{"race":{"laps":1},"compounds":{"soft":{"base":90}}}`, http.StatusUnprocessableEntity},
		"no legal plan":  {`{"race":{"laps":10,"min_compounds":2,"max_stops":0},"compounds":{"soft":{"base":90},"hard":{"base":91}}}`, http.StatusUnprocessableEntity},
		"bad sim config": {`{` + raceJSON + `,"sim":{"runs":0}}`, http.StatusUnprocessableEntity},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := post(c.body); rec.Code != c.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}
