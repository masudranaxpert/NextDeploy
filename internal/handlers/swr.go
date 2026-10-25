package handlers

import (
	"sync"
	"time"
)

// swrCache keeps the last result of a slow lookup. A value that is merely old is returned at once
// while one background refresh runs, so a page never waits for Docker because 3 seconds passed.
// invalidate() is stronger: the next call waits for a fresh result, so a page opened right after
// the user changed something never shows the old state.
type swrCache[T any] struct {
	mu         sync.Mutex
	val        T
	at         time.Time
	ok         bool
	refreshing bool
}

func (c *swrCache[T]) get(ttl time.Duration, load func() T) T {
	c.mu.Lock()
	if c.ok {
		v, fresh := c.val, time.Since(c.at) < ttl
		if !fresh && !c.refreshing {
			c.refreshing = true
			go func() {
				nv := load()
				c.mu.Lock()
				c.val, c.at, c.ok, c.refreshing = nv, time.Now(), true, false
				c.mu.Unlock()
			}()
		}
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	nv := load()
	c.mu.Lock()
	c.val, c.at, c.ok = nv, time.Now(), true
	c.mu.Unlock()
	return nv
}

func (c *swrCache[T]) invalidate() {
	c.mu.Lock()
	c.ok = false
	c.mu.Unlock()
}
