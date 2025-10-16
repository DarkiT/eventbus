package eventbus

import (
	"testing"
)

func BenchmarkPipePublishSync(b *testing.B) {
	pipe := NewPipe[int]()

	if err := pipe.Subscribe(pipeHandlerOne); err != nil {
		b.Fatalf("订阅失败: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := pipe.PublishSync(i); err != nil {
			b.Fatalf("发布失败: %v", err)
		}
	}
}

func BenchmarkPipePublish(b *testing.B) {
	pipe := NewPipe[int]()

	if err := pipe.Subscribe(pipeHandlerOne); err != nil {
		b.Fatalf("订阅失败: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := pipe.Publish(i); err != nil {
			b.Fatalf("发布失败: %v", err)
		}
	}
}

func BenchmarkPipeGoChannel(b *testing.B) {
	ch := make(chan int)

	go func() {
		for {
			val := <-ch
			pipeHandlerOne(val)
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- i
	}
}
