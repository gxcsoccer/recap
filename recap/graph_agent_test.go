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
 * Tests for GraphAgent
 */

package recap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestNewGraphAgent(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ga, err := NewGraphAgent(ctx, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ga == nil {
				t.Fatal("GraphAgent should not be nil")
			}

			if !ga.compiled {
				t.Error("GraphAgent should be compiled")
			}

			if ga.agent == nil {
				t.Error("underlying agent should not be nil")
			}
		})
	}
}

func TestGraphAgent_GetAgent(t *testing.T) {
	ctx := context.Background()

	ga, err := NewGraphAgent(ctx, &AgentConfig{
		Model: &MockChatModel{},
	})
	if err != nil {
		t.Fatalf("failed to create GraphAgent: %v", err)
	}

	agent := ga.GetAgent()
	if agent == nil {
		t.Error("GetAgent should return non-nil agent")
	}
}

func TestGraphAgent_Run_SimplePrimitiveTasks(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			callCount++

			if callCount == 1 {
				plan := Plan{
					Thinking: "Search for the information",
					Subtasks: []Subtask{
						{
							Description: "Search for data",
							IsPrimitive: true,
							ToolName:    "search",
							ToolArgs:    map[string]any{"query": "graph test"},
						},
					},
				}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil
			}

			// Empty plan to signal completion
			plan := Plan{Thinking: "Done", Subtasks: []Subtask{}}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	searchTool := createMockSearchTool()

	ga, err := NewGraphAgent(ctx, &AgentConfig{
		Model: mockModel,
		Tools: []tool.BaseTool{searchTool},
	})
	if err != nil {
		t.Fatalf("failed to create GraphAgent: %v", err)
	}

	result, err := ga.Run(ctx, "Search for graph test data")
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

func TestGraphAgent_Run_MaxDepthLimit(t *testing.T) {
	ctx := context.Background()

	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			// Always return non-primitive to trigger recursion
			plan := Plan{
				Thinking: "Need to go deeper",
				Subtasks: []Subtask{
					{Description: "Non-primitive task", IsPrimitive: false},
				},
			}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	ga, err := NewGraphAgent(ctx, &AgentConfig{
		Model:    mockModel,
		MaxDepth: 2,
	})
	if err != nil {
		t.Fatalf("failed to create GraphAgent: %v", err)
	}

	result, err := ga.Run(ctx, "Deep recursive task")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !strings.Contains(result, "Maximum recursion depth") {
		t.Errorf("expected max depth error in result, got: %s", result)
	}
}

func TestGraphAgent_Run_Recursion(t *testing.T) {
	ctx := context.Background()

	callCount := 0
	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			callCount++

			switch callCount {
			case 1:
				// First: non-primitive task
				plan := Plan{
					Thinking: "Need to decompose",
					Subtasks: []Subtask{
						{Description: "Complex task", IsPrimitive: false},
					},
				}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil

			case 2:
				// Nested: primitive task
				plan := Plan{
					Thinking: "Can execute now",
					Subtasks: []Subtask{
						{Description: "Search", IsPrimitive: true, ToolName: "search", ToolArgs: map[string]any{"query": "nested"}},
					},
				}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil

			default:
				// Done
				plan := Plan{Thinking: "Completed", Subtasks: []Subtask{}}
				planJSON, _ := json.Marshal(plan)
				return schema.AssistantMessage(string(planJSON), nil), nil
			}
		},
	}

	searchTool := createMockSearchTool()

	ga, err := NewGraphAgent(ctx, &AgentConfig{
		Model:    mockModel,
		Tools:    []tool.BaseTool{searchTool},
		MaxDepth: 5,
	})
	if err != nil {
		t.Fatalf("failed to create GraphAgent: %v", err)
	}

	result, err := ga.Run(ctx, "Complex nested task")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result == "" {
		t.Error("result should not be empty")
	}

	if searchTool.CallCount == 0 {
		t.Error("search tool should have been called in nested level")
	}
}

func TestGraphAgentState_JSON(t *testing.T) {
	state := &GraphAgentState{
		Task:         "Test task",
		CurrentDepth: 2,
		MaxDepth:     5,
		Done:         false,
		CurrentPlan: &Plan{
			Thinking: "Test thinking",
			Subtasks: []Subtask{
				{Description: "Sub1", IsPrimitive: true, ToolName: "search"},
			},
		},
		Results: []*ExecutionResult{
			{Success: true, Output: "Result 1"},
		},
	}

	jsonStr := StateToJSON(state)

	if jsonStr == "" {
		t.Error("StateToJSON should return non-empty string")
	}

	if !strings.Contains(jsonStr, "Test task") {
		t.Error("JSON should contain task")
	}

	if !strings.Contains(jsonStr, "Test thinking") {
		t.Error("JSON should contain plan thinking")
	}
}

func TestGraphAgent_routeBranchCondition(t *testing.T) {
	ctx := context.Background()

	ga := &GraphAgent{
		agent: &Agent{
			config: &AgentConfig{},
		},
	}

	tests := []struct {
		name       string
		state      *GraphAgentState
		wantNode   string
		wantErrNil bool
	}{
		{
			name: "error state",
			state: &GraphAgentState{
				Error: "Some error",
			},
			wantNode:   NodeFinalize,
			wantErrNil: true,
		},
		{
			name: "done state",
			state: &GraphAgentState{
				Done: true,
			},
			wantNode:   NodeFinalize,
			wantErrNil: true,
		},
		{
			name: "max depth reached - error already set by checkLimitNode",
			state: &GraphAgentState{
				CurrentDepth: 5,
				MaxDepth:     5,
				Error:        "Maximum recursion depth (5) reached",
			},
			wantNode:   NodeFinalize,
			wantErrNil: true,
		},
		{
			name: "max steps exceeded - error already set by checkLimitNode",
			state: &GraphAgentState{
				CurrentDepth: 0,
				MaxDepth:     5,
				StepCount:    10,
				MaxSteps:     10,
				Error:        "Maximum steps (10) exceeded",
			},
			wantNode:   NodeFinalize,
			wantErrNil: true,
		},
		{
			name: "no plan - with recursion stack",
			state: &GraphAgentState{
				CurrentDepth:   1,
				MaxDepth:       5,
				StepCount:      0,
				MaxSteps:       10,
				CurrentPlan:    nil,
				RecursionStack: []*RecursionFrame{{Task: "parent"}},
			},
			wantNode:   NodePopFrame,
			wantErrNil: true,
		},
		{
			name: "no subtasks - empty stack",
			state: &GraphAgentState{
				CurrentDepth:   0,
				MaxDepth:       5,
				StepCount:      0,
				MaxSteps:       10,
				CurrentPlan:    &Plan{Subtasks: []Subtask{}},
				RecursionStack: []*RecursionFrame{},
			},
			wantNode:   NodeFinalize,
			wantErrNil: true,
		},
		{
			name: "primitive subtask",
			state: &GraphAgentState{
				CurrentDepth: 0,
				MaxDepth:     5,
				StepCount:    0,
				MaxSteps:     10,
				CurrentPlan: &Plan{
					Subtasks: []Subtask{
						{Description: "Primitive", IsPrimitive: true, ToolName: "search"},
					},
				},
			},
			wantNode:   NodeExecute,
			wantErrNil: true,
		},
		{
			name: "non-primitive subtask",
			state: &GraphAgentState{
				CurrentDepth: 0,
				MaxDepth:     5,
				StepCount:    0,
				MaxSteps:     10,
				CurrentPlan: &Plan{
					Subtasks: []Subtask{
						{Description: "Non-primitive", IsPrimitive: false},
					},
				},
			},
			wantNode:   NodePushFrame,
			wantErrNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ga.routeBranchCondition(ctx, tt.state)

			if tt.wantErrNil && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if node != tt.wantNode {
				t.Errorf("node = %s, want %s", node, tt.wantNode)
			}
		})
	}
}

func TestGraphAgent_checkLimitNode(t *testing.T) {
	ctx := context.Background()

	ga := &GraphAgent{
		agent: &Agent{
			config: &AgentConfig{},
		},
	}

	tests := []struct {
		name      string
		state     *GraphAgentState
		wantError string
	}{
		{
			name: "within limits",
			state: &GraphAgentState{
				CurrentDepth: 2,
				MaxDepth:     5,
				StepCount:    5,
				MaxSteps:     10,
			},
			wantError: "",
		},
		{
			name: "max depth reached",
			state: &GraphAgentState{
				CurrentDepth: 5,
				MaxDepth:     5,
				StepCount:    0,
				MaxSteps:     10,
			},
			wantError: "Maximum recursion depth (5) reached",
		},
		{
			name: "max depth exceeded",
			state: &GraphAgentState{
				CurrentDepth: 6,
				MaxDepth:     5,
				StepCount:    0,
				MaxSteps:     10,
			},
			wantError: "Maximum recursion depth (5) reached",
		},
		{
			name: "max steps reached",
			state: &GraphAgentState{
				CurrentDepth: 0,
				MaxDepth:     5,
				StepCount:    10,
				MaxSteps:     10,
			},
			wantError: "Maximum steps (10) exceeded",
		},
		{
			name: "max steps exceeded",
			state: &GraphAgentState{
				CurrentDepth: 0,
				MaxDepth:     5,
				StepCount:    15,
				MaxSteps:     10,
			},
			wantError: "Maximum steps (10) exceeded",
		},
		{
			name: "depth limit checked first - overwrites existing error",
			state: &GraphAgentState{
				CurrentDepth: 6,
				MaxDepth:     5,
				StepCount:    0,
				MaxSteps:     10,
				Error:        "Previous error",
			},
			wantError: "Maximum recursion depth (5) reached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ga.checkLimitNode(ctx, tt.state)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if result.Error != tt.wantError {
				t.Errorf("state.Error = %q, want %q", result.Error, tt.wantError)
			}
		})
	}
}

func TestGraphAgent_planNode(t *testing.T) {
	ctx := context.Background()

	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			plan := Plan{
				Thinking: "Generated plan",
				Subtasks: []Subtask{
					{Description: "Task 1", IsPrimitive: true, ToolName: "search"},
				},
			}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	agent := &Agent{
		config: &AgentConfig{
			Model:             mockModel,
			SlidingWindowSize: 64,
		},
		toolsMap:  make(map[string]tool.BaseTool),
		toolInfos: []*schema.ToolInfo{},
	}

	ga := &GraphAgent{agent: agent}

	state := &GraphAgentState{
		Task:         "Test task",
		Messages:     []*schema.Message{},
		CurrentDepth: 0,
	}

	resultState, err := ga.planNode(ctx, state)
	if err != nil {
		t.Fatalf("planNode failed: %v", err)
	}

	if resultState.CurrentPlan == nil {
		t.Fatal("plan should be set")
	}

	if resultState.CurrentPlan.Thinking != "Generated plan" {
		t.Error("plan thinking mismatch")
	}

	if len(resultState.CurrentPlan.Subtasks) != 1 {
		t.Errorf("expected 1 subtask, got %d", len(resultState.CurrentPlan.Subtasks))
	}
}

func TestGraphAgent_executeNode(t *testing.T) {
	ctx := context.Background()

	searchTool := createMockSearchTool()

	agent := &Agent{
		config: &AgentConfig{},
		toolsMap: map[string]tool.BaseTool{
			"search": searchTool,
		},
	}

	ga := &GraphAgent{agent: agent}

	state := &GraphAgentState{
		Task:         "Test",
		CurrentDepth: 0,
		Messages:     []*schema.Message{},
		CurrentPlan: &Plan{
			Subtasks: []Subtask{
				{Description: "Search", IsPrimitive: true, ToolName: "search", ToolArgs: map[string]any{"query": "test"}},
			},
		},
	}

	resultState, err := ga.executeNode(ctx, state)
	if err != nil {
		t.Fatalf("executeNode failed: %v", err)
	}

	if resultState.StepCount != 1 {
		t.Errorf("step count = %d, want 1", resultState.StepCount)
	}

	if len(resultState.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resultState.Results))
	}

	if !resultState.Results[0].Success {
		t.Error("execution should have succeeded")
	}

	if searchTool.CallCount != 1 {
		t.Errorf("tool call count = %d, want 1", searchTool.CallCount)
	}
}

func TestGraphAgent_pushPopFrame(t *testing.T) {
	ctx := context.Background()

	ga := &GraphAgent{
		agent: &Agent{config: &AgentConfig{}},
	}

	// Test pushFrameNode
	state := &GraphAgentState{
		Task:         "Root task",
		CurrentDepth: 0,
		Messages:     []*schema.Message{},
		CurrentPlan: &Plan{
			Thinking: "Root thinking",
			Subtasks: []Subtask{
				{Description: "Complex", IsPrimitive: false},
				{Description: "Next", IsPrimitive: true, ToolName: "search"},
			},
		},
		RecursionStack: []*RecursionFrame{},
	}

	pushedState, err := ga.pushFrameNode(ctx, state)
	if err != nil {
		t.Fatalf("pushFrameNode failed: %v", err)
	}

	if pushedState.CurrentDepth != 1 {
		t.Errorf("depth after push = %d, want 1", pushedState.CurrentDepth)
	}

	if len(pushedState.RecursionStack) != 1 {
		t.Fatalf("stack size = %d, want 1", len(pushedState.RecursionStack))
	}

	if pushedState.Task != "Complex" {
		t.Errorf("task = %s, want Complex", pushedState.Task)
	}

	if pushedState.CurrentPlan != nil {
		t.Error("plan should be nil after push (will be regenerated)")
	}

	// Test popFrameNode
	pushedState.Results = []*ExecutionResult{
		{Success: true, Output: "Nested result"},
	}

	poppedState, err := ga.popFrameNode(ctx, pushedState)
	if err != nil {
		t.Fatalf("popFrameNode failed: %v", err)
	}

	if poppedState.CurrentDepth != 0 {
		t.Errorf("depth after pop = %d, want 0", poppedState.CurrentDepth)
	}

	if poppedState.Task != "Root task" {
		t.Errorf("task = %s, want Root task", poppedState.Task)
	}

	if len(poppedState.RecursionStack) != 0 {
		t.Errorf("stack size after pop = %d, want 0", len(poppedState.RecursionStack))
	}
}

func TestGraphAgent_finalizeNode(t *testing.T) {
	ctx := context.Background()

	ga := &GraphAgent{
		agent: &Agent{config: &AgentConfig{}},
	}

	tests := []struct {
		name           string
		state          *GraphAgentState
		wantDone       bool
		wantOutputType string // "error", "result", or "empty"
	}{
		{
			name: "with error",
			state: &GraphAgentState{
				Error: "Something went wrong",
			},
			wantDone:       true,
			wantOutputType: "error",
		},
		{
			name: "with results",
			state: &GraphAgentState{
				Results: []*ExecutionResult{
					{Success: true, Output: "First result"},
					{Success: true, Output: "Last result"},
				},
			},
			wantDone:       true,
			wantOutputType: "result",
		},
		{
			name: "no results",
			state: &GraphAgentState{
				Results: []*ExecutionResult{},
			},
			wantDone:       true,
			wantOutputType: "empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultState, err := ga.finalizeNode(ctx, tt.state)
			if err != nil {
				t.Fatalf("finalizeNode failed: %v", err)
			}

			if !resultState.Done {
				t.Error("state should be done after finalize")
			}

			switch tt.wantOutputType {
			case "error":
				if !strings.Contains(resultState.FinalOutput, "Error") {
					t.Error("output should contain error")
				}
			case "result":
				if !strings.Contains(resultState.FinalOutput, "Last result") {
					t.Error("output should contain last result")
				}
			case "empty":
				if !strings.Contains(resultState.FinalOutput, "no execution") {
					t.Error("output should indicate no execution")
				}
			}
		})
	}
}
