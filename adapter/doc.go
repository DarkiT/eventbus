// Package adapter 为 github.com/darkit/eventbus 提供跨节点集群桥接能力。
//
// adapter 包使核心 eventbus 库保持轻量且无 broker SDK 依赖的同时，
// 提供跨节点 pub/sub 能力。它定义了稳定的抽象层，用于桥接本地事件总线与远端消息代理。
//
// 架构
//
// 本包采用分层架构：
//
//	本地 EventBus → Bridge → Transport → Broker/集群
//
//	- EventBus: 负责进程内发布、订阅、通配符匹配、优先级和本地处理器执行。
//	- Bridge: 管理编码、出站队列、防回环、去重、生命周期和错误上报。
//	- Transport: 由具体适配器实现的抽象接口（Redis、NATS、MQTT、Kafka 等）。
//	- Codec: 处理 Go 类型与 []byte 之间的 payload 序列化（默认：JSONCodec）。
//
// 核心类型
//
//   - Message: 跨节点传输的标准事件信封。
//   - Bridge: 连接本地 EventBus 与远端 Transport。
//   - Transport: broker 特定实现的接口。
//   - Codec: payload 编码/解码接口。
//   - JSONCodec: 默认基于 JSON 的编解码实现。
//   - BytesCodec: 用于预序列化负载的原始字节编解码器。
//
// 设计原则
//
//   - 传输层无关：核心 adapter 包不依赖特定 broker SDK。
//   - best-effort 语义：Bridge 提供至少一次投递，不保证 exactly-once。
//   - 防回环：使用 Message.Origin 和 context 标记防止消息循环。
//   - 去重：基于时间窗口的 Message.ID 去重。
//   - 异步出站：有界队列 + worker 模式，避免阻塞本地发布者。
//   - 能力声明：传输层特定特性（通配符、持久化、ACK、消费组、按键保序）
//     通过 Capabilities 声明。
//
// 语义边界（v1）
//
// 支持：
//
//   - best-effort 跨节点 pub/sub
//   - 通过 Message.Origin 防回环
//   - 通过 Message.ID 时间窗口去重
//   - 可配置超时的出站队列
//   - 通过 ErrorHandler 的错误回调
//   - 传输层能力声明（通配符、持久化、ACK、消费组、按键保序、请求-回复）
//
// 不保证：
//
//   - 跨节点 PublishSyncAll / PublishSyncAny
//   - 全局 SubscribeOnce
//   - 全局顺序
//   - exactly-once 投递
//   - Broker 持久化与 ACK 语义一致性
//
// 这些能力应由具体 Transport 实现根据其 broker 特性单独设计。
//
// 错误处理与重试契约
//
//   - handleRemote 返回的 error 仅用于向 Transport 透出处理结果，不代表可重试。
//     其中解码失败（StageDecode）属于不可恢复的毒消息：Transport 不应据此重试，
//     否则会在 broker 端触发无限重投，应直接丢弃或转入死信队列。
//   - 本地发布失败（StageLocalPublish）通常源于总线关闭或入队超时，重试同样无意义；
//     Transport 实现应将 Bridge 返回的 error 视为 best-effort 信号而非重试触发器。
//   - Bridge 的错误回调（ErrorHandler）在内部锁外同步调用并有 recover 保护；
//     Handler 应快速返回，耗时日志/告警建议自行投递到有界队列，避免拖慢 Bridge。
//
// 快速开始
//
//	bus := eventbus.New(-1)
//	transport := NewYourTransport(...) // 实现 adapter.Transport
//
//	bridge, err := adapter.NewBridge(
//	    bus,
//	    transport,
//	    adapter.WithNodeID("service-a-1"),
//	    adapter.WithRemotePatterns("orders.#"),
//	    adapter.WithErrorHandler(func(e adapter.ErrorEvent) {
//	        log.Printf("bridge error: stage=%s topic=%s id=%s err=%v",
//	            e.Stage, e.Topic, e.MessageID, e.Err)
//	    }),
//	)
//	if err != nil {
//	    return err
//	}
//	if err := bridge.Start(context.Background()); err != nil {
//	    return err
//	}
//	defer bridge.Close()
//
// 安全考虑
//
//   - 本包不读取环境变量、凭证文件或网络配置。
//   - Message.Payload 被视为不可信数据；解码错误会返回给 Transport 处理器。
//   - 生产环境应显式配置 NodeID，避免重启后排障困难。
//   - Headers 不应包含密钥或敏感凭证；应使用 broker 原生认证机制。
//
// 限制
//
//   - 无跨节点 PublishSyncAll / PublishSyncAny
//   - 无全局 SubscribeOnce
//   - 无全局顺序保证（可与支持按键保序的 Transport 配合使用 Message.Key）
//   - 无 exactly-once 保证（应用层仍需幂等）
//   - 通配符支持取决于 Transport 的 Capabilities.Wildcard
//
// 详见 DESIGN.md 了解详细设计原理和决策记录。
package adapter
