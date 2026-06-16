package adapter

import (
	"bytes"
	"context"
	"encoding/json"
)

// Codec 负责在本地 payload 与跨节点 Message.Payload 字节之间转换。
// 实现应保持并发安全，因为 Bridge 会从多个 goroutine 调用 Codec。
type Codec interface {
	Encode(ctx context.Context, topic string, payload any) ([]byte, map[string]string, error)
	Decode(ctx context.Context, msg Message) (any, error)
}

// JSONCodec 使用 encoding/json 编码 payload，是 adapter 包默认 codec。
// Decode 使用 json.Number 保留数字精度，调用方可按业务类型二次转换。
type JSONCodec struct{}

// Encode 将 payload 编码为 JSON 字节并返回标准 header。
func (JSONCodec) Encode(ctx context.Context, topic string, payload any) ([]byte, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	return data, map[string]string{
		HeaderContentType: "application/json",
		HeaderCodec:       "json",
	}, nil
}

// Decode 将 JSON 字节解码为 any，并启用 json.Number。
func (JSONCodec) Decode(ctx context.Context, msg Message) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(msg.Payload))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// BytesCodec 直接传输 []byte 或 string，适合调用方自行序列化的场景。
type BytesCodec struct{}

// Encode 将 []byte 原样传输，将 string 转为字节；其他类型按 JSON 兜底会被拒绝。
func (BytesCodec) Encode(ctx context.Context, topic string, payload any) ([]byte, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	switch v := payload.(type) {
	case []byte:
		return append([]byte(nil), v...), map[string]string{
			HeaderContentType: "application/octet-stream",
			HeaderCodec:       "bytes",
		}, nil
	case string:
		return []byte(v), map[string]string{
			HeaderContentType: "text/plain; charset=utf-8",
			HeaderCodec:       "bytes",
		}, nil
	default:
		return nil, nil, ErrUnsupportedPayload
	}
}

// Decode 返回 Message.Payload 的拷贝，避免调用方修改共享数据。
func (BytesCodec) Decode(ctx context.Context, msg Message) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]byte(nil), msg.Payload...), nil
}
