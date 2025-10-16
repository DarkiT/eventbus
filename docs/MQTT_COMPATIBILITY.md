# MQTT 通配符兼容性说明

## 概述

EventBus 现在完全支持 MQTT 风格的主题通配符，提供了与 MQTT 协议兼容的主题匹配功能。

## 支持的通配符

### 1. `+` 单层通配符（Single-level wildcard）

- **用途**: 匹配任意一个层级
- **位置**: 可以出现在主题的任意位置
- **示例**:
  - `sensor/+/temperature` 匹配 `sensor/room1/temperature`、`sensor/room2/temperature`
  - `+/room1/message` 匹配 `chat/room1/message`、`system/room1/message`
  - `sensor/+/+` 匹配 `sensor/room1/temperature`、`sensor/room2/humidity`

### 2. `*` 末尾单层通配符（End-level wildcard）

- **用途**: 匹配最后一个层级
- **位置**: 只能出现在主题的末尾
- **示例**:
  - `sensor/room1/*` 匹配 `sensor/room1/temperature`、`sensor/room1/humidity`
  - `chat/*` 匹配 `chat/message`、`chat/user`

### 3. `#` 多层通配符（Multi-level wildcard）

- **用途**: 匹配零个或多个层级
- **位置**: 只能出现在主题的末尾
- **示例**:
  - `sensor/#` 匹配 `sensor/room1`、`sensor/room1/temperature`、`sensor/room1/temperature/high`
  - `system/#` 匹配 `system/cpu`、`system/memory/usage`
  - `#` 匹配所有主题

## 主题分隔符支持

EventBus 支持两种主题分隔符：

- **点分隔符 (`.`)**: `sensor.room1.temperature`
- **斜杠分隔符 (`/`)**: `sensor/room1/temperature`
- **混合使用**: `sensor/room1.temperature`

所有分隔符在内部都会统一转换为点分隔符处理，但传递给处理器的主题保持原始格式。

## 使用示例

```go
package main

import (
    "fmt"
    "github.com/darkit/eventbus"
)

func main() {
    bus := eventbus.New()
    
    // 订阅使用 + 通配符
    bus.Subscribe("sensor/+/temperature", func(topic string, payload any) {
        fmt.Printf("温度传感器 %s: %v\n", topic, payload)
    })
    
    // 订阅使用 * 通配符  
    bus.Subscribe("alert/*", func(topic string, payload any) {
        fmt.Printf("告警 %s: %v\n", topic, payload)
    })
    
    // 订阅使用 # 通配符
    bus.Subscribe("system/#", func(topic string, payload any) {
        fmt.Printf("系统事件 %s: %v\n", topic, payload)
    })
    
    // 发布消息
    bus.Publish("sensor/room1/temperature", "25°C")     // 匹配第一个订阅
    bus.Publish("alert/fire", "房间1发生火灾")             // 匹配第二个订阅
    bus.Publish("system/cpu/high", "CPU使用率过高")        // 匹配第三个订阅
    bus.Publish("system/memory/low/critical", "内存不足") // 匹配第三个订阅
}
```

## 匹配规则

1. **精确匹配优先**: 如果存在精确匹配的订阅，会优先使用
2. **通配符匹配**: 然后检查所有通配符模式
3. **防重复投递**: 同一个处理器不会收到重复消息
4. **原始主题保持**: 传递给处理器的主题保持发布时的原始格式

## 性能说明

- 通配符匹配采用高效算法，时间复杂度为 O(n)，其中 n 为主题层级数
- 内部使用 Copy-on-Write Map 优化并发访问
- 支持优先级处理和异步消息投递

## 兼容性保证

本实现完全兼容 MQTT 3.1.1 和 MQTT 5.0 规范中的主题过滤器规则，可以安全地用于 MQTT 相关的应用场景。