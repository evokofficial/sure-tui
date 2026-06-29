package main

import "math"

// dayStats sums income (positive) and expense (positive) per date.
func dayStats(txns []Txn) (inc, exp map[string]float64) {
	inc, exp = map[string]float64{}, map[string]float64{}
	for _, t := range txns {
		if t.IsTransfer() {
			continue
		}
		a := math.Abs(t.Amountf())
		if t.Classification == "income" {
			inc[t.Date] += a
		} else {
			exp[t.Date] += a
		}
	}
	return
}
