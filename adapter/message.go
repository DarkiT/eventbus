package adapter

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// HeaderContentType 标识消息体编码格式，适配器可按需映射到 broker header。
	HeaderContentType = "content-type"
	// HeaderCodec 标识消息使用的 codec 名称，便于跨语言或跨版本排障。
	HeaderCodec = "eventbus-codec"
)

var fallbackID atomic.Uint64

// Message 是跨节点传输的标准事件信封。
// Payload 必须是已编码的字节序列；业务对象与字节之间的转换由 Codec 负责。
type Message struct {
	ID        string            `json:"id"`
	Topic     string            `json:"topic"`
	Key       string            `json:"key,omitempty"`
	Payload   []byte            `json:"payload"`
	Headers   map[string]string `json:"headers,omitempty"`
	Origin    string            `json:"origin"`
	Timestamp time.Time         `json:"timestamp"`
}

// Clone 返回消息深拷贝，避免 transport 或调用方意外修改共享切片与 header。
func (m Message) Clone() Message {
	cloned := m
	if m.Payload != nil {
		cloned.Payload = append([]byte(nil), m.Payload...)
	}
	if m.Headers != nil {
		cloned.Headers = make(map[string]string, len(m.Headers))
		maps.Copy(cloned.Headers, m.Headers)
	}
	return cloned
}

// EnsureDefaults 补齐消息 ID、时间戳与 Headers，返回可安全修改的新消息。
func (m Message) EnsureDefaults(origin string) Message {
	m = m.Clone()
	if strings.TrimSpace(m.ID) == "" {
		m.ID = NewMessageID()
	}
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now().UTC()
	}
	if strings.TrimSpace(m.Origin) == "" {
		m.Origin = origin
	}
	if m.Headers == nil {
		m.Headers = make(map[string]string)
	}
	return m
}

// NewMessageID 生成适合作为跨节点去重键的随机消息 ID。
func NewMessageID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("fallback-%d-%d", time.Now().UnixNano(), fallbackID.Add(1))
}

// NewNodeID 生成节点标识；优先使用 hostname + 随机后缀，避免同机多进程冲突。
func NewNodeID() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "node"
	}
	hostname = sanitizeNodeID(hostname)
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", hostname, fallbackID.Add(1))
	}
	return hostname + "-" + hex.EncodeToString(b[:])
}

func sanitizeNodeID(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "node"
	}
	return builder.String()
}
