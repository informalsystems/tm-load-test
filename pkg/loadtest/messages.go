package loadtest

// A generic message to/from a worker.
type workerMsg struct {
	ID           string      `json:"id,omitempty"`             // A UUID for this worker.
	State        workerState `json:"state,omitempty"`          // The worker's desired or actual state.
	TxCount      int         `json:"tx_count,omitempty"`       // The total number of transactions sent thus far by this worker.
	TotalTxBytes int64       `json:"total_tx_bytes,omitempty"` // The total number of transaction bytes sent thus far by this worker.
	Error        string      `json:"error,omitempty"`          // If the worker has failed somehow, a descriptive error message as to why.
	Config       *Config     `json:"config,omitempty"`         // The load testing configuration, if relevant.
}
