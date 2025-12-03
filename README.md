# ReCAP Agent Implementation

基于 [eino](https://github.com/cloudwego/eino) 框架实现的 [ReCAP](https://arxiv.org/abs/2510.23822) (Recursive Context-Aware Reasoning and Planning) Agent。

## 概述

ReCAP 是一个为 LLM Agent 设计的分层框架，解决长期任务中的关键问题：
- 长期任务需要多步推理和动态重新规划
- 顺序提示容易导致上下文漂移和目标信息丧失

### 核心机制

1. **Plan-ahead task decomposition (前向规划分解)**
   - 一次性生成完整的子任务列表
   - 执行第一项任务
   - 完成后精化剩余计划

2. **Structured re-injection of parent plans (父计划的结构化重注入)**
   - 在递归返回过程中维持一致的多层级上下文
   - 确保不同层级之间的连续性

3. **Memory-efficient execution (内存高效执行)**
   - 使用滑动窗口限制活跃提示规模
   - 成本按任务深度线性增长

## 项目结构

```
recap/
├── recap/                    # 核心包
│   ├── types.go              # 类型定义
│   ├── agent.go              # 直接实现的 ReCAP Agent
│   ├── graph_agent.go        # 基于 eino Graph 编排的实现
│   ├── planner.go            # 计划生成和精化
│   └── context.go            # 上下文管理
├── example/
│   └── main.go               # 使用示例
├── go.mod
└── README.md
```

## 两种实现

### 1. 直接实现 (`Agent`)

传统的递归函数实现，更直观：

```go
agent, err := recap.NewAgent(ctx, &recap.AgentConfig{
    Model:             chatModel,
    Tools:             tools,
    MaxDepth:          3,
    MaxStepsPerLevel:  10,
    SlidingWindowSize: 64,
})

result, err := agent.Run(ctx, "Your task description")
```

### 2. Graph 编排实现 (`GraphAgent`)

使用 eino 的 `compose.Graph` 编排，更符合 eino 框架风格：

```go
graphAgent, err := recap.NewGraphAgent(ctx, &recap.AgentConfig{
    Model:             chatModel,
    Tools:             tools,
    MaxDepth:          3,
    MaxStepsPerLevel:  10,
})

result, err := graphAgent.Run(ctx, "Your task description")
```

## 算法流程

```
Algorithm: ReCAP(C, task)

Input: LLM context C, task description
Output: Execution result

T, S ← π(C, task)           # 生成初始计划 (thinking, subtasks)

while S ≠ ∅ do
    if S[0] is primitive then
        result ← execute(S[0])          # 执行原始任务
    else
        result ← ReCAP(C ∥ ⟨T, S, S[0]⟩, S[0])  # 递归调用
    end
    T, S ← ρ(C, T, S, result)           # 精化计划
end

return C ∥ ⟨T, S[1:]⟩                    # 返回剩余上下文
```

## 使用方法

### 安装依赖

```bash
go mod tidy
```

### 环境变量

```bash
export OPENAI_API_KEY="your-api-key"
```

### 运行示例

```bash
# 使用直接实现
go run example/main.go

# 使用 Graph 编排实现
go run example/main.go --graph
```

## 配置选项

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `Model` | ChatModel 实例 | (必需) |
| `Tools` | 可用工具列表 | (必需) |
| `MaxDepth` | 最大递归深度 | 5 |
| `MaxStepsPerLevel` | 每层最大执行步数 | 10 |
| `SlidingWindowSize` | 滑动窗口大小 | 64 |
| `SystemPrompt` | 系统提示词 | 默认提示 |
| `PlanPrompt` | 计划生成提示词 | 默认提示 |
| `RefinePrompt` | 计划精化提示词 | 默认提示 |
| `Debug` | 启用调试日志 | false |

## 回调机制

直接实现支持回调来监控执行过程：

```go
type Callback interface {
    OnPlanGenerated(ctx context.Context, depth int, plan *Plan)
    OnSubtaskStart(ctx context.Context, depth int, subtask *Subtask)
    OnSubtaskEnd(ctx context.Context, depth int, subtask *Subtask, result *ExecutionResult)
    OnRecursionEnter(ctx context.Context, depth int, subtask *Subtask)
    OnRecursionExit(ctx context.Context, depth int, result *ExecutionResult)
    OnPlanRefined(ctx context.Context, depth int, oldPlan, newPlan *Plan)
}
```

## 自定义工具

使用 eino 的工具 API 定义工具：

```go
tool := utils.NewTool(
    &schema.ToolInfo{
        Name: "my_tool",
        Desc: "Tool description",
        ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
            "param1": {
                Type:     "string",
                Desc:     "Parameter description",
                Required: true,
            },
        }),
    },
    func(ctx context.Context, input *MyInput) (*MyOutput, error) {
        // Tool implementation
        return &MyOutput{...}, nil
    },
)
```

## 参考

- [ReCAP 论文](https://arxiv.org/abs/2510.23822) - Recursive Context-Aware Reasoning and Planning for LLM Agents
- [eino 框架](https://github.com/cloudwego/eino) - CloudWeGo LLM 应用开发框架
- [eino 文档](https://www.cloudwego.io/docs/eino/) - 官方文档

## License

Apache License 2.0
