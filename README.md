# pitwall-sim

An F1 race-strategy engine in Go, with no dependencies. It does three things:

1. **Fit:** learns how each tyre compound wears and how much fuel weight costs per lap from raw lap times. It uses least-squares regression, written by hand.
2. **Plan:** finds the fastest legal pit strategy using dynamic programming. Legal means it uses at least two dry compounds and stays under a stop limit.
3. **Simulate:** runs thousands of races with random safety cars to see which plan actually wins most often. It also compares sticking to the plan with pitting early when a safety car comes out.

It runs as a CLI and as a JSON HTTP API.

```
lap times ─▶ fit (OLS) ─▶ tyre model ─▶ plan (DP) ─▶ best 1/2/3-stop plans ─▶ simulate (Monte Carlo) ─▶ which one wins
```

## Example

From `data/race.json`, a made-up 57-lap race with 22 s pit loss and three compounds that have a wear "cliff":

```
$ pitwall plan
best plan per stop count (57 laps, pit loss 22.0s, solved in 5ms):
  2-stop  soft×17 → medium×23 → soft×17            pit laps [17 40]      1:29:27.894  (+0.0s)
  3-stop  soft×14 → medium×15 → soft×14 → soft×14  pit laps [14 29 43]   1:29:29.895  (+2.0s)
  1-stop  hard×32 → medium×25                      pit laps [32]         1:29:40.673  (+12.8s)

safety cars 2%/lap, 5000 runs, stick to the plan:
  stops  plan                                             mean          p10          p90    wins
  2      soft×17 → medium×23 → soft×17             1:32:04.101  1:29:26.987  1:35:07.395   60.9%
  3      soft×14 → medium×15 → soft×14 → soft×14   1:32:04.472  1:29:28.991  1:35:07.670   38.8%
  1      hard×32 → medium×25                       1:32:18.710  1:29:39.767  1:35:22.991    0.3%

safety cars 2%/lap, 5000 runs, pit early under safety car if a stop is due within 5 laps:
  stops  plan                                             mean          p10          p90    wins
  3      soft×14 → medium×15 → soft×14 → soft×14   1:32:02.415  1:29:28.991  1:35:03.938   47.1%
  2      soft×17 → medium×23 → soft×17             1:32:03.174  1:29:26.987  1:35:05.591   52.6%
  1      hard×32 → medium×25                       1:32:17.704  1:29:39.767  1:35:21.655    0.3%
```

What this shows:

- **The 1-stop loses** even though it pits 1–2 fewer times. Running soft tyres hard and short beats managing hards.
- **The 3-stop is only 2 s slower on paper,** but it has more pit windows. A team that reacts to safety cars has more chances to catch one, which makes the 3-stop the best plan on average (mean time). It still wins fewer head-to-heads than the 2-stop: 47% vs 53%.
- **Reacting to safety cars saves about 1–2 s on average** for every multi-stop plan.

## How it works

### Lap-time model (`internal/tyre`)

```
lap time = base[c] + lin[c]·age + quad[c]·age² + cliff[c](age) + fuel · laps_remaining
```

`age` is how many laps the set has already done. The cliff adds a steep penalty per lap once a set passes its life. The fuel term makes early laps slower because the car is heavier.

### Fitting (`internal/fit`)

This is one joint ordinary least squares regression. Each compound gets three columns (intercept, age, age²), and one fuel column is shared by all of them, so fuel is learned from every lap, not separately per compound. I build the normal equations `XᵀX β = Xᵀy` directly and solve them with Gaussian elimination and partial pivoting. No linear algebra library.

`pitwall gen` creates synthetic practice long-runs from the "true" model with 0.15 s noise. `pitwall fit` learns from those laps:

| | base | lin | quad | fuel |
|---|---|---|---|---|
| soft, true | 91.200 | 0.0850 | 0.00400 | 0.0350 |
| soft, fitted | 91.277 | 0.0693 | 0.00443 | 0.0347 |
| hard, true | 92.400 | 0.0350 | 0.00080 | |
| hard, fitted | 92.439 | 0.0347 | 0.00076 | |

The fit has an RMSE of 0.144 s and R² of 0.947. Individual coefficients wobble, because linear and quadratic wear are correlated over a 15–25 lap run. But predicted stint times, which are what the optimizer uses, land close to the truth. I scored the plans from the fitted model with the true model, and they lost 0.000 s compared to planning with the truth.

Practice runs stop before the cliff, so cliffs can't be learned and come from the race file.

### Optimizer (`internal/strategy`)

The state after each lap is: compound on the car, age of that set, the set of compounds used so far (a bitmask) and stops made. From each state the car either stays out or pits for any compound, paying the pit loss. Each state keeps only its fastest history, so a 57-lap race with 3 compounds solves in about 5 ms instead of trying every combination of pit laps.

Parent pointers rebuild the actual plan. A pit for the same compound (soft → soft) shows up as the tyre age resetting.

The tests compare the optimizer to a brute-force search over every possible strategy on a 14-lap race, and also replay every returned plan lap by lap.

### Simulator (`internal/sim`)

Each run draws a safety-car timeline: 2% chance per lap, lasting 3–5 laps. Under a safety car, laps are 40% slower and a pit stop costs half as much. Every plan in a run sees the same safety cars (common random numbers), so the differences come from the plans, not from luck. Per-lap noise is added on top.

The reactive policy brings a planned stop forward if a safety car is out and the stop is due within N laps.

## Usage

Requires Go 1.22+.

```bash
make test                                 # unit tests with the race detector
go run ./cmd/pitwall gen                  # synthetic practice laps -> data/sample_laps.csv
go run ./cmd/pitwall fit -out tyres.json  # learn the tyre model from them
go run ./cmd/pitwall plan -model tyres.json
go run ./cmd/pitwall plan -sc 0.05 -react 3   # more safety cars, tighter reaction window
go run ./cmd/pitwall serve                # HTTP API on :8080
```

### API

`POST /strategy` takes the same JSON as `data/race.json`, plus an optional `sim` block:

```bash
curl -s localhost:8080/strategy -d '{
  "race": {"laps": 57, "fuel_per_lap": 0.035, "pit_loss": 22, "min_compounds": 2, "max_stops": 3},
  "compounds": {
    "soft":   {"base": 91.2, "lin": 0.085, "quad": 0.004,  "cliff_age": 16, "cliff_rate": 0.35},
    "medium": {"base": 91.8, "lin": 0.055, "quad": 0.0018, "cliff_age": 28, "cliff_rate": 0.30},
    "hard":   {"base": 92.4, "lin": 0.035, "quad": 0.0008, "cliff_age": 40, "cliff_rate": 0.25}
  },
  "sim": {"sc_prob_per_lap": 0.02, "sc_min_laps": 3, "sc_max_laps": 5, "sc_pit_factor": 0.5,
          "sc_lap_factor": 1.4, "lap_noise": 0.25, "runs": 2000, "seed": 1, "react_window": 5}
}'
```

It returns `plans`, the best plan per stop count, fastest first, and `simulation`, with mean, std, p10/p50/p90 and win share per plan. Invalid races return 422 with a reason. `GET /healthz` is a health check.

### Docker

```bash
docker build -t pitwall . && docker run -p 8080:8080 pitwall
```

## Limits

- The numbers in `data/race.json` are made up to look like a real race. They aren't official data. To use real data, export long-run laps (for example with FastF1) to `compound,tyre_age,lap,lap_time` CSV and run `fit` on it. Drop in-laps, out-laps and safety-car laps first.
- It plans for one car with no traffic, so there's no undercut or overcut modelling and no track-position cost.
- The reactive policy only moves stops earlier. It doesn't re-plan the rest of the race.

## License

MIT
