package loadtest

type workerState string

// Remote worker possible states
const (
	workerConnected workerState = "connected"
	workerAccepted  workerState = "accepted"
	workerRejected  workerState = "rejected"
	workerTesting   workerState = "testing"
	workerFailed    workerState = "failed"
	workerCompleted workerState = "completed"
)
