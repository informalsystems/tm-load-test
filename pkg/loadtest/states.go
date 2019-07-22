package loadtest

type slaveState string

// Remote slave possible states
const (
	slaveConnected slaveState = "connected"
	slaveAccepted  slaveState = "accepted"
	slaveRejected  slaveState = "rejected"
	slaveTesting   slaveState = "testing"
	slaveFailed    slaveState = "failed"
	slaveCompleted slaveState = "completed"
)
