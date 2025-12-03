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
 * Tests for planner functions
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

func TestAgent_generatePlan(t *testing.T) {
	ctx := context.Background()

	// Create mock model that returns a valid plan
	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			plan := Plan{
				Thinking: "I need to search for information first, then calculate the result",
				Subtasks: []Subtask{
					{
						Description: "Search for data",
						IsPrimitive: true,
						ToolName:    "search",
						ToolArgs:    map[string]any{"query": "test data"},
					},
					{
						Description: "Calculate result",
						IsPrimitive: true,
						ToolName:    "calculate",
						ToolArgs:    map[string]any{"expression": "2+2"},
					},
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

	messages := []*schema.Message{
		schema.UserMessage("Do something complex"),
	}

	plan, err := agent.generatePlan(ctx, "Do something complex", messages)
	if err != nil {
		t.Fatalf("generatePlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("generatePlan returned nil plan")
	}

	if plan.Thinking == "" {
		t.Error("plan thinking should not be empty")
	}

	if len(plan.Subtasks) != 2 {
		t.Errorf("expected 2 subtasks, got %d", len(plan.Subtasks))
	}

	if !plan.Subtasks[0].IsPrimitive {
		t.Error("first subtask should be primitive")
	}

	if plan.Subtasks[0].ToolName != "search" {
		t.Errorf("first subtask tool = %s, want search", plan.Subtasks[0].ToolName)
	}
}

func TestAgent_generatePlan_WithJSONBlock(t *testing.T) {
	ctx := context.Background()

	// Create mock model that returns JSON in markdown code block
	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			response := "Here's my plan:\n```json\n" + `{
				"thinking": "First search, then calculate",
				"subtasks": [
					{"description": "Search", "is_primitive": true, "tool_name": "search", "tool_args": {"query": "test"}}
				]
			}` + "\n```\nThat's the plan."
			return schema.AssistantMessage(response, nil), nil
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

	plan, err := agent.generatePlan(ctx, "Task", []*schema.Message{})
	if err != nil {
		t.Fatalf("generatePlan failed: %v", err)
	}

	if len(plan.Subtasks) != 1 {
		t.Errorf("expected 1 subtask, got %d", len(plan.Subtasks))
	}
}

func TestAgent_generatePlan_InvalidJSON(t *testing.T) {
	ctx := context.Background()

	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return schema.AssistantMessage("This is not valid JSON at all", nil), nil
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

	_, err := agent.generatePlan(ctx, "Task", []*schema.Message{})
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

func TestAgent_refinePlan(t *testing.T) {
	ctx := context.Background()

	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			plan := Plan{
				Thinking: "Based on the search result, I now need to calculate",
				Subtasks: []Subtask{
					{
						Description: "Calculate final result",
						IsPrimitive: true,
						ToolName:    "calculate",
						ToolArgs:    map[string]any{"expression": "10*5"},
					},
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

	currentPlan := &Plan{
		Thinking: "Original thinking",
		Subtasks: []Subtask{
			{Description: "Search", IsPrimitive: true, ToolName: "search"},
			{Description: "Calculate", IsPrimitive: true, ToolName: "calculate"},
		},
	}

	completedSubtask := &Subtask{
		Description: "Search",
		IsPrimitive: true,
		ToolName:    "search",
	}

	result := &ExecutionResult{
		Success:            true,
		Output:             "Found: value = 50",
		SubtaskDescription: "Search",
	}

	refinedPlan, err := agent.refinePlan(ctx, "Main task", currentPlan, completedSubtask, result, []*schema.Message{})
	if err != nil {
		t.Fatalf("refinePlan failed: %v", err)
	}

	if refinedPlan == nil {
		t.Fatal("refinePlan returned nil")
	}

	if refinedPlan.Thinking == currentPlan.Thinking {
		t.Error("refined plan thinking should be different from original")
	}

	if len(refinedPlan.Subtasks) != 1 {
		t.Errorf("expected 1 subtask after refinement, got %d", len(refinedPlan.Subtasks))
	}
}

func TestAgent_parsePlanResponse(t *testing.T) {
	agent := &Agent{
		config: &AgentConfig{},
	}

	tests := []struct {
		name        string
		content     string
		wantErr     bool
		wantTasks   int
		wantThink   string
	}{
		{
			name: "simple JSON",
			content: `{
				"thinking": "Simple plan",
				"subtasks": [
					{"description": "Task 1", "is_primitive": true, "tool_name": "search"}
				]
			}`,
			wantErr:   false,
			wantTasks: 1,
			wantThink: "Simple plan",
		},
		{
			name: "JSON in markdown block",
			content: "Here's the plan:\n```json\n" + `{
				"thinking": "Block plan",
				"subtasks": []
			}` + "\n```",
			wantErr:   false,
			wantTasks: 0,
			wantThink: "Block plan",
		},
		{
			name: "JSON with surrounding text",
			content: `Let me think about this.
			{
				"thinking": "Embedded plan",
				"subtasks": [{"description": "A", "is_primitive": false}]
			}
			That's my plan.`,
			wantErr:   false,
			wantTasks: 1,
			wantThink: "Embedded plan",
		},
		{
			name:    "no JSON",
			content: "This is just plain text without any JSON",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			content: `{"thinking": "broken, "subtasks": []}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := agent.parsePlanResponse(tt.content)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(plan.Subtasks) != tt.wantTasks {
				t.Errorf("subtask count = %d, want %d", len(plan.Subtasks), tt.wantTasks)
			}

			if plan.Thinking != tt.wantThink {
				t.Errorf("thinking = %s, want %s", plan.Thinking, tt.wantThink)
			}
		})
	}
}

func TestAgent_formatToolDescriptions(t *testing.T) {
	ctx := context.Background()

	searchTool := createMockSearchTool()
	calcTool := createMockCalculateTool()

	agent := &Agent{
		config: &AgentConfig{},
		toolsMap: map[string]tool.BaseTool{
			"search":    searchTool,
			"calculate": calcTool,
		},
		toolInfos: []*schema.ToolInfo{},
	}

	// Get tool infos
	searchInfo, _ := searchTool.Info(ctx)
	calcInfo, _ := calcTool.Info(ctx)
	agent.toolInfos = []*schema.ToolInfo{searchInfo, calcInfo}

	desc := agent.formatToolDescriptions()

	if !strings.Contains(desc, "search") {
		t.Error("description should contain search tool")
	}

	if !strings.Contains(desc, "calculate") {
		t.Error("description should contain calculate tool")
	}

	if !strings.Contains(desc, "Search for information") {
		t.Error("description should contain search tool description")
	}
}

func TestAgent_generatePlan_SlidingWindow(t *testing.T) {
	ctx := context.Background()

	var receivedMessages []*schema.Message

	mockModel := &MockChatModel{
		GenerateFunc: func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			receivedMessages = messages
			plan := Plan{
				Thinking: "Plan",
				Subtasks: []Subtask{{Description: "Task", IsPrimitive: true, ToolName: "search"}},
			}
			planJSON, _ := json.Marshal(plan)
			return schema.AssistantMessage(string(planJSON), nil), nil
		},
	}

	windowSize := 5
	agent := &Agent{
		config: &AgentConfig{
			Model:             mockModel,
			SlidingWindowSize: windowSize,
		},
		toolsMap:  make(map[string]tool.BaseTool),
		toolInfos: []*schema.ToolInfo{},
	}

	// Create more messages than window size
	messages := make([]*schema.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = schema.UserMessage("Message " + string(rune('A'+i)))
	}

	_, err := agent.generatePlan(ctx, "Task", messages)
	if err != nil {
		t.Fatalf("generatePlan failed: %v", err)
	}

	// Should have: system message + windowSize messages + task message
	// The window should contain the last 'windowSize' messages from the input
	if receivedMessages == nil {
		t.Fatal("model should have received messages")
	}

	// Check that recent messages are included
	found := false
	for _, msg := range receivedMessages {
		if strings.Contains(msg.Content, "Message J") { // Last message (index 9 = 'J')
			found = true
			break
		}
	}
	if !found {
		t.Error("recent messages should be included in sliding window")
	}
}
