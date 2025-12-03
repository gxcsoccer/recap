# ReCAP Agent

ReCAP（Reason, Execute, Critique, Adjust/Plan）是一个基于 [eino](https://github.com/cloudwego/eino) 框架实现的增强型 Agent Loop。

## 概述

ReCAP 是 ReAct（Reasoning and Acting）模式的增强版本，增加了批判和规划步骤，使 Agent 能够：

1. **Reason（推理）** - 分析当前状态并决定采取什么行动
2. **Execute（执行）** - 使用工具执行行动
3. **Critique（批判）** - 评估行动的结果
4. **Adjust/Plan（调整/规划）** - 根据评估结果调整策略并规划下一步

这种循环模式使 Agent 具有更强的自我纠错能力和更好的任务完成质量。

## 特性

- ✅ 基于 eino Graph 的灵活架构
- ✅ 支持工具调用（Tool Calling）
- ✅ 内置批判和规划机制
- ✅ 支持流式输出（Streaming）
- ✅ 可配置的最大迭代次数
- ✅ 自定义批判提示词
- ✅ 完整的状态管理

## 安装

```bash
go get github.com/gxcsoccer/recap
go get github.com/cloudwego/eino
```

## 快速开始

### 基本使用

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/cloudwego/eino-ext/components/model/openai"
    "github.com/cloudwego/eino/components/tool"
    "github.com/cloudwego/eino/compose"
    "github.com/cloudwego/eino/schema"
    "github.com/gxcsoccer/recap/recap"
)

func main() {
    ctx := context.Background()

    // 创建 OpenAI 模型
    chatModel, _ := openai.NewChatModel(ctx, &openai.ChatModelConfig{
        APIKey: "your-api-key",
        Model:  "gpt-4",
    })

    // 配置 ReCAP Agent
    config := &recap.AgentConfig{
        ToolCallingModel: chatModel,
        ToolsConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{
                // 添加你的工具
            },
        },
        MaxIterations: 5,
    }

    // 创建 Agent
    agent, err := recap.NewAgent(ctx, config)
    if err != nil {
        log.Fatal(err)
    }

    // 使用 Agent
    response, err := agent.Generate(ctx, []*schema.Message{
        schema.UserMessage("你的问题"),
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(response.Content)
}
```

## 架构说明

ReCAP Agent 使用 eino 的 Graph 来构建工作流：

```
┌─────────┐
│  START  │
└────┬────┘
     │
     ▼
┌─────────────┐      No Tools      ┌─────┐
│   Reason    ├──────────────────► │ END │
│  (ChatML)   │                    └─────┘
└──────┬──────┘
       │ Has Tools
       ▼
┌─────────────┐
│   Execute   │
│   (Tools)   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Critique   │
│  (ChatML)   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│    Plan     │
│  (Lambda)   │
└──────┬──────┘
       │
       ├─────► Continue? ──► Reason (循环)
       │
       └─────► Complete? ──► Format ──► END
```

## 配置选项

### AgentConfig

```go
type AgentConfig struct {
    // 支持工具调用的聊天模型（推荐）
    ToolCallingModel model.ToolCallingChatModel

    // 工具配置
    ToolsConfig compose.ToolsNodeConfig

    // 批判模型（可选，默认使用 ToolCallingModel）
    CritiqueModel model.ChatModel

    // 最大迭代次数（默认：5）
    MaxIterations int

    // 最大执行步骤（默认：MaxIterations * 4 + 10）
    MaxSteps int

    // 批判提示词模板（可选）
    CritiquePrompt string

    // Graph 名称（默认："ReCAP"）
    GraphName string
}
```

## 与 ReAct 的对比

| 特性 | ReAct | ReCAP |
|------|-------|-------|
| 推理步骤 | ✅ | ✅ |
| 行动执行 | ✅ | ✅ |
| 结果批判 | ❌ | ✅ |
| 策略调整 | ❌ | ✅ |
| 自我纠错 | 有限 | 增强 |
| 迭代控制 | 简单 | 精细 |

## 示例

查看 `examples/basic/main.go` 获取完整示例。

运行示例：

```bash
export OPENAI_API_KEY=your_key_here
cd examples/basic
go run main.go
```

## API 参考

### Agent Methods

- `Generate(ctx, messages, opts...)` - 生成响应
- `Stream(ctx, messages, opts...)` - 流式生成响应
- `ExportGraph()` - 导出底层 Graph

## 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件

## 致谢

- [eino](https://github.com/cloudwego/eino) - 优秀的 LLM 应用开发框架
- ReAct 论文的启发
- CloudWeGo 社区
