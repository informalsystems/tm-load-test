package loadtest

import (
	"encoding/csv"
	"fmt"
	"os"
)

type AggregateStats struct {
	TotalTxs         int     // The total number of transactions sent.
	TotalTimeSeconds float64 // The total time taken to send `TotalTxs` transactions.
	TotalBytes       int64   // The cumulative number of bytes sent as transactions.

	// Computed statistics
	AvgTxRate   float64 // The rate at which transactions were submitted (tx/sec).
	AvgDataRate float64 // The rate at which data was transmitted in transactions (bytes/sec).
}

func (s *AggregateStats) String() string {
	return fmt.Sprintf(
		"AggregateStats{TotalTimeSeconds: %.3f, TotalTxs: %d, TotalBytes: %d, AvgTxRate: %.6f, AvgDataRate: %.6f}",
		s.TotalTimeSeconds,
		s.TotalTxs,
		s.TotalBytes,
		s.AvgTxRate,
		s.AvgDataRate,
	)
}

func (s *AggregateStats) Compute() {
	s.AvgTxRate = 0
	s.AvgDataRate = 0
	if s.TotalTimeSeconds > 0.0 {
		s.AvgTxRate = float64(s.TotalTxs) / s.TotalTimeSeconds
		s.AvgDataRate = float64(s.TotalBytes) / s.TotalTimeSeconds
	}
}

func writeAggregateStats(filename string, stats AggregateStats) error {
	stats.Compute()
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	records := [][]string{
		{"Parameter", "Value", "Units"},
		{"total_time", fmt.Sprintf("%.3f", stats.TotalTimeSeconds), "seconds"},
		{"total_txs", fmt.Sprintf("%d", stats.TotalTxs), "count"},
		{"total_bytes", fmt.Sprintf("%d", stats.TotalBytes), "bytes"},
		{"avg_tx_rate", fmt.Sprintf("%.6f", stats.AvgTxRate), "transactions per second"},
		{"avg_data_rate", fmt.Sprintf("%.6f", stats.AvgDataRate), "bytes per second"},
	}
	return w.WriteAll(records)
}
