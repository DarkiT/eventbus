package eventbus

import (
	"sync"
	"sync/atomic"
)

// _CowMap 实现了一个线程安全的映射，使用的是写时复制（Copy-on-Write）模式
type _CowMap struct {
	mu    sync.RWMutex
	items atomic.Value
}

// newCowMap 创建一个新的 _CowMap 实例
func newCowMap() *_CowMap {
	cm := &_CowMap{}
	cm.items.Store(make(map[interface{}]interface{}))
	return cm
}

// Store 向 map 中添加或更新一个项
func (c *_CowMap) Store(key, value interface{}) {
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

// Load 从 map 中获取一个项
func (c *_CowMap) Load(key interface{}) (value interface{}, ok bool) {
	currentMap := c.items.Load().(map[interface{}]interface{})
	value, ok = currentMap[key]
	return
}

// Delete 从 map 中删除一个项
func (c *_CowMap) Delete(key interface{}) {
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

// Range 遍历 map 中的项
func (c *_CowMap) Range(f func(key, value interface{}) bool) {
	currentMap := c.items.Load().(map[interface{}]interface{})
	for k, v := range currentMap {
		if !f(k, v) {
			break
		}
	}
}

// Len 返回 map 中项的数量
func (c *_CowMap) Len() uint32 {
	currentMap := c.items.Load().(map[interface{}]interface{})
	return uint32(len(currentMap))
}

// Clear 移除 map 中的所有项
func (c *_CowMap) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items.Store(make(map[interface{}]interface{}))
}
