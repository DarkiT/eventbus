package eventbus

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_newCowMap(t *testing.T) {
	m := newCowMap()
	assert.NotNil(t, m)
}

func Test_CowMapLoad(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}

	for i := range 100 {
		val, ok := m.Load(i)
		assert.True(t, ok)
		assert.Equal(t, strconv.Itoa(i), val.(string))
	}

	val, ok := m.Load(999999)
	assert.False(t, ok)
	assert.Equal(t, nil, val)
}

func Test_CowMapStore(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}

	assert.Equal(t, 100, m.Len())
}

func Test_CowMapDelete(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}

	for i := range 50 {
		m.Delete(i)
	}

	for i := range 50 {
		val, ok := m.Load(i)
		assert.False(t, ok)
		assert.Equal(t, nil, val)
	}

	for i := 50; i < 100; i++ {
		val, ok := m.Load(i)
		assert.True(t, ok)
		assert.Equal(t, strconv.Itoa(i), val.(string))
	}
}

func Test_CowMapClear(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}

	m.Clear()
	assert.Equal(t, 0, m.Len())
}

func Test_CowMapStoreUncomparable(t *testing.T) {
	m := newCowMap()
	slice := []int{1, 2, 3}

	require.NotPanics(t, func() {
		m.Store("slice", slice)
	})
	require.NotPanics(t, func() {
		m.Store("slice", slice)
	})

	mapValue := map[string]int{"a": 1}
	require.NotPanics(t, func() {
		m.Store("map", mapValue)
	})
	require.NotPanics(t, func() {
		m.Store("map", mapValue)
	})

	assert.Equal(t, 2, m.Len())
}

func Test_CowMapLen(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}
	assert.Equal(t, 100, m.Len())
}

func Test_CowMapRange(t *testing.T) {
	m := newCowMap()
	expert := make([]bool, 100)

	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}
	m.Range(func(key any, value any) bool {
		if value.(string) == strconv.Itoa(key.(int)) {
			expert[key.(int)] = true
		}
		return true
	})

	for _, val := range expert {
		assert.True(t, val)
	}
}

func Test_CowMapRangeStop(t *testing.T) {
	m := newCowMap()
	results := make([]bool, 100)

	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}

	count := 0
	m.Range(func(key any, value any) bool {
		// only range ten elements and then stop range
		if count >= 10 {
			return false
		}

		count++
		if value.(string) == strconv.Itoa(key.(int)) {
			results[key.(int)] = true
		}
		return true
	})

	expert := 0
	for _, val := range results {
		if val {
			expert++
		}
	}
	assert.Equal(t, count, expert)
}

func Test_CowMapConcurrentLoadPanic(t *testing.T) {
	m := newCowMap()
	assert.NotPanics(t, func() {
		for range 100 {
			go func() {
				for j := range 100 {
					m.Store(j, strconv.Itoa(j))
				}
			}()
		}
	})
}

func Test_CowMapConcurrentStorePanic(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, strconv.Itoa(i))
	}

	assert.NotPanics(t, func() {
		for range 100 {
			go func() {
				for j := range 100 {
					m.Load(j)
				}
			}()
		}
	})
}

func Test_CowMapStoreOrLoadConcurrent(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, i)
	}

	storewg := sync.WaitGroup{}
	storewg.Add(100)
	assert.NotPanics(t, func() {
		for i := range 100 {
			go func(index int) {
				for j := index * 100; j < (index+1)*100; j++ {
					m.Store(j, j)
				}
				storewg.Done()
			}(i)
		}
	})
	storewg.Wait()

	loadwg := sync.WaitGroup{}
	loadwg.Add(100)
	assert.NotPanics(t, func() {
		for i := range 100 {
			go func(index int) {
				for j := index * 100; j < (index+1)*100; j++ {
					val, ok := m.Load(j)
					assert.True(t, ok)
					assert.Equal(t, j, val)
				}
				loadwg.Done()
			}(i)
		}
	})
	loadwg.Wait()
}

func Test_CowMapStoreAndLoadConcurrent(t *testing.T) {
	m := newCowMap()
	for i := range 100 {
		m.Store(i, i)
	}

	assert.NotPanics(t, func() {
		loadGoroutineSize := 100
		loadWg := sync.WaitGroup{}
		loadWg.Add(loadGoroutineSize)

		for range loadGoroutineSize {
			go func() {
				for j := range 100 {
					val, ok := m.Load(j)
					assert.True(t, ok)
					assert.Equal(t, j, val)
				}
				loadWg.Done()
			}()
		}

		storeGoroutineSize := 100
		storeWg := sync.WaitGroup{}
		storeWg.Add(storeGoroutineSize)
		for i := range storeGoroutineSize {
			go func(index int) {
				for j := range 100 {
					m.Store(j, j)
				}
				storeWg.Done()
			}(i)
		}

		storeWg.Wait()
		loadWg.Wait()
	})
}

func Test_CowMapLoadOrStoreConcurrentSingleCreation(t *testing.T) {
	m := newCowMap()

	const goroutines = 200
	start := make(chan struct{})
	var created int64
	var first any

	results := make(chan any, goroutines)
	flags := make(chan bool, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			<-start
			actual, loaded := m.LoadOrStore("only", id)
			flags <- loaded
			results <- actual
			if !loaded {
				atomic.AddInt64(&created, 1)
				first = actual
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(results)
	close(flags)

	assert.Equal(t, int64(1), created)

	final, ok := m.Load("only")
	assert.True(t, ok)
	assert.Equal(t, first, final)
	assert.Equal(t, 1, m.Len())

	for v := range results {
		assert.Equal(t, first, v)
	}

	loadedCount := 0
	for f := range flags {
		if f {
			loadedCount++
		}
	}
	assert.Equal(t, goroutines-1, loadedCount)
}

func Test_CowMapLoadOrStoreExistingValue(t *testing.T) {
	m := newCowMap()
	m.Store("key", "value1")

	actual, loaded := m.LoadOrStore("key", "value2")
	assert.True(t, loaded)
	assert.Equal(t, "value1", actual)
	assert.Equal(t, 1, m.Len())

	val, ok := m.Load("key")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}
