/*
 * Copyright 2024 ReCAP Authors
 *
 * Licensed under the MIT License.
 */

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gxcsoccer/recap/recap"
)

// Example calculator tool
type CalculatorTool struct{}

func (c *CalculatorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "calculator",
		Desc: "Performs basic arithmetic operations. Input should be a mathematical expression like '2 + 2' or '10 * 5'",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"expression": {
				Type:     schema.String,
				Desc:     "The mathematical expression to evaluate",
				Required: true,
			},
		}),
	}, nil
}

func (c *CalculatorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// Simple implementation - in real use, parse and evaluate the expression
	// For demo purposes, we'll return a mock result
	return fmt.Sprintf("Result of %s = 42 (mock result)", argumentsInJSON), nil
}

// Example search tool
type SearchTool struct{}

func (s *SearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "search",
		Desc: "Searches for information on the internet",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query",
				Required: true,
			},
		}),
	}, nil
}

func (s *SearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// Mock search result
	return fmt.Sprintf("Search results for %s: Found 3 relevant articles about the topic.", argumentsInJSON), nil
}

func main() {
	ctx := context.Background()

	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("Warning: OPENAI_API_KEY not set. This example requires an OpenAI API key.")
		log.Println("Usage: export OPENAI_API_KEY=your_key_here")
		return
	}

	// Create OpenAI chat model with tool calling support
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey: apiKey,
		Model:  "gpt-4",
	})
	if err != nil {
		log.Fatalf("Failed to create chat model: %v", err)
	}

	// Create tools
	calculator := &CalculatorTool{}
	search := &SearchTool{}

	// Configure ReCAP agent
	config := &recap.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{calculator, search},
		},
		MaxIterations: 3,
		GraphName:     "ReCAP-Example",
	}

	// Create the agent
	agent, err := recap.NewAgent(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create ReCAP agent: %v", err)
	}

	// Example 1: Simple question that requires tool use
	fmt.Println("=== Example 1: Calculator ===")
	response1, err := agent.Generate(ctx, []*schema.Message{
		schema.UserMessage("What is 15 multiplied by 8?"),
	})
	if err != nil {
		log.Printf("Error in example 1: %v", err)
	} else {
		fmt.Printf("User: What is 15 multiplied by 8?\n")
		fmt.Printf("Agent: %s\n\n", response1.Content)
	}

	// Example 2: Question requiring multiple steps
	fmt.Println("=== Example 2: Search and Analysis ===")
	response2, err := agent.Generate(ctx, []*schema.Message{
		schema.UserMessage("Search for information about Go programming language and tell me why it's popular for building AI applications."),
	})
	if err != nil {
		log.Printf("Error in example 2: %v", err)
	} else {
		fmt.Printf("User: Search for information about Go programming language...\n")
		fmt.Printf("Agent: %s\n\n", response2.Content)
	}

	// Example 3: Streaming response
	fmt.Println("=== Example 3: Streaming ===")
	stream, err := agent.Stream(ctx, []*schema.Message{
		schema.UserMessage("Calculate 100 + 200 and explain the result."),
	})
	if err != nil {
		log.Printf("Error starting stream: %v", err)
	} else {
		fmt.Printf("User: Calculate 100 + 200 and explain the result.\n")
		fmt.Printf("Agent (streaming): ")
		for {
			msg, err := stream.Recv()
			if err != nil {
				break
			}
			if msg != nil && msg.Content != "" {
				fmt.Print(msg.Content)
			}
		}
		fmt.Println()
	}

	fmt.Println("=== ReCAP Agent Examples Complete ===")
}
