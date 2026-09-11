package server

import (
	"sync"
	"time"

	"github.com/MilanBehnam/tokensaver/internal/convert"
	"github.com/MilanBehnam/tokensaver/internal/source"
)

// The cache lives only as long as the server process (one agent session). It
// makes follow-up calls — page 2, a section after the outline — fast and
// consistent without re-fetching or re-parsing. Nothing is written to disk.
const (
	cacheTTL      = 10 * time.Minute
	cacheMaxBytes = 128 << 20
)

type cacheEntry struct {
	kind source.Kind
	doc  *convert.Doc   // converted document (non-JSON)
	src  *source.Source // raw content (JSON, which is shrunk per call)
	at   time.Time
}

func (e *cacheEntry) size() int {
	n := 0
	if e.doc != nil {
		n += len(e.doc.Markdown)
	}
	if e.src != nil {
		n += len(e.src.Data)
	}
	return n
}

type cache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	order   []string // oldest first
	bytes   int
}

func newCache() *cache { return &cache{entries: map[string]*cacheEntry{}} }

func (c *cache) get(key string) *cacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Since(e.at) > cacheTTL {
		return nil
	}
	return e
}

func (c *cache) put(key string, e *cacheEntry) {
	e.at = time.Now()
	if e.size() > cacheMaxBytes/2 {
		return // too big to be worth holding
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(key)
	c.entries[key] = e
	c.order = append(c.order, key)
	c.bytes += e.size()
	for c.bytes > cacheMaxBytes && len(c.order) > 0 {
		c.remove(c.order[0])
	}
}

func (c *cache) remove(key string) {
	e, ok := c.entries[key]
	if !ok {
		return
	}
	delete(c.entries, key)
	c.bytes -= e.size()
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}
