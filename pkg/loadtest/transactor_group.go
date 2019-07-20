package loadtest

import (
	"sync"
)

// TransactorGroup allows us to encapsulate the management of a group of
// transactors.
type TransactorGroup struct {
	transactors []*Transactor
}

func NewTransactorGroup() *TransactorGroup {
	return &TransactorGroup{
		transactors: make([]*Transactor, 0),
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

// Start will handle through all transactors and start them.
func (g *TransactorGroup) Start() {
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

func (g *TransactorGroup) close() {
	for _, t := range g.transactors {
		t.close()
	}
}
