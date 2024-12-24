package eventbus

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_SingletonSubscribe(t *testing.T) {
	ResetSingleton()
	err := Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)
	assert.NotNil(t, singleton)

	err = Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)
	err = Subscribe("testtopic", 1)
	assert.Equal(t, ErrHandlerIsNotFunc, err)

	err = Subscribe("testtopic", func(topic string) error {
		return nil
	})
	assert.Equal(t, ErrHandlerParamNum, err)
	err = Subscribe("testtopic", func(topic int, payload int) error {
		return nil
	})

	assert.Equal(t, ErrHandlerFirstParam, err)
	singleton.Close()
	err = Unsubscribe("testtopic", busHandlerTwo)
	assert.Equal(t, ErrChannelClosed, err)
	Close()
}

func Test_SingletonUnsubscribe(t *testing.T) {
	ResetSingleton()
	err := Unsubscribe("testtopic", busHandlerOne)
	assert.Equal(t, ErrNoSubscriber, err)
	assert.NotNil(t, singleton)

	err = Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	err = Unsubscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)
	singleton.Close()

	err = Unsubscribe("testtopic", busHandlerTwo)
	assert.Equal(t, ErrChannelClosed, err)
	Close()
}

func Test_SingletonPublish(t *testing.T) {
	ResetSingleton()
	err := Publish("testtopic", 1)
	assert.Nil(t, err)
	assert.NotNil(t, singleton)

	err = Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	var wg sync.WaitGroup
	wg.Add(100)

	for i := 0; i < 100; i++ {
		go func() {
			for i := 0; i < 100; i++ {
				err := Publish("testtopic", i)
				assert.Nil(t, err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	Close()
}

func Test_SingletonPublishSync(t *testing.T) {
	ResetSingleton()
	err := Publish("testtopic", 1)
	assert.Nil(t, err)
	assert.NotNil(t, singleton)

	err = Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	var wg sync.WaitGroup
	wg.Add(100)

	for i := 0; i < 100; i++ {
		go func() {
			for i := 0; i < 100; i++ {
				err := PublishSync("testtopic", i)
				assert.Nil(t, err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	Close()
}

func Test_SingletonPublishWithTimeout(t *testing.T) {
	ResetSingleton()
	assert.NotNil(t, singleton, "Singleton should be initialized")

	// 测试超时情况
	err := Subscribe("slowtopic", func(topic string, msg interface{}) {
		time.Sleep(2 * time.Second)
	})
	assert.Nil(t, err, "Subscribe should succeed")

	err = PublishWithTimeout("slowtopic", "test", time.Second)
	assert.NotNil(t, err, "Should timeout")
	assert.Contains(t, err.Error(), "timeout")

	// 测试正常情况
	processed := false
	err = Subscribe("fasttopic", func(topic string, msg interface{}) {
		processed = true
	})
	assert.Nil(t, err, "Subscribe should succeed")

	err = PublishWithTimeout("fasttopic", "test", 2*time.Second)
	assert.Nil(t, err, "Should not timeout")
	assert.True(t, processed, "Message should have been processed")

	// 测试无订阅者情况
	err = PublishWithTimeout("notopic", "test", time.Second)
	assert.Nil(t, err, "Should succeed with no subscribers")

	Close()
}

func BenchmarkSingletonPublish(b *testing.B) {
	ResetSingleton()
	Subscribe("testtopic", busHandlerOne)

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < b.N; i++ {
			Publish("testtopic", i)
		}
		wg.Done()
	}()
	wg.Wait()
	Close()
}

func BenchmarkSingletonPublishSync(b *testing.B) {
	ResetSingleton()
	Subscribe("testtopic", busHandlerOne)

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < b.N; i++ {
			PublishSync("testtopic", i)
		}
		wg.Done()
	}()
	wg.Wait()
	Close()
}
