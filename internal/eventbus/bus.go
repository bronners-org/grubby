package eventbus

import (
	"sync"

	"grubby/internal/protocol"
)

type FilterFunc func(protocol.EventEnvelope) bool

type Publisher interface {
	Publish(protocol.EventEnvelope)
}

type Subscription struct {
	C      chan protocol.EventEnvelope
	filter FilterFunc
	bus    *Bus
	id     uint64
	closed bool
	mu     sync.Mutex
}

func (s *Subscription) Unsubscribe() {
	s.bus.unsubscribe(s.id)
}

type Bus struct {
	mu      sync.RWMutex
	nextID  uint64
	subs    map[uint64]*Subscription
	bufSize int
	closed  bool
}

func NewBus(bufferSize int) *Bus {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &Bus{
		subs:    make(map[uint64]*Subscription),
		bufSize: bufferSize,
	}
}

func (b *Bus) Subscribe(filter FilterFunc) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		sub := &Subscription{
			C:      make(chan protocol.EventEnvelope),
			closed: true,
			bus:    b,
		}
		close(sub.C)
		return sub
	}

	b.nextID++
	sub := &Subscription{
		C:      make(chan protocol.EventEnvelope, b.bufSize),
		filter: filter,
		bus:    b,
		id:     b.nextID,
	}
	b.subs[b.nextID] = sub
	return sub
}

func (b *Bus) Publish(evt protocol.EventEnvelope) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, sub := range b.subs {
		if sub.filter == nil || sub.filter(evt) {
			select {
			case sub.C <- evt:
			default:
				// Channel buffer full: skip delivery to prevent blocking the event publisher
			}
		}
	}
}

func (b *Bus) unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sub, ok := b.subs[id]; ok {
		sub.mu.Lock()
		if !sub.closed {
			sub.closed = true
			close(sub.C)
		}
		sub.mu.Unlock()
		delete(b.subs, id)
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, sub := range b.subs {
		sub.mu.Lock()
		if !sub.closed {
			sub.closed = true
			close(sub.C)
		}
		sub.mu.Unlock()
	}
	b.subs = make(map[uint64]*Subscription)
}
