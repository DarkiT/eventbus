package adapter

import (
	"sync"
	"time"
)

type seenCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	items map[string]time.Time
}

func newSeenCache(ttl time.Duration, max int) *seenCache {
	if ttl <= 0 || max <= 0 {
		return nil
	}
	return &seenCache{ttl: ttl, max: max, items: make(map[string]time.Time)}
}

func (c *seenCache) seen(id string) bool {
	if c == nil || id == "" {
		return false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if deadline, ok := c.items[id]; ok {
		if now.Before(deadline) {
			return true
		}
		delete(c.items, id)
	}
	c.items[id] = now.Add(c.ttl)
	if len(c.items) > c.max {
		c.purgeLocked(now)
	}
	return false
}

// purgeLocked 清理过期项；当条目数仍超过上限时，按 map 遍历顺序随机淘汰，
// 属于 best-effort 去重（与 adapter 包"无 exactly-once 保证"的语义一致）。
// 调用方应保证 dedupMaxEntries 足够覆盖一个去重窗口内的消息量，避免有效 ID 被误淘汰。
func (c *seenCache) purgeLocked(now time.Time) {
	for id, deadline := range c.items {
		if !now.Before(deadline) || len(c.items) > c.max {
			delete(c.items, id)
		}
		if len(c.items) <= c.max {
			return
		}
	}
}
