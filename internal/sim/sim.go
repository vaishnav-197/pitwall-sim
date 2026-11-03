// Package sim stress-tests plans with random safety cars and lap-time noise.
//
// The optimizer assumes a clean race. Real races have safety cars, and
// pitting under one costs far less time because the field is slowed down.
// A plan that looks 3 seconds slower on paper can win most real races if its
// pit window lines up with when safety cars tend to happen. Monte Carlo runs
// show that trade-off.
package sim

import (
	"errors"
	"math"
	"math/rand"
	"sort"

	"github.com/vaishnav-197/pitwall-sim/internal/strategy"
	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

// Config controls the simulation.
type Config struct {
	SCProbPerLap float64 `json:"sc_prob_per_lap"` // chance a safety car starts on any lap
	SCMinLaps    int     `json:"sc_min_laps"`
	SCMaxLaps    int     `json:"sc_max_laps"`
	SCPitFactor  float64 `json:"sc_pit_factor"` // pit loss multiplier under SC, e.g. 0.5
	SCLapFactor  float64 `json:"sc_lap_factor"` // lap time multiplier under SC, e.g. 1.4
	LapNoise     float64 `json:"lap_noise"`     // std dev of random lap-time noise, seconds
	// ReactWindow lets the team bring a planned stop forward when a safety
	// car is out and the stop is due within this many laps. 0 = stick to plan.
	ReactWindow int   `json:"react_window"`
	Runs        int   `json:"runs"`
	Seed        int64 `json:"seed"`
}

// DefaultConfig returns rough, illustrative values.
func DefaultConfig() Config {
	return Config{
		SCProbPerLap: 0.02, SCMinLaps: 3, SCMaxLaps: 5,
		SCPitFactor: 0.5, SCLapFactor: 1.4, LapNoise: 0.25,
		Runs: 5000, Seed: 1,
	}
}

// Stats summarises one plan over all runs.
type Stats struct {
	Plan     strategy.Plan `json:"plan"`
	Mean     float64       `json:"mean"`
	Std      float64       `json:"std"`
	P10      float64       `json:"p10"`
	P50      float64       `json:"p50"`
	P90      float64       `json:"p90"`
	WinShare float64       `json:"win_share"` // fraction of runs where this plan was fastest
}

func (c Config) validate() error {
	switch {
	case c.Runs < 1 || c.Runs > 100000:
		return errors.New("runs must be 1..100000")
	case c.SCProbPerLap < 0 || c.SCProbPerLap > 1:
		return errors.New("sc_prob_per_lap must be 0..1")
	case c.SCMinLaps < 1 || c.SCMaxLaps < c.SCMinLaps:
		return errors.New("need 1 <= sc_min_laps <= sc_max_laps")
	case c.ReactWindow < 0:
		return errors.New("react_window must be >= 0")
	case c.SCPitFactor < 0 || c.SCLapFactor < 1 || c.LapNoise < 0:
		return errors.New("sc_pit_factor >= 0, sc_lap_factor >= 1, lap_noise >= 0")
	}
	return nil
}

// Run simulates every plan against the same random safety-car timelines
// (common random numbers), so differences between plans come from the
// plans, not from luck.
func Run(m *tyre.Model, plans []strategy.Plan, cfg Config) ([]Stats, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, errors.New("no plans to simulate")
	}
	for _, p := range plans {
		if _, err := strategy.RaceTime(m, p); err != nil {
			return nil, err
		}
	}

	rng := rand.New(rand.NewSource(cfg.Seed))
	laps := m.Race.Laps
	times := make([][]float64, len(plans))
	for i := range times {
		times[i] = make([]float64, cfg.Runs)
	}
	wins := make([]int, len(plans))
	underSC := make([]bool, laps+1)

	for run := 0; run < cfg.Runs; run++ {
		// One safety-car timeline per run, shared by every plan.
		for i := range underSC {
			underSC[i] = false
		}
		for lap := 1; lap <= laps; lap++ {
			if !underSC[lap] && rng.Float64() < cfg.SCProbPerLap {
				n := cfg.SCMinLaps + rng.Intn(cfg.SCMaxLaps-cfg.SCMinLaps+1)
				for l := lap; l < lap+n && l <= laps; l++ {
					underSC[l] = true
				}
			}
		}

		bestT, bestI := math.Inf(1), 0
		for i, p := range plans {
			t := raceTime(m, p, underSC, cfg, rng)
			times[i][run] = t
			if t < bestT {
				bestT, bestI = t, i
			}
		}
		wins[bestI]++
	}

	out := make([]Stats, len(plans))
	for i, p := range plans {
		out[i] = summarise(p, times[i], float64(wins[i])/float64(cfg.Runs))
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Mean < out[b].Mean })
	return out, nil
}

// raceTime drives one plan through one safety-car timeline. Stops happen at
// the end of a lap. With ReactWindow > 0, a stop due within that many laps is
// taken early if the safety car is out, which is what real teams do.
func raceTime(m *tyre.Model, p strategy.Plan, underSC []bool, cfg Config, rng *rand.Rand) float64 {
	laps := m.Race.Laps
	pits := append([]int(nil), p.PitLaps...)
	stint, age, total := 0, 0, 0.0
	for lap := 1; lap <= laps; lap++ {
		t := m.LapTime(m.Compounds[p.Stints[stint].Compound], age, lap)
		if underSC[lap] {
			t *= cfg.SCLapFactor
		}
		total += t + rng.NormFloat64()*cfg.LapNoise
		age++

		if len(pits) == 0 || lap == laps {
			continue
		}
		due := pits[0] - lap
		early := cfg.ReactWindow > 0 && underSC[lap] && due > 0 && due <= cfg.ReactWindow
		if due == 0 || early {
			loss := m.Race.PitLoss
			if underSC[lap] {
				loss *= cfg.SCPitFactor
			}
			total += loss
			pits = pits[1:]
			stint++
			age = 0
		}
	}
	return total
}

func summarise(p strategy.Plan, ts []float64, winShare float64) Stats {
	s := append([]float64(nil), ts...)
	sort.Float64s(s)
	mean := 0.0
	for _, t := range s {
		mean += t
	}
	mean /= float64(len(s))
	v := 0.0
	for _, t := range s {
		v += (t - mean) * (t - mean)
	}
	pct := func(q float64) float64 { return s[int(q*float64(len(s)-1))] }
	return Stats{
		Plan: p, Mean: mean, Std: math.Sqrt(v / float64(len(s))),
		P10: pct(0.1), P50: pct(0.5), P90: pct(0.9), WinShare: winShare,
	}
}
