package eventbus

import (
	"strconv"
	"sync"
	"testing"
)

func BenchmarkSingleGoroutineStoreAbsentByMap(b *testing.B) {
	m := make(map[string]any)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m[strconv.Itoa(i)] = "value"
	}
}

func BenchmarkSingleGoroutineStoreAbsentBySyncMap(b *testing.B) {
	var m sync.Map
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store(strconv.Itoa(i), "value")
	}
}

func BenchmarkSingleGoroutineStoreAbsentBy_CowMap(b *testing.B) {
	m := newCowMap()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store(strconv.Itoa(i), "value")
	}
}

func BenchmarkSingleGoroutineStorePresentByMap(b *testing.B) {
	m := make(map[string]any)
	m["key"] = "value"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m["key"] = "value"
	}
}

func BenchmarkSingleGoroutineStorePresentBySyncMap(b *testing.B) {
	var m sync.Map
	m.Store("key", "value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store("key", "value")
	}
}

func BenchmarkSingleGoroutineStorePresentBy_CowMap(b *testing.B) {
	m := newCowMap()
	m.Store("key", "value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store("key", "value")
	}
}

func BenchmarkMultiGoroutineLoadDifferentBySyncMap(b *testing.B) {
	var m sync.Map
	finished := make(chan struct{}, b.N)
	for i := 0; i < 1000; i++ {
		m.Store(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() {
			for j := 0; j < 1000; j++ {
				m.Load(j)
			}
			finished <- struct{}{}
		}()
	}
	for i := 0; i < b.N; i++ {
		<-finished
	}
}

func BenchmarkMultiGoroutineLoadDifferentBy_CowMap(b *testing.B) {
	m := newCowMap()
	finished := make(chan struct{}, b.N)
	for i := 0; i < 1000; i++ {
		m.Store(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() {
			for j := 0; j < 1000; j++ {
				m.Load(j)
			}
			finished <- struct{}{}
		}()
	}
	for i := 0; i < b.N; i++ {
		<-finished
	}
}

func BenchmarkMultiGoroutineLoadSameBySyncMap(b *testing.B) {
	var m sync.Map
	finished := make(chan struct{}, b.N)
	m.Store("key", "value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() {
			for i := 0; i < 10; i++ {
				m.Load("key")
			}
			finished <- struct{}{}
		}()
	}
	for i := 0; i < b.N; i++ {
		<-finished
	}
}

func BenchmarkMultiGoroutineLoadSameBy_CowMap(b *testing.B) {
	m := newCowMap()
	finished := make(chan struct{}, b.N)
	m.Store("key", "value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() {
			for i := 0; i < 10; i++ {
				m.Load("key")
			}
			finished <- struct{}{}
		}()
	}
	for i := 0; i < b.N; i++ {
		<-finished
	}
}

func Benchmark__CowMapStore(b *testing.B) {
	m := newCowMap()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store(i, strconv.Itoa(i))
	}
}

func Benchmark__CowMapLoad(b *testing.B) {
	m := newCowMap()
	for i := 0; i < 1000; i++ {
		m.Store(i, strconv.Itoa(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Load(i % 1000)
	}
}

func Benchmark_SyncMapStore(b *testing.B) {
	var m sync.Map
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store(i, strconv.Itoa(i))
	}
}

func Benchmark_SyncMapLoad(b *testing.B) {
	var m sync.Map
	for i := 0; i < 1000; i++ {
		m.Store(i, strconv.Itoa(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Load(i % 1000)
	}
}
