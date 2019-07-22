package loadtest

import (
	"sync"
	"time"
)

// TransactorGroup allows us to encapsulate the management of a group of
// transactors.
type TransactorGroup struct {
	transactors []*Transactor

	statsMtx sync.RWMutex
	txCounts map[int]int // The counts of all of the total transactions per transactor.

	progressCallbackMtx      sync.RWMutex
	progressCallbackInterval time.Duration
	progressCallback         func(g *TransactorGroup, txCount int)

	stopProgressReporter    chan struct{} // Close this to stop the progress reporter.
	progressReporterStopped chan struct{} // Closed when the progress reporter goroutine has completely stopped.
}

func NewTransactorGroup() *TransactorGroup {
	return &TransactorGroup{
		transactors:              make([]*Transactor, 0),
		txCounts:                 make(map[int]int),
		progressCallbackInterval: defaultProgressCallbackInterval,
		stopProgressReporter:     make(chan struct{}, 1),
		progressReporterStopped:  make(chan struct{}, 1),
	}
}

// Add will instantiate a new Transactor with the given parameters. If
// instantiation fails it'll automatically shut down and close all other
// transactors, returning the error.
func (g *TransactorGroup) Add(remoteAddr string, config *Config) error {
	t, err := NewTransactor(remoteAddr, config)
	if err != nil {
		g.close()
		return err
	}
	id := len(g.transactors)
	t.SetProgressCallback(id, g.getProgressCallbackInterval()/2, g.trackTransactorProgress)
	g.transactors = append(g.transactors, t)
	return nil
}

func (g *TransactorGroup) AddAll(cfg *Config) error {
	for _, endpoint := range cfg.Endpoints {
		for c := 0; c < cfg.Connections; c++ {
			if err := g.Add(endpoint, cfg); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *TransactorGroup) SetProgressCallback(interval time.Duration, callback func(*TransactorGroup, int)) {
	g.progressCallbackMtx.Lock()
	g.progressCallbackInterval = interval
	g.progressCallback = callback
	g.progressCallbackMtx.Unlock()
}

// Start will handle through all transactors and start them.
func (g *TransactorGroup) Start() {
	go g.progressReporter()
	for _, t := range g.transactors {
		t.Start()
	}
}

// Cancel signals to all transactors to stop their operations.
func (g *TransactorGroup) Cancel() {
	for _, t := range g.transactors {
		t.Cancel()
	}
}

// Wait will wait for all transactors to complete, returning the first error
// we encounter.
func (g *TransactorGroup) Wait() error {
	defer func() {
		close(g.stopProgressReporter)
		<-g.progressReporterStopped
	}()

	var wg sync.WaitGroup
	var err error
	errc := make(chan error, len(g.transactors))
	for _, t := range g.transactors {
		wg.Add(1)
		go func(_t *Transactor) {
			errc <- _t.Wait()
			defer wg.Done()
		}(t)
	}
	wg.Wait()
	// collect the results
	for i := 0; i < len(g.transactors); i++ {
		if e := <-errc; e != nil {
			err = e
			break
		}
	}
	return err
}

func (g *TransactorGroup) progressReporter() {
	defer close(g.progressReporterStopped)

	ticker := time.NewTicker(g.getProgressCallbackInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			g.reportProgress()

		case <-g.stopProgressReporter:
			return
		}
	}
}

func (g *TransactorGroup) trackTransactorProgress(id int, txCount int) {
	g.statsMtx.Lock()
	g.txCounts[id] = txCount
	g.statsMtx.Unlock()
}

func (g *TransactorGroup) getProgressCallbackInterval() time.Duration {
	g.progressCallbackMtx.RLock()
	defer g.progressCallbackMtx.RUnlock()
	return g.progressCallbackInterval
}

func (g *TransactorGroup) reportProgress() {
	totalTxs := g.totalTxs()

	g.progressCallbackMtx.RLock()
	if g.progressCallback != nil {
		g.progressCallback(g, totalTxs)
	}
	g.progressCallbackMtx.RUnlock()
}

func (g *TransactorGroup) totalTxs() int {
	g.statsMtx.RLock()
	defer g.statsMtx.RUnlock()
	total := 0
	for _, txCount := range g.txCounts {
		total += txCount
	}
	return total
}

func (g *TransactorGroup) close() {
	for _, t := range g.transactors {
		t.close()
	}
}
