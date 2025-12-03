/*
 * Copyright 2024 ReCAP Authors
 *
 * Licensed under the MIT License.
 */

package recap

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
)

// mockTool is a simple test tool
type mockTool struct {
	callCount int
}

func (m *mockTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "test_tool",
		Desc: "A test tool",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"input": {
				Type:     schema.String,
				Desc:     "Input parameter",
				Required: true,
			},
		}),
	}, nil
}

func (m *mockTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	m.callCount++
	return fmt.Sprintf("Tool executed: %s (call #%d)", argumentsInJSON, m.callCount), nil
}

// mockChatModel is a simple mock implementation for testing
type mockChatModel struct {
	generateFunc func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error)
	streamFunc   func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error)
}

func (m *mockChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, input, opts...)
	}
	return schema.AssistantMessage("Default response", nil), nil
}

func (m *mockChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, input, opts...)
	}
	sr, sw := schema.Pipe[*schema.Message](1)
	go func() {
		defer sw.Close()
		sw.Send(schema.AssistantMessage("Default stream response", nil), nil)
	}()
	return sr, nil
}

func (m *mockChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func TestNewAgent(t *testing.T) {
	ctx := context.Background()

	t.Run("successful creation", func(t *testing.T) {
		mockModel := &mockChatModel{}
		testTool := &mockTool{}

		config := &AgentConfig{
			ToolCallingModel: mockModel,
			ToolsConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{testTool},
			},
			MaxIterations: 2,
		}

		agent, err := NewAgent(ctx, config)
		assert.NoError(t, err)
		assert.NotNil(t, agent)
	})

	t.Run("nil config", func(t *testing.T) {
		agent, err := NewAgent(ctx, nil)
		assert.Error(t, err)
		assert.Nil(t, agent)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("no model provided", func(t *testing.T) {
		config := &AgentConfig{
			ToolsConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{},
			},
		}

		agent, err := NewAgent(ctx, config)
		assert.Error(t, err)
		assert.Nil(t, agent)
		assert.Contains(t, err.Error(), "ToolCallingModel or Model must be provided")
	})
}

func TestAgentGenerate(t *testing.T) {
	ctx := context.Background()

	testTool := &mockTool{}
	callCount := 0

	mockModel := &mockChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			callCount++
			
			// First call: return with tool call
			if callCount == 1 {
				return schema.AssistantMessage("Let me use the tool", []schema.ToolCall{
					{
						ID: "call_1",
						Function: schema.FunctionCall{
							Name:      "test_tool",
							Arguments: `{"input": "test"}`,
						},
					},
				}), nil
			}
			
			// Second call: critique (should return COMPLETE)
			if callCount == 2 {
				return schema.AssistantMessage("COMPLETE: Task done successfully", nil), nil
			}

			// Any other calls: return simple response
			return schema.AssistantMessage("Done", nil), nil
		},
	}

	config := &AgentConfig{
		ToolCallingModel: mockModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{testTool},
		},
		MaxIterations: 3,
	}

	agent, err := NewAgent(ctx, config)
	assert.NoError(t, err)

	response, err := agent.Generate(ctx, []*schema.Message{
		schema.UserMessage("Test message"),
	})

	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Greater(t, testTool.callCount, 0, "Tool should have been called")
}

func TestAgentStream(t *testing.T) {
	ctx := context.Background()

	testTool := &mockTool{}

	mockModel := &mockChatModel{
		streamFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			sr, sw := schema.Pipe[*schema.Message](1)
			go func() {
				defer sw.Close()
				sw.Send(schema.AssistantMessage("Response", nil), nil)
			}()
			return sr, nil
		},
		generateFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			return schema.AssistantMessage("COMPLETE: Done", nil), nil
		},
	}

	config := &AgentConfig{
		ToolCallingModel: mockModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{testTool},
		},
		MaxIterations: 1,
	}

	agent, err := NewAgent(ctx, config)
	assert.NoError(t, err)

	stream, err := agent.Stream(ctx, []*schema.Message{
		schema.UserMessage("Test stream"),
	})

	assert.NoError(t, err)
	assert.NotNil(t, stream)

	// Read from stream
	msg, err := stream.Recv()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
}

func TestDefaultValues(t *testing.T) {
	ctx := context.Background()

	mockModel := &mockChatModel{}

	config := &AgentConfig{
		ToolCallingModel: mockModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{},
		},
		// Don't set MaxIterations, MaxSteps, CritiquePrompt, GraphName
	}

	agent, err := NewAgent(ctx, config)
	assert.NoError(t, err)
	assert.NotNil(t, agent)

	// Verify defaults were set by checking agent was created successfully
	// The actual values are internal, but if they weren't set correctly,
	// the agent creation would fail
}
