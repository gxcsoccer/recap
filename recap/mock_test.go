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
 * Mock implementations for testing
 */

package recap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// MockChatModel is a mock implementation of model.ChatModel for testing
type MockChatModel struct {
	// GenerateFunc allows customizing the Generate behavior
	GenerateFunc func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error)

	// StreamFunc allows customizing the Stream behavior
	StreamFunc func(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error)

	// Responses is a queue of responses to return
	Responses []*schema.Message

	// CallCount tracks how many times Generate was called
	CallCount int

	// LastMessages stores the last messages passed to Generate
	LastMessages []*schema.Message
}

// Generate implements model.ChatModel
func (m *MockChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.CallCount++
	m.LastMessages = messages

	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, messages, opts...)
	}

	if len(m.Responses) > 0 {
		resp := m.Responses[0]
		m.Responses = m.Responses[1:]
		return resp, nil
	}

	return schema.AssistantMessage("Default mock response", nil), nil
}

// Stream implements model.ChatModel
func (m *MockChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.StreamFunc != nil {
		return m.StreamFunc(ctx, messages, opts...)
	}
	return nil, fmt.Errorf("stream not implemented in mock")
}

// BindTools implements model.ChatModel (no-op for mock)
func (m *MockChatModel) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

// MockTool is a mock implementation of tool.InvokableTool for testing
type MockTool struct {
	// ToolInfo is the tool information
	ToolInfo *schema.ToolInfo

	// InvokeFunc allows customizing the InvokableRun behavior
	InvokeFunc func(ctx context.Context, args string) (string, error)

	// CallCount tracks how many times InvokableRun was called
	CallCount int

	// LastArgs stores the last arguments passed
	LastArgs string
}

// Info implements tool.BaseTool
func (m *MockTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return m.ToolInfo, nil
}

// InvokableRun implements tool.InvokableTool
func (m *MockTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	m.CallCount++
	m.LastArgs = args

	if m.InvokeFunc != nil {
		return m.InvokeFunc(ctx, args)
	}

	return fmt.Sprintf("Mock result for tool %s", m.ToolInfo.Name), nil
}

// Ensure MockTool implements tool.InvokableTool
var _ tool.InvokableTool = (*MockTool)(nil)

// Helper functions to create test fixtures

// createMockPlanResponse creates a mock response with a valid plan JSON
func createMockPlanResponse(thinking string, subtasks []Subtask) *schema.Message {
	plan := Plan{
		Thinking: thinking,
		Subtasks: subtasks,
	}
	planJSON, _ := json.Marshal(plan)
	return schema.AssistantMessage(string(planJSON), nil)
}

// createSimplePrimitiveSubtask creates a primitive subtask for testing
func createSimplePrimitiveSubtask(desc, toolName string, args map[string]any) Subtask {
	return Subtask{
		Description: desc,
		IsPrimitive: true,
		ToolName:    toolName,
		ToolArgs:    args,
	}
}

// createNonPrimitiveSubtask creates a non-primitive subtask for testing
func createNonPrimitiveSubtask(desc string) Subtask {
	return Subtask{
		Description: desc,
		IsPrimitive: false,
	}
}

// createMockSearchTool creates a mock search tool
func createMockSearchTool() *MockTool {
	return &MockTool{
		ToolInfo: &schema.ToolInfo{
			Name: "search",
			Desc: "Search for information",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"query": {
					Type:     "string",
					Desc:     "Search query",
					Required: true,
				},
			}),
		},
		InvokeFunc: func(ctx context.Context, args string) (string, error) {
			var input struct {
				Query string `json:"query"`
			}
			json.Unmarshal([]byte(args), &input)
			return fmt.Sprintf("Search results for: %s", input.Query), nil
		},
	}
}

// createMockCalculateTool creates a mock calculate tool
func createMockCalculateTool() *MockTool {
	return &MockTool{
		ToolInfo: &schema.ToolInfo{
			Name: "calculate",
			Desc: "Perform calculations",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"expression": {
					Type:     "string",
					Desc:     "Math expression",
					Required: true,
				},
			}),
		},
		InvokeFunc: func(ctx context.Context, args string) (string, error) {
			return "42", nil
		},
	}
}

// MockStreamReader implements a simple stream reader for testing
type MockStreamReader struct {
	messages []*schema.Message
	index    int
}

func (r *MockStreamReader) Recv() (*schema.Message, error) {
	if r.index >= len(r.messages) {
		return nil, io.EOF
	}
	msg := r.messages[r.index]
	r.index++
	return msg, nil
}

func (r *MockStreamReader) Close() {}
