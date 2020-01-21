package loadtest

import (
	"encoding/csv"
	"fmt"
	"os"
)

func writeAggregateStats(filename string, totalTxs int, totalTimeSeconds float64) error {
	avgTxRate := float64(0)
	if totalTimeSeconds > 0.0 {
		avgTxRate = float64(totalTxs) / totalTimeSeconds
	}
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	records := [][]string{
		{"Parameter", "Value", "Units"},
		{"total_time", fmt.Sprintf("%.3f", totalTimeSeconds), "seconds"},
		{"total_txs", fmt.Sprintf("%d", totalTxs), "count"},
		{"avg_tx_rate", fmt.Sprintf("%.6f", avgTxRate), "transactions per second"},
	}
	return w.WriteAll(records)
}
