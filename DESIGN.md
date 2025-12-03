# ReCAP Agent Design

## 概述 (Overview)

ReCAP 是一个基于 eino 框架实现的增强型 Agent Loop 架构。与传统的 ReAct (Reasoning and Acting) 模式相比，ReCAP 增加了批判（Critique）和规划（Plan）步骤，形成一个完整的反馈循环。

## 核心组件 (Core Components)

### 1. Reason（推理）
- **职责**: 分析当前状态，决定采取什么行动
- **实现**: 使用带有工具调用能力的 ChatModel
- **输入**: 用户消息 + 历史对话
- **输出**: 带有工具调用的消息 或 最终回复

### 2. Execute（执行）
- **职责**: 执行 LLM 选择的工具
- **实现**: ToolsNode，执行多个工具调用
- **输入**: 包含 ToolCalls 的消息
- **输出**: 工具执行结果的消息数组

### 3. Critique（批判）
- **职责**: 评估工具执行结果的质量和完整性
- **实现**: 使用 ChatModel 进行评估
- **输入**: 工具执行结果
- **输出**: 评估结果，包含以下三种状态之一：
  - `COMPLETE: <summary>` - 任务已完成
  - `CONTINUE: <what's missing>` - 需要继续执行
  - `ERROR: <issue>` - 出现问题

### 4. Plan（规划）
- **职责**: 根据批判结果决定下一步行动
- **实现**: Lambda 函数，基于状态做决策
- **输入**: 批判结果
- **输出**: 
  - 继续: 返回更新后的消息数组，循环回 Reason
  - 完成: 返回最终答案

## 工作流程 (Workflow)

```
用户输入
  ↓
[Reason] → 推理并决定行动
  ↓
  ├─→ 无需工具 → [END] 直接返回答案
  │
  └─→ 需要工具 ↓
     [Execute] → 执行工具
       ↓
     [Critique] → 评估结果
       ↓
     [Plan] → 决策
       ↓
       ├─→ 继续 → 返回 [Reason] (循环)
       │
       └─→ 完成 → [Format] → [END]
```

## 状态管理 (State Management)

ReCAP 使用 `State` 结构管理整个执行过程：

```go
type State struct {
    Messages         []*schema.Message  // 所有消息历史
    Iterations       int                // 当前迭代次数
    CritiqueResult   string             // 最新批判结果
    ShouldContinue   bool               // 是否继续迭代
    FinalAnswer      string             // 最终答案
    ReturnDirectlyID string             // 直接返回的工具调用 ID
}
```

## 与 ReAct 的对比 (Comparison with ReAct)

| 特性 | ReAct | ReCAP |
|------|-------|-------|
| **基本流程** | Reason → Act → Repeat | Reason → Execute → Critique → Plan |
| **自我评估** | ❌ 无 | ✅ 内置批判机制 |
| **迭代控制** | 基于最大步数 | 基于批判结果 + 最大迭代次数 |
| **错误处理** | 有限 | 通过批判步骤识别和处理 |
| **质量保证** | 依赖 LLM | 双重验证（执行 + 批判） |
| **适用场景** | 简单任务 | 复杂任务、需要质量保证的场景 |

## 配置选项 (Configuration)

### AgentConfig

```go
type AgentConfig struct {
    // 必需：支持工具调用的聊天模型
    ToolCallingModel model.ToolCallingChatModel

    // 必需：工具配置
    ToolsConfig compose.ToolsNodeConfig

    // 可选：批判模型（默认使用 ToolCallingModel）
    CritiqueModel model.BaseChatModel

    // 可选：最大迭代次数（默认：5）
    MaxIterations int

    // 可选：最大执行步骤（默认：MaxIterations * 4 + 10）
    MaxSteps int

    // 可选：自定义批判提示词
    CritiquePrompt string

    // 可选：Graph 名称（默认："ReCAP"）
    GraphName string
}
```

## 使用示例 (Usage Example)

### 基本用法

```go
// 1. 创建模型
chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
    APIKey: "your-api-key",
    Model:  "gpt-4",
})

// 2. 定义工具
tools := []tool.BaseTool{
    &CalculatorTool{},
    &SearchTool{},
}

// 3. 配置 ReCAP
config := &recap.AgentConfig{
    ToolCallingModel: chatModel,
    ToolsConfig: compose.ToolsNodeConfig{
        Tools: tools,
    },
    MaxIterations: 3,
}

// 4. 创建 Agent
agent, err := recap.NewAgent(ctx, config)

// 5. 使用 Agent
response, err := agent.Generate(ctx, []*schema.Message{
    schema.UserMessage("请帮我计算 123 * 456"),
})
```

### 流式输出

```go
stream, err := agent.Stream(ctx, []*schema.Message{
    schema.UserMessage("查询北京的天气并给出建议"),
})

for {
    msg, err := stream.Recv()
    if err == io.EOF {
        break
    }
    fmt.Print(msg.Content)
}
```

## 自定义批判逻辑 (Custom Critique Logic)

可以通过 `CritiquePrompt` 参数自定义批判逻辑：

```go
customCritiquePrompt := `
你是一个严格的质量评审员。评估工具执行结果：

之前的对话:
%s

工具执行结果:
%s

请评估:
1. 结果是否准确？
2. 是否完整回答了问题？
3. 是否需要更多信息？

必须以以下格式之一回复:
- "COMPLETE: <总结>" - 完全回答
- "CONTINUE: <缺少什么>" - 需要更多工作
- "ERROR: <问题>" - 出现错误
`

config := &recap.AgentConfig{
    ToolCallingModel: chatModel,
    ToolsConfig:      toolsConfig,
    CritiquePrompt:   customCritiquePrompt,
}
```

## 优势与限制 (Advantages and Limitations)

### 优势
1. **更高的准确性**: 通过批判步骤提高结果质量
2. **更好的错误处理**: 自动识别和纠正错误
3. **灵活的迭代控制**: 基于结果质量而非固定步数
4. **可观察性**: 完整的状态跟踪

### 限制
1. **更高的成本**: 需要额外的 LLM 调用（批判步骤）
2. **更长的延迟**: 增加了额外的处理步骤
3. **依赖批判质量**: 批判模型的质量影响整体表现

## 最佳实践 (Best Practices)

1. **选择合适的模型**
   - 推理：使用较强的模型（如 GPT-4）
   - 批判：可以使用相同或稍弱的模型

2. **设置合理的迭代次数**
   - 简单任务：2-3 次
   - 复杂任务：5-7 次
   - 避免过大的值以控制成本

3. **自定义批判提示词**
   - 根据具体任务调整批判标准
   - 明确定义"完成"的标准

4. **工具设计**
   - 保持工具的单一职责
   - 提供清晰的工具描述
   - 返回结构化的结果

5. **监控和调试**
   - 使用 eino 的 callbacks 机制监控执行
   - 记录批判结果以优化提示词

## 扩展性 (Extensibility)

ReCAP 可以通过以下方式扩展：

1. **自定义节点**: 添加额外的处理节点
2. **多模型策略**: 为不同步骤使用不同的模型
3. **外部评审**: 集成人工评审步骤
4. **结果缓存**: 缓存工具执行结果
5. **并行执行**: 支持并行工具调用

## 参考资料 (References)

- [eino 框架文档](https://github.com/cloudwego/eino)
- [ReAct 论文](https://arxiv.org/abs/2210.03629)
- [eino-ext 模型实现](https://github.com/cloudwego/eino-ext)
