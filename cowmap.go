package eventbus

import (
	"sync"
	"sync/atomic"
)

// CowMap implements a thread-safe map using the Copy-on-Write pattern
type CowMap struct {
	mu    sync.RWMutex
	items atomic.Value // stores map[interface{}]interface{}
}

// NewCowMap creates a new CowMap instance
func NewCowMap() *CowMap {
	cm := &CowMap{}
	cm.items.Store(make(map[interface{}]interface{}))
	return cm
}

// Store adds or updates an item in the map
func (c *CowMap) Store(key, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldMap := c.items.Load().(map[interface{}]interface{})
	newMap := make(map[interface{}]interface{}, len(oldMap)+1)

	for k, v := range oldMap {
		newMap[k] = v
	}
	newMap[key] = value
	c.items.Store(newMap)
}

// Load retrieves an item from the map
func (c *CowMap) Load(key interface{}) (value interface{}, ok bool) {
	currentMap := c.items.Load().(map[interface{}]interface{})
	value, ok = currentMap[key]
	return
}

// Delete removes an item from the map
func (c *CowMap) Delete(key interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldMap := c.items.Load().(map[interface{}]interface{})
	if _, exists := oldMap[key]; !exists {
		return
	}

	newMap := make(map[interface{}]interface{}, len(oldMap)-1)
	for k, v := range oldMap {
		if k != key {
			newMap[k] = v
		}
	}
	c.items.Store(newMap)
}

// Range iterates over the items in the map
func (c *CowMap) Range(f func(key, value interface{}) bool) {
	currentMap := c.items.Load().(map[interface{}]interface{})
	for k, v := range currentMap {
		if !f(k, v) {
			break
		}
	}
}

// Len returns the number of items in the map
func (c *CowMap) Len() uint32 {
	currentMap := c.items.Load().(map[interface{}]interface{})
	return uint32(len(currentMap))
}

// Clear removes all items from the map
func (c *CowMap) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items.Store(make(map[interface{}]interface{}))
}
