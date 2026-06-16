package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"
)

func benchmarkBusWithFastSuccess(b *testing.B, topic string) *EventBus {
	b.Helper()

	bus := New()
	if err := bus.SubscribeWithResponse(topic, func(topic string, payload any) (any, error) {
		return "ok", nil
	}); err != nil {
		b.Fatalf("响应式订阅失败: %v", err)
	}
	return bus
}

func benchmarkBusWithSlowAndFastSuccess(b *testing.B, topic string) *EventBus {
	b.Helper()

	bus := New()
	if err := bus.SubscribeWithResponseContext(topic, func(ctx context.Context, topic string, payload any) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Microsecond):
			return nil, errors.New("slow handler timeout")
		}
	}); err != nil {
		b.Fatalf("慢处理器订阅失败: %v", err)
	}
	if err := bus.SubscribeWithResponse(topic, func(topic string, payload any) (any, error) {
		return "ok", nil
	}); err != nil {
		b.Fatalf("快速处理器订阅失败: %v", err)
	}
	return bus
}

func BenchmarkPublishSyncAny_Scenarios(b *testing.B) {
	b.Run("SingleFastSuccess", func(b *testing.B) {
		bus := benchmarkBusWithFastSuccess(b, "bench.sync.any.single")
		defer bus.Close()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result, err := bus.PublishSyncAny("bench.sync.any.single", "payload")
			if err != nil || !result.Success {
				b.Fatalf("PublishSyncAny 失败: %v", err)
			}
		}
	})

	b.Run("SlowAndFastSuccess", func(b *testing.B) {
		bus := benchmarkBusWithSlowAndFastSuccess(b, "bench.sync.any.mix")
		defer bus.Close()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result, err := bus.PublishSyncAny("bench.sync.any.mix", "payload")
			if err != nil || !result.Success {
				b.Fatalf("PublishSyncAny 失败: %v", err)
			}
		}
	})
}

func BenchmarkPublishSyncAnyValue_Scenarios(b *testing.B) {
	b.Run("SingleFastSuccess", func(b *testing.B) {
		bus := benchmarkBusWithFastSuccess(b, "bench.sync.anyvalue.single")
		defer bus.Close()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			value, err := bus.PublishSyncAnyValue("bench.sync.anyvalue.single", "payload")
			if err != nil || value != "ok" {
				b.Fatalf("PublishSyncAnyValue 失败: value=%v err=%v", value, err)
			}
		}
	})

	b.Run("SlowAndFastSuccess", func(b *testing.B) {
		bus := benchmarkBusWithSlowAndFastSuccess(b, "bench.sync.anyvalue.mix")
		defer bus.Close()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			value, err := bus.PublishSyncAnyValue("bench.sync.anyvalue.mix", "payload")
			if err != nil || value != "ok" {
				b.Fatalf("PublishSyncAnyValue 失败: value=%v err=%v", value, err)
			}
		}
	})
}
