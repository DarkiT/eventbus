package eventbus

import (
	"maps"
	"reflect"
	"sync"
	"sync/atomic"
)

// cowMap 实现了一个线程安全的映射，使用的是写时复制（Copy-on-Write）模式
// 优化版本：减少内存分配，提升写操作性能
type cowMap struct {
	mu      sync.RWMutex
	items   atomic.Value
	size    int32 // 使用原子操作的大小计数
	initial int   // 初始容量
}

// newCowMap 创建一个新的 cowMap 实例
func newCowMap() *cowMap {
	cm := &cowMap{
		initial: 16,
		size:    0,
	}
	cm.items.Store(make(map[interface{}]interface{}, cm.initial))
	return cm
}

// Store 向 map 中添加或更新一个项
// 优化版本：智能容量管理，减少不必要的复制
func (c *cowMap) Store(key, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldMap := c.items.Load().(map[interface{}]interface{})

	// 检查是否是更新操作
	oldValue, exists := oldMap[key]
	if exists {
		// 更新操作：检查值是否相同，避免不必要的复制
		if valuesEqual(oldValue, value) {
			return // 值相同，无需操作
		}
	}

	newMap := maps.Clone(oldMap)
	newMap[key] = value
	c.items.Store(newMap)
	if !exists {
		atomic.AddInt32(&c.size, 1)
	}
}

// valuesEqual 判断两个值是否相等，兼容不可比较类型
func valuesEqual(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == b
	}

	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	if ta != tb {
		return false
	}

	if ta.Comparable() {
		return reflect.ValueOf(a).Interface() == reflect.ValueOf(b).Interface()
	}

	return reflect.DeepEqual(a, b)
}

// Load 从 map 中获取一个项
func (c *cowMap) Load(key interface{}) (value interface{}, ok bool) {
	currentMap := c.items.Load().(map[interface{}]interface{})
	value, ok = currentMap[key]
	return value, ok
}

// Delete 从 map 中删除一个项
// 优化版本：智能容量管理
func (c *cowMap) Delete(key interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldMap := c.items.Load().(map[interface{}]interface{})
	if _, exists := oldMap[key]; !exists {
		return
	}

	// 更新大小计数
	atomic.AddInt32(&c.size, -1)

	if len(oldMap) == 1 {
		c.items.Store(make(map[interface{}]interface{}, c.initial))
		return
	}

	newMap := maps.Clone(oldMap)
	delete(newMap, key)
	c.items.Store(newMap)
}

// Range 遍历 map 中的项
func (c *cowMap) Range(f func(key, value interface{}) bool) {
	currentMap := c.items.Load().(map[interface{}]interface{})
	for k, v := range currentMap {
		if !f(k, v) {
			break
		}
	}
}

// Len 返回 map 中项的数量
// 优化版本：使用原子操作获取大小，避免访问map
func (c *cowMap) Len() uint32 {
	return uint32(atomic.LoadInt32(&c.size))
}

// Clear 移除 map 中的所有项
func (c *cowMap) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	atomic.StoreInt32(&c.size, 0)
	c.items.Store(make(map[interface{}]interface{}, c.initial))
}
