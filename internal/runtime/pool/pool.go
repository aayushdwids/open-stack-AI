// Package pool manages a warm pool of sandboxes: acquire → assign → reset → async
// replenish, keeping a configured number pre-baked to avoid cold-start latency.
package pool

import (
	"context"
	"sync"

	"github.com/faraday-stack/faraday/internal/runtime/sandbox"
)

// Pool is a warm pool of ready sandboxes.
type Pool struct {
	driver sandbox.Driver
	size   int
	ready  chan sandbox.Sandbox
	mu     sync.Mutex
	closed bool
	all    map[string]sandbox.Sandbox
}

// New creates a pool and pre-warms it to size.
func New(ctx context.Context, driver sandbox.Driver, size int) (*Pool, error) {
	if size < 1 {
		size = 1
	}
	p := &Pool{
		driver: driver,
		size:   size,
		ready:  make(chan sandbox.Sandbox, size),
		all:    map[string]sandbox.Sandbox{},
	}
	for i := 0; i < size; i++ {
		sb, err := driver.New(ctx)
		if err != nil {
			p.Close()
			return nil, err
		}
		p.track(sb)
		p.ready <- sb
	}
	return p, nil
}

// Driver returns the underlying driver (for capability queries).
func (p *Pool) Driver() sandbox.Driver { return p.driver }

func (p *Pool) track(sb sandbox.Sandbox) {
	p.mu.Lock()
	p.all[sb.ID()] = sb
	p.mu.Unlock()
}

// Acquire returns a ready sandbox, creating one on demand if the pool is momentarily empty.
func (p *Pool) Acquire(ctx context.Context) (sandbox.Sandbox, error) {
	select {
	case sb := <-p.ready:
		return sb, nil
	default:
	}
	// Pool empty: create on demand rather than blocking forever.
	sb, err := p.driver.New(ctx)
	if err != nil {
		return nil, err
	}
	p.track(sb)
	return sb, nil
}

// Release resets a sandbox and returns it to the pool, replenishing asynchronously. If
// the pool is full it destroys the surplus.
func (p *Pool) Release(sb sandbox.Sandbox) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		_ = sb.Destroy()
		return
	}
	if err := sb.Reset(); err != nil {
		// Reset failed: destroy and asynchronously create a replacement.
		_ = sb.Destroy()
		p.mu.Lock()
		delete(p.all, sb.ID())
		p.mu.Unlock()
		go p.replenish()
		return
	}
	select {
	case p.ready <- sb:
	default:
		_ = sb.Destroy()
		p.mu.Lock()
		delete(p.all, sb.ID())
		p.mu.Unlock()
	}
}

func (p *Pool) replenish() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	sb, err := p.driver.New(context.Background())
	if err != nil {
		return
	}
	p.track(sb)
	select {
	case p.ready <- sb:
	default:
		_ = sb.Destroy()
		p.mu.Lock()
		delete(p.all, sb.ID())
		p.mu.Unlock()
	}
}

// Available reports how many sandboxes are currently ready.
func (p *Pool) Available() int { return len(p.ready) }

// Close destroys all sandboxes.
func (p *Pool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	all := p.all
	p.all = map[string]sandbox.Sandbox{}
	p.mu.Unlock()

	close(p.ready)
	for range p.ready { //nolint:revive // drain
	}
	for _, sb := range all {
		_ = sb.Destroy()
	}
}
