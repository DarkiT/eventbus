package eventbus

import "testing"

func BenchmarkEventBus_SinglePublish1000(b *testing.B) {
	bus := New()
	_ = bus.Subscribe("a", func(topic string, payload any) {})
	defer bus.Close()

	payload := 1
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for range 1000 {
			_ = bus.Publish("a", payload)
		}
	}
}

func BenchmarkEventBus_BatchPublish1000(b *testing.B) {
	bus := New()
	_ = bus.Subscribe("a", func(topic string, payload any) {})
	defer bus.Close()

	batch := make([]BatchMessage, 1000)
	for i := range 1000 {
		batch[i] = BatchMessage{Topic: "a", Payload: i}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bus.PublishBatch(batch)
	}
}

func BenchmarkEventBus_MixedTopicBatch(b *testing.B) {
	bus := New()
	_ = bus.Subscribe("a", func(topic string, payload any) {})
	_ = bus.Subscribe("b", func(topic string, payload any) {})
	defer bus.Close()

	batch := make([]BatchMessage, 1000)
	for i := range 1000 {
		if i%2 == 0 {
			batch[i] = BatchMessage{Topic: "a", Payload: i}
		} else {
			batch[i] = BatchMessage{Topic: "b", Payload: i}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bus.PublishBatch(batch)
	}
}

// 补充单发对比基准（1000 次单独调用）
func BenchmarkEventBus_SinglePublish1000Times(b *testing.B) {
	bus := New()
	_ = bus.Subscribe("bench.single", func(topic string, payload any) {})
	defer bus.Close()

	payload := 1
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for range 1000 {
			_ = bus.Publish("bench.single", payload)
		}
	}
}

// 补充批量一次 1000 条
func BenchmarkEventBus_BatchPublish1000Items(b *testing.B) {
	bus := New()
	_ = bus.Subscribe("bench.batch", func(topic string, payload any) {})
	defer bus.Close()

	batch := make([]BatchMessage, 1000)
	for i := range 1000 {
		batch[i] = BatchMessage{Topic: "bench.batch", Payload: i}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bus.PublishBatch(batch)
	}
}
