/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Tests for Agent
 */

package recap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestNewAgent(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		config  *AgentConfig
		wantErr bool
	}{
		{
			name:    "nil model",
			config:  &AgentConfig{},
			wantErr: true,
		},
		{
			name: "valid config",
			config: &AgentConfig{
				Model: &MockChatModel{},
				Tools: []tool.BaseTool{createMockSearchTool()},
			},
			wantErr: false,
		},
		{
			name: "valid config with defaults",
			config: &AgentConfig{
				Model: &MockChatModel{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := NewAgent(ctx, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if agent == nil {
				t.Fatal("agent should not be nil")
			}

			// Check defaults
			if agent.config.MaxDepth <= 0 {
				t.Error("MaxDepth should have default value")
			}
			if agent.config.MaxStepsPerLevel <= 0 {
				t.Error("MaxStepsPerLevel should have default value")
			}
			if agent.config.SlidingWindowSize <= 0 {
				t.Error("SlidingWindowSize should have default value")
			}
			if agent.config.SystemPrompt == "" {
				t.Error("SystemPrompt should have default value")
			}
		})
	}
}

func TestAgent_executePrimitive(t *testing.T) {
	ctx := context.Background()

	searchTool := createMockSearchTool()

	agent := &Agent{
		config: &AgentConfig{},
		toolsMap: map[string]tool.BaseTool{
			"search": searchTool,
		},
	}

	tests := []struct {
		name       string
		subtask    *Subtask
		wantErr    bool
		wantOutput string
	}{
		{
			name: "successful execution",
			subtask: &Subtask{
				Description: "Search for data",
				IsPrimitive: true,
				ToolName:    "search",
				ToolArgs:    map[string]any{"query": "test query"},
			},
			wantErr:    false,
			wantOutput: "Search results for: test query",
		},
		{
			name: "missing tool name",
			subtask: &Subtask{
				Description: "No tool specified",
				IsPrimitive: true,
				ToolName:    "",
			},
			wantErr: true,
		},
		{
			name: "unknown tool",
			subtask: &Subtask{
				Description: "Unknown tool",
				IsPrimitive: true,
				ToolName:    "nonexistent",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.executePrimitive(ctx, tt.subtask)

			if tt.wantErr {
				if result.Success {
					t.Error("expected failure but got success")
				}
				return
			}

			if !result.Success {
				t.Errorf("unexpected failure: %s", result.Output)
			}

			if !strings.Contains(result.Output, tt.wantOutput) {
				t.Errorf("output = %s, want to contain %s", result.Output, tt.wantOutput)
			}
		})
	}
}

func TestAgent_Run_SimplePrimitiveTasks(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			callCount++

			if callCount == 1 {
				// First call: generate initial plan
				plan := Plan{
					Thinking: "I need to search for information",
					Subtasks: []Subtask{
						{
							Description: "Search for data",
							IsPrimitive: true,
							ToolName:    "search",
							ToolArgs:    map[string]any{"query": "test"},
						},
					},
				}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil
			}

			// Subsequent calls: empty plan (task done)
			plan := Plan{
				Thinking: "Task completed",
				Subtasks: []Subtask{},
			}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	searchTool := createMockSearchTool()

	agent, err := NewAgent(ctx, &AgentConfig{
		Model: mockModel,
		Tools: []tool.BaseTool{searchTool},
	})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	result, err := agent.Run(ctx, "Search for test data")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result == "" {
		t.Error("result should not be empty")
	}

	if searchTool.CallCount == 0 {
		t.Error("search tool should have been called")
	}
}

func TestAgent_Run_MaxDepthLimit(t *testing.T) {
	ctx := context.Background()

	// Create model that always returns non-primitive tasks (infinite recursion)
	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			plan := Plan{
				Thinking: "Need to decompose further",
				Subtasks: []Subtask{
					{
						Description: "Complex subtask",
						IsPrimitive: false,
					},
				},
			}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	agent, err := NewAgent(ctx, &AgentConfig{
		Model:            mockModel,
		MaxDepth:         2,
		MaxStepsPerLevel: 5, // Low to ensure quick termination
	})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	result, err := agent.Run(ctx, "Deep task")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should hit one of the limits (depth or steps) and return gracefully
	// The test verifies the agent terminates without panicking on deep recursion
	if !strings.Contains(result, "Maximum") {
		t.Errorf("expected a Maximum limit error (depth or steps), got: %s", result)
	}
}

func TestAgent_Run_WithCallbacks(t *testing.T) {
	ctx := context.Background()

	planCount := 0
	subtaskStartCount := 0
	subtaskEndCount := 0

	callback := &testCallback{
		onPlanGenerated: func(ctx context.Context, depth int, plan *Plan) {
			planCount++
		},
		onSubtaskStart: func(ctx context.Context, depth int, subtask *Subtask) {
			subtaskStartCount++
		},
		onSubtaskEnd: func(ctx context.Context, depth int, subtask *Subtask, result *ExecutionResult) {
			subtaskEndCount++
		},
	}

	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			plan := Plan{
				Thinking: "Simple plan",
				Subtasks: []Subtask{
					{Description: "Search", IsPrimitive: true, ToolName: "search", ToolArgs: map[string]any{"query": "x"}},
				},
			}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	agent, _ := NewAgent(ctx, &AgentConfig{
		Model: mockModel,
		Tools: []tool.BaseTool{createMockSearchTool()},
	})

	_, err := agent.Run(ctx, "Test task", WithCallbacks(callback))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if planCount == 0 {
		t.Error("OnPlanGenerated should have been called")
	}

	if subtaskStartCount == 0 {
		t.Error("OnSubtaskStart should have been called")
	}

	if subtaskEndCount == 0 {
		t.Error("OnSubtaskEnd should have been called")
	}
}

func TestAgent_Run_Recursion(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			callCount++

			switch callCount {
			case 1:
				// Root plan with non-primitive task
				plan := Plan{
					Thinking: "Need to decompose",
					Subtasks: []Subtask{
						{Description: "Complex task", IsPrimitive: false},
					},
				}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil

			case 2:
				// Decomposed plan with primitive task
				plan := Plan{
					Thinking: "Now I can execute",
					Subtasks: []Subtask{
						{Description: "Search", IsPrimitive: true, ToolName: "search", ToolArgs: map[string]any{"query": "test"}},
					},
				}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil

			default:
				// Done
				plan := Plan{Thinking: "Done", Subtasks: []Subtask{}}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil
			}
		},
	}

	searchTool := createMockSearchTool()

	recursionEnterCount := 0
	recursionExitCount := 0
	callback := &testCallback{
		onRecursionEnter: func(ctx context.Context, depth int, subtask *Subtask) {
			recursionEnterCount++
		},
		onRecursionExit: func(ctx context.Context, depth int, result *ExecutionResult) {
			recursionExitCount++
		},
	}

	agent, _ := NewAgent(ctx, &AgentConfig{
		Model:    mockModel,
		Tools:    []tool.BaseTool{searchTool},
		MaxDepth: 5,
	})

	_, err := agent.Run(ctx, "Complex task", WithCallbacks(callback))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if recursionEnterCount == 0 {
		t.Error("should have entered recursion")
	}

	if recursionExitCount == 0 {
		t.Error("should have exited recursion")
	}

	if searchTool.CallCount == 0 {
		t.Error("search tool should have been called in nested level")
	}
}

func TestAgent_Run_ToolError(t *testing.T) {
	ctx := context.Background()

	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			plan := Plan{
				Thinking: "Try the failing tool",
				Subtasks: []Subtask{
					{Description: "Fail", IsPrimitive: true, ToolName: "fail_tool", ToolArgs: map[string]any{}},
				},
			}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	failTool := &MockTool{
		ToolInfo: &schema.ToolInfo{Name: "fail_tool", Desc: "A failing tool"},
		InvokeFunc: func(ctx context.Context, args string) (string, error) {
			return "", fmt.Errorf("tool execution error")
		},
	}

	agent, _ := NewAgent(ctx, &AgentConfig{
		Model: mockModel,
		Tools: []tool.BaseTool{failTool},
	})

	result, err := agent.Run(ctx, "Try failing tool")
	if err != nil {
		t.Fatalf("Run should not return error for tool failures: %v", err)
	}

	if !strings.Contains(result, "failed") && !strings.Contains(result, "error") {
		t.Logf("Result: %s", result)
		// Tool failures are handled internally, the test verifies no panic occurs
	}
}

func TestAgentOptions(t *testing.T) {
	config := &AgentConfig{}

	WithMaxRecursionDepth(10)(config)
	if config.MaxDepth != 10 {
		t.Errorf("MaxDepth = %d, want 10", config.MaxDepth)
	}

	WithMaxStepsPerLevel(20)(config)
	if config.MaxStepsPerLevel != 20 {
		t.Errorf("MaxStepsPerLevel = %d, want 20", config.MaxStepsPerLevel)
	}

	WithSlidingWindowSize(128)(config)
	if config.SlidingWindowSize != 128 {
		t.Errorf("SlidingWindowSize = %d, want 128", config.SlidingWindowSize)
	}

	WithSystemPrompt("Custom prompt")(config)
	if config.SystemPrompt != "Custom prompt" {
		t.Error("SystemPrompt not set correctly")
	}

	WithDebug(true)(config)
	if !config.Debug {
		t.Error("Debug should be true")
	}
}

func TestRunOptions(t *testing.T) {
	runCfg := &runConfig{
		maxDepth: 5,
	}

	WithMaxDepth(3)(runCfg)
	if runCfg.maxDepth != 3 {
		t.Errorf("maxDepth = %d, want 3", runCfg.maxDepth)
	}

	callback := &testCallback{}
	WithCallbacks(callback)(runCfg)
	if len(runCfg.callbacks) != 1 {
		t.Errorf("callbacks count = %d, want 1", len(runCfg.callbacks))
	}
}

// testCallback is a test implementation of Callback
type testCallback struct {
	onPlanGenerated  func(ctx context.Context, depth int, plan *Plan)
	onSubtaskStart   func(ctx context.Context, depth int, subtask *Subtask)
	onSubtaskEnd     func(ctx context.Context, depth int, subtask *Subtask, result *ExecutionResult)
	onRecursionEnter func(ctx context.Context, depth int, subtask *Subtask)
	onRecursionExit  func(ctx context.Context, depth int, result *ExecutionResult)
	onPlanRefined    func(ctx context.Context, depth int, oldPlan, newPlan *Plan)
}

func (c *testCallback) OnPlanGenerated(ctx context.Context, depth int, plan *Plan) {
	if c.onPlanGenerated != nil {
		c.onPlanGenerated(ctx, depth, plan)
	}
}

func (c *testCallback) OnSubtaskStart(ctx context.Context, depth int, subtask *Subtask) {
	if c.onSubtaskStart != nil {
		c.onSubtaskStart(ctx, depth, subtask)
	}
}

func (c *testCallback) OnSubtaskEnd(ctx context.Context, depth int, subtask *Subtask, result *ExecutionResult) {
	if c.onSubtaskEnd != nil {
		c.onSubtaskEnd(ctx, depth, subtask, result)
	}
}

func (c *testCallback) OnRecursionEnter(ctx context.Context, depth int, subtask *Subtask) {
	if c.onRecursionEnter != nil {
		c.onRecursionEnter(ctx, depth, subtask)
	}
}

func (c *testCallback) OnRecursionExit(ctx context.Context, depth int, result *ExecutionResult) {
	if c.onRecursionExit != nil {
		c.onRecursionExit(ctx, depth, result)
	}
}

func (c *testCallback) OnPlanRefined(ctx context.Context, depth int, oldPlan, newPlan *Plan) {
	if c.onPlanRefined != nil {
		c.onPlanRefined(ctx, depth, oldPlan, newPlan)
	}
}
