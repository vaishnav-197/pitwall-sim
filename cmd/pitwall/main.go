// Command pitwall fits tyre models, plans pit strategies and simulates them.
//
//	pitwall gen      -out data/sample_laps.csv        # synthetic practice laps
//	pitwall fit      -laps data/sample_laps.csv -race data/race.json -out tyres.json
//	pitwall plan     -model tyres.json [-sim]
//	pitwall serve    -addr :8080
package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/vaishnav-197/pitwall-sim/internal/api"
	"github.com/vaishnav-197/pitwall-sim/internal/fit"
	"github.com/vaishnav-197/pitwall-sim/internal/sim"
	"github.com/vaishnav-197/pitwall-sim/internal/strategy"
	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "gen":
		err = cmdGen(os.Args[2:])
	case "fit":
		err = cmdFit(os.Args[2:])
	case "plan":
		err = cmdPlan(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pitwall <gen|fit|plan|serve> [flags]")
	os.Exit(2)
}

// cmdGen writes synthetic long-run practice laps from the "true" model in
// data/race.json plus noise, so `fit` has something to learn from and we can
// check it recovers the right numbers.
func cmdGen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	truthPath := fs.String("truth", "data/race.json", "model to generate laps from")
	out := fs.String("out", "data/sample_laps.csv", "output CSV")
	noise := fs.Float64("noise", 0.15, "lap-time noise std dev, seconds")
	seed := fs.Int64("seed", 7, "random seed")
	fs.Parse(args)

	m, err := tyre.Load(*truthPath)
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewSource(*seed))
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"compound", "tyre_age", "lap", "lap_time"})

	rows := 0
	for _, name := range m.Names() {
		c := m.Compounds[name]
		maxAge := 25
		if c.CliffAge > 0 && c.CliffAge < maxAge {
			maxAge = c.CliffAge // practice runs stop before the cliff
		}
		// Several long runs per compound, starting at different fuel loads.
		for _, startLap := range []int{1, 10, 20, 30} {
			for age := 0; age < maxAge && startLap+age <= m.Race.Laps; age++ {
				lap := startLap + age
				t := m.LapTime(c, age, lap) + rng.NormFloat64()**noise
				w.Write([]string{name, strconv.Itoa(age), strconv.Itoa(lap), strconv.FormatFloat(t, 'f', 3, 64)})
				rows++
			}
		}
	}
	w.Flush()
	fmt.Printf("wrote %d laps to %s\n", rows, *out)
	return w.Error()
}

func cmdFit(args []string) error {
	fs := flag.NewFlagSet("fit", flag.ExitOnError)
	lapsPath := fs.String("laps", "data/sample_laps.csv", "lap CSV")
	racePath := fs.String("race", "data/race.json", "race settings (laps, pit loss, rules) and cliff info")
	out := fs.String("out", "tyres.json", "fitted model output")
	fs.Parse(args)

	base, err := tyre.Load(*racePath)
	if err != nil {
		return err
	}
	laps, err := fit.LoadCSV(*lapsPath)
	if err != nil {
		return err
	}
	res, err := fit.Fit(laps, base.Race.Laps)
	if err != nil {
		return err
	}

	fmt.Printf("fitted %d laps: RMSE=%.3fs R²=%.4f fuel=%.4fs/lap\n", res.N, res.RMSE, res.R2, res.FuelPerLap)
	fmt.Printf("%-8s %9s %9s %9s\n", "compound", "base", "lin", "quad")

	m := &tyre.Model{Race: base.Race, Compounds: map[string]tyre.Compound{}}
	m.Race.FuelPerLap = res.FuelPerLap
	for _, n := range sortedKeys(res.Compounds) {
		c := res.Compounds[n]
		// Cliffs can't be learned from practice runs that stop before them,
		// so keep the ones from the race file.
		if b, ok := base.Compounds[n]; ok {
			c.CliffAge, c.CliffRate = b.CliffAge, b.CliffRate
		}
		m.Compounds[n] = c
		fmt.Printf("%-8s %9.3f %9.4f %9.5f\n", n, c.Base, c.Lin, c.Quad)
	}
	if err := m.Save(*out); err != nil {
		return err
	}
	fmt.Println("wrote", *out)
	return nil
}

func sortedKeys(m map[string]tyre.Compound) []string {
	tm := tyre.Model{Compounds: m}
	return tm.Names()
}

func cmdPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	modelPath := fs.String("model", "data/race.json", "model JSON (race + compounds)")
	doSim := fs.Bool("sim", true, "also run the safety-car simulation")
	react := fs.Int("react", 5, "bring a stop forward under safety car if due within N laps (0 = never)")
	runs := fs.Int("runs", 5000, "simulation runs")
	scProb := fs.Float64("sc", 0.02, "safety car chance per lap")
	fs.Parse(args)

	m, err := tyre.Load(*modelPath)
	if err != nil {
		return err
	}
	start := time.Now()
	plans, err := strategy.Optimize(m)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return errors.New("no legal plan: raise max_stops or lower min_compounds")
	}
	fmt.Printf("best plan per stop count (%d laps, pit loss %.1fs, solved in %s):\n",
		m.Race.Laps, m.Race.PitLoss, time.Since(start).Round(time.Millisecond))
	for _, p := range plans {
		fmt.Printf("  %d-stop  %-40s pit laps %-12s %s  (+%.1fs)\n",
			p.Stops, p.String(), fmt.Sprint(p.PitLaps), clock(p.Time), p.Time-plans[0].Time)
	}
	if !*doSim {
		return nil
	}

	cfg := sim.DefaultConfig()
	cfg.Runs, cfg.SCProbPerLap = *runs, *scProb
	for _, rw := range []int{0, *react} {
		cfg.ReactWindow = rw
		stats, err := sim.Run(m, plans, cfg)
		if err != nil {
			return err
		}
		policy := "stick to the plan"
		if rw > 0 {
			policy = fmt.Sprintf("pit early under safety car if a stop is due within %d laps", rw)
		}
		fmt.Printf("\nsafety cars %.0f%%/lap, %d runs, %s:\n", cfg.SCProbPerLap*100, cfg.Runs, policy)
		fmt.Printf("  %-6s %-40s %12s %12s %12s %7s\n", "stops", "plan", "mean", "p10", "p90", "wins")
		for _, s := range stats {
			fmt.Printf("  %-6d %-40s %12s %12s %12s %6.1f%%\n",
				s.Plan.Stops, s.Plan.String(), clock(s.Mean), clock(s.P10), clock(s.P90), s.WinShare*100)
		}
		if *react == 0 {
			break
		}
	}
	return nil
}

func clock(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := d.Seconds() - float64(h*3600+m*60)
	return fmt.Sprintf("%d:%02d:%06.3f", h, m, s)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	fs.Parse(args)

	srv := &http.Server{Addr: *addr, Handler: api.New(), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	log.Printf("serving on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
