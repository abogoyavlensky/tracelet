package query

import "context"

// Gate bounds query concurrency and lets maintenance exclude queries. A query
// holds one slot for its snapshot's lifetime; retention, which unlinks
// Parquet files a query may be reading, holds every slot. Unlike a
// sync.RWMutex, every acquisition gives up when its context ends.
type Gate struct {
	slots chan struct{}
	// exclusive admits one Exclusive caller at a time, so two of them cannot
	// each hold part of the slots and wait on each other.
	exclusive chan struct{}
}

// NewGate returns a gate with the given number of query slots.
func NewGate(slots int) *Gate {
	return &Gate{
		slots:     make(chan struct{}, slots),
		exclusive: make(chan struct{}, 1),
	}
}

// Acquire takes one slot, or returns ctx's error if it ends first. The
// returned func releases the slot.
func (g *Gate) Acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Exclusive takes every slot, waiting for open snapshots to close, or
// returns ctx's error if it ends first, releasing whatever it took. The
// returned func releases all slots.
func (g *Gate) Exclusive(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case g.exclusive <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	taken := 0
	release := func() {
		for range taken {
			<-g.slots
		}
		<-g.exclusive
	}
	for taken < cap(g.slots) {
		select {
		case g.slots <- struct{}{}:
			taken++
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
	return release, nil
}
