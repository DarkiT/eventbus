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
	// 控制是否启用 valuesEqual，便于基准测试对比
	useValuesEqual bool
}

// newCowMap 创建一个新的 cowMap 实例
func newCowMap() *cowMap {
	cm := &cowMap{
		initial:        16,
		size:           0,
		useValuesEqual: true,
	}
	cm.items.Store(make(map[any]any, cm.initial))
	return cm
}

// newCowMapWithEqual 允许在基准测试中关闭 valuesEqual，避免影响默认行为
func newCowMapWithEqual(enable bool) *cowMap {
	cm := newCowMap()
	cm.useValuesEqual = enable
	return cm
}

// Store 向 map 中添加或更新一个项
// 优化版本：智能容量管理，减少不必要的复制
func (c *cowMap) Store(key, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldMap := c.items.Load().(map[any]any)

	// 检查是否是更新操作
	oldValue, exists := oldMap[key]
	if exists && c.useValuesEqual {
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
// 优化版本：针对常见类型快速路径优化
func valuesEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}

	// 快速路径：指针类型直接比较（EventBus 中最常见的场景）
	// 这避免了反射开销
	switch av := a.(type) {
	case *channel:
		if bv, ok := b.(*channel); ok {
			return av == bv
		}
		return false
	case string:
		if bv, ok := b.(string); ok {
			return av == bv
		}
		return false
	case int:
		if bv, ok := b.(int); ok {
			return av == bv
		}
		return false
	case int64:
		if bv, ok := b.(int64); ok {
			return av == bv
		}
		return false
	case bool:
		if bv, ok := b.(bool); ok {
			return av == bv
		}
		return false
	}

	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	if ta != tb {
		return false
	}

	// 快速路径：指针类型直接比较地址
	if ta.Kind() == reflect.Pointer {
		return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
	}

	// 快速路径：可比较类型直接比较
	if ta.Comparable() {
		return a == b
	}

	// 优化：slice 类型特殊处理
	if ta.Kind() == reflect.Slice {
		return slicesEqual(a, b)
	}

	// 其他不可比较类型使用 DeepEqual
	return reflect.DeepEqual(a, b)
}

// slicesEqual 优化的 slice 比较函数
func slicesEqual(a, b any) bool {
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)

	// 长度检查（快速路径）
	if va.Len() != vb.Len() {
		return false
	}

	// 常见类型快速路径（避免反射）
	switch a := a.(type) {
	case []byte:
		if b, ok := b.([]byte); ok {
			if len(a) != len(b) {
				return false
			}
			for i := range a {
				if a[i] != b[i] {
					return false
				}
			}
			return true
		}
	case []string:
		if b, ok := b.([]string); ok {
			if len(a) != len(b) {
				return false
			}
			for i := range a {
				if a[i] != b[i] {
					return false
				}
			}
			return true
		}
	case []int:
		if b, ok := b.([]int); ok {
			if len(a) != len(b) {
				return false
			}
			for i := range a {
				if a[i] != b[i] {
					return false
				}
			}
			return true
		}
	case []int64:
		if b, ok := b.([]int64); ok {
			if len(a) != len(b) {
				return false
			}
			for i := range a {
				if a[i] != b[i] {
					return false
				}
			}
			return true
		}
	case []float64:
		if b, ok := b.([]float64); ok {
			if len(a) != len(b) {
				return false
			}
			for i := range a {
				if a[i] != b[i] {
					return false
				}
			}
			return true
		}
	}

	// 其他 slice 类型使用反射逐元素比较
	for i := 0; i < va.Len(); i++ {
		if !reflect.DeepEqual(va.Index(i).Interface(), vb.Index(i).Interface()) {
			return false
		}
	}
	return true
}

// Load 从 map 中获取一个项
func (c *cowMap) Load(key any) (value any, ok bool) {
	currentMap := c.items.Load().(map[any]any)
	value, ok = currentMap[key]
	return value, ok
}

// LoadOrStore 原子地返回已存在的值或存入新值
func (c *cowMap) LoadOrStore(key, value any) (actual any, loaded bool) {
	currentMap := c.items.Load().(map[any]any)
	if v, ok := currentMap[key]; ok {
		return v, true
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	currentMap = c.items.Load().(map[any]any)
	if v, ok := currentMap[key]; ok {
		return v, true
	}

	newMap := maps.Clone(currentMap)
	newMap[key] = value
	c.items.Store(newMap)
	atomic.AddInt32(&c.size, 1)

	return value, false
}

// Delete 从 map 中删除一个项
// 优化版本：智能容量管理
func (c *cowMap) Delete(key any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldMap := c.items.Load().(map[any]any)
	if _, exists := oldMap[key]; !exists {
		return
	}

	// 更新大小计数
	atomic.AddInt32(&c.size, -1)

	if len(oldMap) == 1 {
		c.items.Store(make(map[any]any, c.initial))
		return
	}

	newMap := maps.Clone(oldMap)
	delete(newMap, key)
	c.items.Store(newMap)
}

// CompareAndDelete 仅当当前值与期望值一致时删除键，常用于安全回收共享对象。
func (c *cowMap) CompareAndDelete(key, expected any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldMap := c.items.Load().(map[any]any)
	current, exists := oldMap[key]
	if !exists || !valuesEqual(current, expected) {
		return false
	}

	atomic.AddInt32(&c.size, -1)

	if len(oldMap) == 1 {
		c.items.Store(make(map[any]any, c.initial))
		return true
	}

	newMap := maps.Clone(oldMap)
	delete(newMap, key)
	c.items.Store(newMap)
	return true
}

// Range 遍历 map 中的项
func (c *cowMap) Range(f func(key, value any) bool) {
	currentMap := c.items.Load().(map[any]any)
	for k, v := range currentMap {
		if !f(k, v) {
			break
		}
	}
}

// Len 返回 map 中项的数量
// 优化版本：使用原子操作获取大小，避免访问map
func (c *cowMap) Len() int {
	return int(atomic.LoadInt32(&c.size))
}

// Clear 移除 map 中的所有项
func (c *cowMap) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	atomic.StoreInt32(&c.size, 0)
	c.items.Store(make(map[any]any, c.initial))
}
