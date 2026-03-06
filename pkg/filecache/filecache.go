package filecache

import (
	"context"
	"os"
	"sync"
	"time"
)

type entry struct {
	data       []byte
	modTime    time.Time
	size       int64
	lastAccess time.Time
}

type Cache struct {
	mu              sync.RWMutex
	entries         map[string]*entry
	maxEntries      int
	maxFileSize     int64
	ttl             time.Duration
	refreshInterval time.Duration
	stop            chan struct{}
}

type Option func(*Cache)

func WithMaxEntries(n int) Option {
	return func(c *Cache) { c.maxEntries = n }
}

func WithMaxFileSize(n int64) Option {
	return func(c *Cache) { c.maxFileSize = n }
}

func WithTTL(d time.Duration) Option {
	return func(c *Cache) { c.ttl = d }
}

func WithRefreshInterval(d time.Duration) Option {
	return func(c *Cache) { c.refreshInterval = d }
}

func New(opts ...Option) *Cache {
	c := &Cache{
		entries:         make(map[string]*entry),
		maxEntries:      256,
		maxFileSize:     2 * 1024 * 1024, // 2 MB
		ttl:             30 * time.Minute,
		refreshInterval: 5 * time.Minute,
		stop:            make(chan struct{}),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Start launches the background refresh goroutine. Call Stop to terminate it.
func (c *Cache) Start(ctx context.Context) {
	go c.refreshLoop(ctx)
}

func (c *Cache) Stop() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
}

// Get returns the file contents, serving from cache when possible.
// On cache miss, reads from disk and caches the result.
// Each call renews the entry's TTL.
func (c *Cache) Get(path string) ([]byte, error) {
	c.mu.Lock()
	e, ok := c.entries[path]
	if ok {
		e.lastAccess = time.Now()
	}
	c.mu.Unlock()

	if ok {
		cp := make([]byte, len(e.data))
		copy(cp, e.data)
		return cp, nil
	}

	return c.load(path)
}

// Invalidate removes a path from the cache. Call after writes.
func (c *Cache) Invalidate(path string) {
	c.mu.Lock()
	delete(c.entries, path)
	c.mu.Unlock()
}

func (c *Cache) load(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Don't cache files that are too large.
	if info.Size() > c.maxFileSize {
		return os.ReadFile(path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	c.mu.Lock()
	if len(c.entries) >= c.maxEntries {
		c.evictOne()
	}
	c.entries[path] = &entry{
		data:       data,
		modTime:    info.ModTime(),
		size:       info.Size(),
		lastAccess: now,
	}
	c.mu.Unlock()

	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

// evictOne removes the least-recently-accessed entry. Must be called with mu held.
func (c *Cache) evictOne() {
	var oldest string
	var oldestAccess time.Time
	for path, e := range c.entries {
		if oldest == "" || e.lastAccess.Before(oldestAccess) {
			oldest = path
			oldestAccess = e.lastAccess
		}
	}
	if oldest != "" {
		delete(c.entries, oldest)
	}
}

func (c *Cache) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		case <-ticker.C:
			c.refreshAll()
		}
	}
}

func (c *Cache) refreshAll() {
	now := time.Now()

	c.mu.Lock()
	// Evict expired entries first.
	for path, e := range c.entries {
		if now.Sub(e.lastAccess) > c.ttl {
			delete(c.entries, path)
		}
	}
	c.mu.Unlock()

	// Snapshot surviving entries for refresh.
	c.mu.RLock()
	type snapshot struct {
		path    string
		modTime time.Time
	}
	live := make([]snapshot, 0, len(c.entries))
	for p, e := range c.entries {
		live = append(live, snapshot{path: p, modTime: e.modTime})
	}
	c.mu.RUnlock()

	for _, s := range live {
		info, err := os.Stat(s.path)
		if err != nil {
			c.Invalidate(s.path)
			continue
		}

		if info.ModTime().Equal(s.modTime) {
			continue
		}

		if info.Size() > c.maxFileSize {
			c.Invalidate(s.path)
			continue
		}

		data, err := os.ReadFile(s.path)
		if err != nil {
			c.Invalidate(s.path)
			continue
		}

		c.mu.Lock()
		if e, ok := c.entries[s.path]; ok {
			e.data = data
			e.modTime = info.ModTime()
			e.size = info.Size()
			// Don't touch lastAccess — only reads renew TTL.
		}
		c.mu.Unlock()
	}
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
