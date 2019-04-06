package loadtest

import (
	"sync"

	"github.com/AsynkronIT/protoactor-go/actor"
)

// Probe allows us to hook into the actors facilitating the load testing to
// obtain insight into what's going on inside of them.
type Probe interface {
	OnStartup(ctx actor.Context)
	OnShutdown(ctx actor.Context, err error)
	OnStopped(ctx actor.Context)
}

type NoopProbe struct{}

func (p *NoopProbe) OnStartup(ctx actor.Context)             {}
func (p *NoopProbe) OnShutdown(ctx actor.Context, err error) {}
func (p *NoopProbe) OnStopped(ctx actor.Context)             {}

type StandardProbe struct {
	mtx *sync.Mutex
	wg  *sync.WaitGroup
	err error
}

func NewStandardProbe() *StandardProbe {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	return &StandardProbe{
		mtx: &sync.Mutex{},
		wg:  wg,
		err: nil,
	}
}

func (p *StandardProbe) OnStartup(ctx actor.Context) {}

func (p *StandardProbe) OnShutdown(ctx actor.Context, err error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.err = err
}

func (p *StandardProbe) OnStopped(ctx actor.Context) {
	p.wg.Done()
}

func (p *StandardProbe) Wait() error {
	// wait for the actor to stop
	p.wg.Wait()
	// return the error code, if any
	p.mtx.Lock()
	defer p.mtx.Unlock()
	return p.err
}
