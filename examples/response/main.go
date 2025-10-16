package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkit/eventbus"
)

func must(err error) {
	if err != nil {
		log.Fatalf("操作失败: %v", err)
	}
}

func main() {
	bus := eventbus.New()
	defer bus.Close()

	fmt.Println("🚀 EventBus 响应式发布功能演示")
	fmt.Println("================================")

	// 示例1: PublishSyncAll - 所有处理器必须成功
	fmt.Println("\n📋 示例1: PublishSyncAll - 订单处理场景")
	setupOrderProcessing(bus)
	demonstrateOrderProcessing(bus)

	// 示例2: PublishSyncAny - 任一处理器成功即可
	fmt.Println("\n📋 示例2: PublishSyncAny - 通知服务场景")
	setupNotificationService(bus)
	demonstrateNotificationService(bus)

	// 示例3: 错误处理和超时
	fmt.Println("\n📋 示例3: 错误处理和超时控制")
	setupErrorHandling(bus)
	demonstrateErrorHandling(bus)

	// 示例4: MQTT通配符支持
	fmt.Println("\n📋 示例4: MQTT通配符响应式发布")
	setupMQTTWildcard(bus)
	demonstrateMQTTWildcard(bus)
}

func setupOrderProcessing(bus *eventbus.EventBus) {
	// 库存检查服务
	must(bus.SubscribeWithResponse("order/process", func(topic string, payload any) (any, error) {
		order := payload.(map[string]any)
		fmt.Printf("  📦 库存检查: 订单 %v\n", order["id"])

		// 模拟库存检查
		time.Sleep(10 * time.Millisecond)
		if order["quantity"].(int) > 100 {
			return nil, errors.New("库存不足")
		}
		return map[string]any{"inventory": "充足", "reserved": true}, nil
	}))

	// 支付处理服务
	must(bus.SubscribeWithResponse("order/process", func(topic string, payload any) (any, error) {
		order := payload.(map[string]any)
		fmt.Printf("  💳 支付处理: 订单 %v, 金额 ¥%v\n", order["id"], order["amount"])

		// 模拟支付处理
		time.Sleep(15 * time.Millisecond)
		return map[string]any{"payment": "成功", "transaction_id": "TXN123456"}, nil
	}))

	// 物流安排服务
	must(bus.SubscribeWithResponse("order/process", func(topic string, payload any) (any, error) {
		order := payload.(map[string]any)
		fmt.Printf("  🚚 物流安排: 订单 %v\n", order["id"])

		// 模拟物流安排
		time.Sleep(8 * time.Millisecond)
		return map[string]any{"shipping": "已安排", "tracking": "SF123456789"}, nil
	}))
}

func demonstrateOrderProcessing(bus *eventbus.EventBus) {
	// 正常订单处理
	order1 := map[string]any{
		"id":       "ORDER-001",
		"quantity": 50,
		"amount":   299.99,
	}

	fmt.Printf("处理订单: %v\n", order1["id"])
	result, err := bus.PublishSyncAll("order/process", order1)
	if err != nil {
		log.Printf("❌ 订单处理失败: %v", err)
		return
	}

	if result.Success {
		fmt.Printf("✅ 订单处理成功! 耗时: %v\n", result.TotalTime)
		fmt.Printf("   📊 统计: %d个服务全部成功\n", result.SuccessCount)

		// 显示各服务的响应
		for i, handlerResult := range result.Results {
			if handlerResult.Success {
				fmt.Printf("   服务%d: %v (耗时: %v)\n", i+1, handlerResult.Result, handlerResult.Duration)
			}
		}
	} else {
		fmt.Printf("❌ 订单处理失败: %d/%d 服务成功\n", result.SuccessCount, result.HandlerCount)
		for _, handlerResult := range result.Results {
			if !handlerResult.Success {
				fmt.Printf("   ❌ 服务失败: %v\n", handlerResult.Error)
			}
		}
	}

	// 库存不足的订单
	fmt.Printf("\n处理大批量订单(将触发库存不足):\n")
	order2 := map[string]any{
		"id":       "ORDER-002",
		"quantity": 150, // 超过库存限制
		"amount":   999.99,
	}

	result, err = bus.PublishSyncAll("order/process", order2)
	if err != nil {
		log.Printf("❌ 订单处理失败: %v", err)
		return
	}

	if !result.Success {
		fmt.Printf("❌ 订单处理失败: %d/%d 服务成功\n", result.SuccessCount, result.HandlerCount)
	}
}

func setupNotificationService(bus *eventbus.EventBus) {
	// 邮件通知服务 (主要)
	must(bus.SubscribeWithResponse("notification/send", func(topic string, payload any) (any, error) {
		msg := payload.(map[string]any)
		fmt.Printf("  📧 邮件服务: 发送给 %v\n", msg["recipient"])

		// 模拟网络延迟
		time.Sleep(20 * time.Millisecond)
		return map[string]any{"channel": "email", "status": "sent"}, nil
	}))

	// 短信通知服务 (备用)
	must(bus.SubscribeWithResponse("notification/send", func(topic string, payload any) (any, error) {
		msg := payload.(map[string]any)
		fmt.Printf("  📱 短信服务: 发送给 %v\n", msg["recipient"])

		// 模拟短信发送
		time.Sleep(5 * time.Millisecond)
		return map[string]any{"channel": "sms", "status": "sent"}, nil
	}))

	// 推送通知服务 (备用)
	must(bus.SubscribeWithResponse("notification/send", func(topic string, payload any) (any, error) {
		msg := payload.(map[string]any)
		fmt.Printf("  🔔 推送服务: 发送给 %v\n", msg["recipient"])

		// 模拟推送通知
		time.Sleep(3 * time.Millisecond)
		return map[string]any{"channel": "push", "status": "sent"}, nil
	}))
}

func demonstrateNotificationService(bus *eventbus.EventBus) {
	notification := map[string]any{
		"recipient": "user@example.com",
		"title":     "订单确认",
		"content":   "您的订单已成功处理",
	}

	fmt.Printf("发送通知给: %v\n", notification["recipient"])
	result, err := bus.PublishSyncAny("notification/send", notification)
	if err != nil {
		log.Printf("❌ 通知发送失败: %v", err)
		return
	}

	if result.Success {
		fmt.Printf("✅ 通知发送成功! 耗时: %v\n", result.TotalTime)
		fmt.Printf("   📊 统计: %d/%d 个通道成功\n", result.SuccessCount, result.HandlerCount)

		// 显示成功的通道
		for _, handlerResult := range result.Results {
			if handlerResult.Success {
				channel := handlerResult.Result.(map[string]any)["channel"]
				fmt.Printf("   ✅ %s 通道发送成功 (耗时: %v)\n", channel, handlerResult.Duration)
			}
		}
	} else {
		fmt.Printf("❌ 所有通知通道都失败了\n")
	}
}

func setupErrorHandling(bus *eventbus.EventBus) {
	// 会超时的服务
	must(bus.SubscribeWithResponse("timeout/test", func(topic string, payload any) (any, error) {
		fmt.Println("  ⏰ 慢服务开始处理...")
		time.Sleep(200 * time.Millisecond) // 超过超时时间
		return "完成", nil
	}))

	// 会panic的服务
	must(bus.SubscribeWithResponse("panic/test", func(topic string, payload any) (any, error) {
		fmt.Println("  💥 即将panic的服务...")
		panic("模拟服务崩溃")
	}))

	// 正常的服务
	must(bus.SubscribeWithResponse("panic/test", func(topic string, payload any) (any, error) {
		fmt.Println("  ✅ 正常服务处理中...")
		return "正常完成", nil
	}))
}

func demonstrateErrorHandling(bus *eventbus.EventBus) {
	// Panic恢复测试
	fmt.Println("\n测试Panic恢复:")
	result, err := bus.PublishSyncAll("panic/test", "data")
	if err != nil {
		log.Printf("❌ 发布失败: %v", err)
		return
	}

	fmt.Printf("  📊 结果: %d成功, %d失败\n", result.SuccessCount, result.FailureCount)
	for _, handlerResult := range result.Results {
		if !handlerResult.Success {
			fmt.Printf("  💥 Panic已捕获: %v\n", handlerResult.Error)
		} else {
			fmt.Printf("  ✅ 正常服务: %v\n", handlerResult.Result)
		}
	}
}

func setupMQTTWildcard(bus *eventbus.EventBus) {
	// 传感器数据处理 (+通配符)
	must(bus.SubscribeWithResponse("sensor/+/temperature", func(topic string, payload any) (any, error) {
		fmt.Printf("  🌡️  温度处理器: %s = %v°C\n", topic, payload)
		return map[string]any{"processed": true, "type": "temperature"}, nil
	}))

	// 系统监控 (#通配符)
	must(bus.SubscribeWithResponse("system/#", func(topic string, payload any) (any, error) {
		fmt.Printf("  📊 系统监控: %s = %v\n", topic, payload)
		return map[string]any{"logged": true, "timestamp": time.Now()}, nil
	}))
}

func demonstrateMQTTWildcard(bus *eventbus.EventBus) {
	// 发布传感器数据
	fmt.Println("发布传感器数据:")
	result, err := bus.PublishSyncAll("sensor/room1/temperature", "25.5")
	if err != nil {
		log.Printf("❌ 发布失败: %v", err)
		return
	}

	if result.Success {
		fmt.Printf("✅ 传感器数据处理成功! 匹配了 %d 个处理器\n", result.HandlerCount)
	}

	// 发布系统事件
	fmt.Println("\n发布系统事件:")
	result, err = bus.PublishSyncAll("system/cpu/usage", "85%")
	if err != nil {
		log.Printf("❌ 发布失败: %v", err)
		return
	}

	if result.Success {
		fmt.Printf("✅ 系统事件处理成功! 匹配了 %d 个处理器\n", result.HandlerCount)
	}
}
