package loadtest

// A generic message to/from a slave.
type slaveMsg struct {
	ID           string     `json:"id,omitempty"`             // A UUID for this slave.
	State        slaveState `json:"state,omitempty"`          // The slave's desired or actual state.
	TxCount      int        `json:"tx_count,omitempty"`       // The total number of transactions sent thus far by this slave.
	TotalTxBytes int64      `json:"total_tx_bytes,omitempty"` // The total number of transaction bytes sent thus far by this slave.
	Error        string     `json:"error,omitempty"`          // If the slave has failed somehow, a descriptive error message as to why.
	Config       *Config    `json:"config,omitempty"`         // The load testing configuration, if relevant.
}
