package eventbus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipePublishBatch(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	var total int32
	require.NoError(t, pipe.Subscribe(func(v int) { atomic.AddInt32(&total, int32(v)) }))

	res, err := pipe.PublishBatch([]int{1, 2, 3})
	assert.NoError(t, err)
	assert.Equal(t, 3, res.SuccessCount)
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, int32(6), atomic.LoadInt32(&total))
}

func TestPipePublishBatchSync(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	var total int32
	require.NoError(t, pipe.Subscribe(func(v int) { atomic.AddInt32(&total, int32(v)) }))

	res, err := pipe.PublishBatchSync([]int{4, 5})
	assert.NoError(t, err)
	assert.Equal(t, 2, res.SuccessCount)
	assert.Equal(t, int32(9), atomic.LoadInt32(&total))
}

func TestPipePublishBatch_NoHandlers(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	res, err := pipe.PublishBatch([]int{1})
	assert.NoError(t, err)
	assert.Equal(t, 1, res.SuccessCount)
}

func TestPipePublishBatch_Closed(t *testing.T) {
	pipe := NewPipe[int]()
	pipe.Close()

	res, err := pipe.PublishBatch([]int{1, 2})
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrChannelClosed)
}

func TestPipePublishBatchSync_Empty(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	res, err := pipe.PublishBatchSync(nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, res.Count)
}

func TestPipePublishBatch_PanicHandler(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	require.NoError(t, pipe.Subscribe(func(v int) {
		if v == 2 {
			panic("boom")
		}
	}))

	// second handler to ensure pipeline continues
	require.NoError(t, pipe.Subscribe(func(v int) {}))

	res, err := pipe.PublishBatch([]int{1, 2, 3})
	assert.NoError(t, err)
	assert.Equal(t, 3, res.Count)
}
