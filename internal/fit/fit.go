// Package fit learns tyre and fuel parameters from lap times with ordinary
// least squares, solved by hand (normal equations + Gaussian elimination).
package fit

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

// Lap is one observed lap.
type Lap struct {
	Compound string
	TyreAge  int // laps already done on this set before this lap
	Lap      int // race lap number, 1-based
	Time     float64
}

// Result is a fitted model plus how well it fits.
type Result struct {
	Compounds  map[string]tyre.Compound
	FuelPerLap float64
	RMSE       float64 // seconds
	R2         float64
	N          int
}

// LoadCSV reads rows of compound,tyre_age,lap,lap_time with a header line.
func LoadCSV(path string) ([]Lap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadCSV(f)
}

// ReadCSV parses lap rows from r.
func ReadCSV(r io.Reader) ([]Lap, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = 4
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	want := []string{"compound", "tyre_age", "lap", "lap_time"}
	for i, h := range header {
		if strings.TrimSpace(strings.ToLower(h)) != want[i] {
			return nil, fmt.Errorf("header must be %s", strings.Join(want, ","))
		}
	}
	var laps []Lap
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		age, err1 := strconv.Atoi(strings.TrimSpace(rec[1]))
		lap, err2 := strconv.Atoi(strings.TrimSpace(rec[2]))
		t, err3 := strconv.ParseFloat(strings.TrimSpace(rec[3]), 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("line %d: bad number", line)
		}
		laps = append(laps, Lap{Compound: strings.TrimSpace(rec[0]), TyreAge: age, Lap: lap, Time: t})
	}
	return laps, nil
}

// Fit estimates, for every compound, a base time and linear + quadratic
// wear, plus one fuel effect shared by all compounds:
//
//	time = base[c] + lin[c]*age + quad[c]*age² + fuel*(raceLaps - lap)
//
// The shared fuel term is why this is one joint regression instead of one
// per compound. Laps on a worn-out set (past the cliff) should be removed
// first; outliers like in/out laps and safety-car laps too.
func Fit(laps []Lap, raceLaps int) (*Result, error) {
	if raceLaps < 2 {
		return nil, errors.New("fit: raceLaps must be >= 2")
	}
	idx := map[string]int{}
	for _, l := range laps {
		if _, ok := idx[l.Compound]; !ok {
			idx[l.Compound] = 0
		}
	}
	names := make([]string, 0, len(idx))
	for n := range idx {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		idx[n] = i
	}

	k := len(names)
	p := 3*k + 1 // per compound: base, lin, quad; then shared fuel
	if len(laps) < p+1 {
		return nil, fmt.Errorf("fit: need more than %d laps, got %d", p, len(laps))
	}

	features := func(l Lap) []float64 {
		x := make([]float64, p)
		c := idx[l.Compound]
		a := float64(l.TyreAge)
		x[3*c] = 1
		x[3*c+1] = a
		x[3*c+2] = a * a
		x[p-1] = float64(raceLaps - l.Lap)
		return x
	}

	// Build XᵀX and Xᵀy.
	xtx := make([][]float64, p)
	for i := range xtx {
		xtx[i] = make([]float64, p)
	}
	xty := make([]float64, p)
	for _, l := range laps {
		x := features(l)
		for i := 0; i < p; i++ {
			if x[i] == 0 {
				continue
			}
			xty[i] += x[i] * l.Time
			for j := 0; j < p; j++ {
				xtx[i][j] += x[i] * x[j]
			}
		}
	}

	beta, err := solve(xtx, xty)
	if err != nil {
		return nil, fmt.Errorf("fit: %w (each compound needs laps at 3+ different tyre ages)", err)
	}

	res := &Result{Compounds: map[string]tyre.Compound{}, FuelPerLap: beta[p-1], N: len(laps)}
	for n, c := range idx {
		res.Compounds[n] = tyre.Compound{Base: beta[3*c], Lin: beta[3*c+1], Quad: beta[3*c+2]}
	}

	// Goodness of fit.
	mean := 0.0
	for _, l := range laps {
		mean += l.Time
	}
	mean /= float64(len(laps))
	var ssRes, ssTot float64
	for _, l := range laps {
		x := features(l)
		pred := 0.0
		for i := range x {
			pred += x[i] * beta[i]
		}
		ssRes += (l.Time - pred) * (l.Time - pred)
		ssTot += (l.Time - mean) * (l.Time - mean)
	}
	res.RMSE = math.Sqrt(ssRes / float64(len(laps)))
	if ssTot > 0 {
		res.R2 = 1 - ssRes/ssTot
	}
	return res, nil
}

// solve does Gaussian elimination with partial pivoting on a copy of a.
func solve(a [][]float64, b []float64) ([]float64, error) {
	n := len(b)
	m := make([][]float64, n)
	for i := range a {
		m[i] = append(append([]float64(nil), a[i]...), b[i])
	}
	for col := 0; col < n; col++ {
		pivot := col
		for r := col + 1; r < n; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(m[pivot][col]) < 1e-9 {
			return nil, errors.New("singular system")
		}
		m[col], m[pivot] = m[pivot], m[col]
		for r := col + 1; r < n; r++ {
			f := m[r][col] / m[col][col]
			for c := col; c <= n; c++ {
				m[r][c] -= f * m[col][c]
			}
		}
	}
	x := make([]float64, n)
	for r := n - 1; r >= 0; r-- {
		s := m[r][n]
		for c := r + 1; c < n; c++ {
			s -= m[r][c] * x[c]
		}
		x[r] = s / m[r][r]
	}
	return x, nil
}
