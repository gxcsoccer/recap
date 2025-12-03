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
 * Example: ReCAP Agent with sample tools
 */

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cloudwego/eino-examples/recap/recap"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// Sample tool input/output types

type SearchInput struct {
	Query string `json:"query" jsonschema:"description=The search query"`
}

type SearchOutput struct {
	Results []string `json:"results"`
}

type CalculateInput struct {
	Expression string `json:"expression" jsonschema:"description=The mathematical expression to evaluate"`
}

type CalculateOutput struct {
	Result float64 `json:"result"`
}

type WeatherInput struct {
	City string `json:"city" jsonschema:"description=The city name to get weather for"`
}

type WeatherOutput struct {
	Temperature float64 `json:"temperature"`
	Condition   string  `json:"condition"`
}

type FileReadInput struct {
	Path string `json:"path" jsonschema:"description=The file path to read"`
}

type FileReadOutput struct {
	Content string `json:"content"`
}

type FileWriteInput struct {
	Path    string `json:"path" jsonschema:"description=The file path to write to"`
	Content string `json:"content" jsonschema:"description=The content to write"`
}

type FileWriteOutput struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Sample tool implementations

func searchTool() tool.BaseTool {
	return utils.NewTool(
		&schema.ToolInfo{
			Name: "search",
			Desc: "Search for information on the web",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"query": {
					Type:     "string",
					Desc:     "The search query",
					Required: true,
				},
			}),
		},
		func(ctx context.Context, input *SearchInput) (*SearchOutput, error) {
			// Simulated search results
			return &SearchOutput{
				Results: []string{
					fmt.Sprintf("Result 1 for '%s': Found relevant information...", input.Query),
					fmt.Sprintf("Result 2 for '%s': Additional details...", input.Query),
				},
			}, nil
		},
	)
}

func calculateTool() tool.BaseTool {
	return utils.NewTool(
		&schema.ToolInfo{
			Name: "calculate",
			Desc: "Perform mathematical calculations",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"expression": {
					Type:     "string",
					Desc:     "The mathematical expression to evaluate (e.g., '2 + 2', '10 * 5')",
					Required: true,
				},
			}),
		},
		func(ctx context.Context, input *CalculateInput) (*CalculateOutput, error) {
			// Simulated calculation (in real implementation, use a proper expression evaluator)
			return &CalculateOutput{
				Result: 42.0, // Placeholder result
			}, nil
		},
	)
}

func weatherTool() tool.BaseTool {
	return utils.NewTool(
		&schema.ToolInfo{
			Name: "get_weather",
			Desc: "Get current weather information for a city",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"city": {
					Type:     "string",
					Desc:     "The city name",
					Required: true,
				},
			}),
		},
		func(ctx context.Context, input *WeatherInput) (*WeatherOutput, error) {
			// Simulated weather data
			return &WeatherOutput{
				Temperature: 22.5,
				Condition:   "Sunny with some clouds",
			}, nil
		},
	)
}

func fileReadTool() tool.BaseTool {
	return utils.NewTool(
		&schema.ToolInfo{
			Name: "read_file",
			Desc: "Read content from a file",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"path": {
					Type:     "string",
					Desc:     "The file path to read",
					Required: true,
				},
			}),
		},
		func(ctx context.Context, input *FileReadInput) (*FileReadOutput, error) {
			content, err := os.ReadFile(input.Path)
			if err != nil {
				return &FileReadOutput{
					Content: fmt.Sprintf("Error reading file: %v", err),
				}, nil
			}
			return &FileReadOutput{
				Content: string(content),
			}, nil
		},
	)
}

func fileWriteTool() tool.BaseTool {
	return utils.NewTool(
		&schema.ToolInfo{
			Name: "write_file",
			Desc: "Write content to a file",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"path": {
					Type:     "string",
					Desc:     "The file path to write to",
					Required: true,
				},
				"content": {
					Type:     "string",
					Desc:     "The content to write",
					Required: true,
				},
			}),
		},
		func(ctx context.Context, input *FileWriteInput) (*FileWriteOutput, error) {
			err := os.WriteFile(input.Path, []byte(input.Content), 0644)
			if err != nil {
				return &FileWriteOutput{
					Success: false,
					Message: fmt.Sprintf("Error writing file: %v", err),
				}, nil
			}
			return &FileWriteOutput{
				Success: true,
				Message: "File written successfully",
			}, nil
		},
	)
}

// LoggerCallback implements recap.Callback for logging
type LoggerCallback struct{}

func (l *LoggerCallback) OnPlanGenerated(ctx context.Context, depth int, plan *recap.Plan) {
	fmt.Printf("\n📋 [Depth %d] Plan Generated:\n", depth)
	fmt.Printf("   Thinking: %s\n", truncate(plan.Thinking, 100))
	fmt.Printf("   Subtasks (%d):\n", len(plan.Subtasks))
	for i, st := range plan.Subtasks {
		prim := "🔧"
		if !st.IsPrimitive {
			prim = "📦"
		}
		fmt.Printf("     %d. %s %s\n", i+1, prim, truncate(st.Description, 60))
	}
}

func (l *LoggerCallback) OnSubtaskStart(ctx context.Context, depth int, subtask *recap.Subtask) {
	prim := "primitive"
	if !subtask.IsPrimitive {
		prim = "non-primitive"
	}
	fmt.Printf("\n▶️  [Depth %d] Starting subtask (%s): %s\n", depth, prim, truncate(subtask.Description, 60))
}

func (l *LoggerCallback) OnSubtaskEnd(ctx context.Context, depth int, subtask *recap.Subtask, result *recap.ExecutionResult) {
	status := "✅"
	if !result.Success {
		status = "❌"
	}
	fmt.Printf("%s [Depth %d] Subtask completed: %s\n", status, depth, truncate(result.Output, 80))
}

func (l *LoggerCallback) OnRecursionEnter(ctx context.Context, depth int, subtask *recap.Subtask) {
	fmt.Printf("\n⬇️  [Depth %d] Entering recursion for: %s\n", depth, truncate(subtask.Description, 60))
}

func (l *LoggerCallback) OnRecursionExit(ctx context.Context, depth int, result *recap.ExecutionResult) {
	fmt.Printf("\n⬆️  [Depth %d] Exiting recursion\n", depth)
}

func (l *LoggerCallback) OnPlanRefined(ctx context.Context, depth int, oldPlan, newPlan *recap.Plan) {
	fmt.Printf("\n🔄 [Depth %d] Plan refined: %d -> %d subtasks\n", depth, len(oldPlan.Subtasks), len(newPlan.Subtasks))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func main() {
	ctx := context.Background()

	// Parse command line arguments
	useGraph := false
	for _, arg := range os.Args[1:] {
		if arg == "--graph" || arg == "-g" {
			useGraph = true
		}
	}

	// Get OpenAI API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI chat model
	model, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey: apiKey,
		Model:  "gpt-4o", // or "gpt-4", "gpt-3.5-turbo"
	})
	if err != nil {
		log.Fatalf("Failed to create chat model: %v", err)
	}

	// Create tools
	tools := []tool.BaseTool{
		searchTool(),
		calculateTool(),
		weatherTool(),
		fileReadTool(),
		fileWriteTool(),
	}

	// Agent configuration
	config := &recap.AgentConfig{
		Model:             model,
		Tools:             tools,
		MaxDepth:          3,
		MaxStepsPerLevel:  10,
		SlidingWindowSize: 64,
		Debug:             false,
		SystemPrompt: `You are a helpful AI assistant that excels at breaking down complex tasks.
When given a task:
1. Think about what needs to be done
2. Break it into concrete subtasks
3. Mark simple, single-action tasks as primitive (with tool name and args)
4. Mark complex tasks requiring multiple steps as non-primitive

Available tools: search, calculate, get_weather, read_file, write_file`,
	}

	// Example task
	task := `I need to plan a trip to Tokyo. Please:
1. Check the current weather in Tokyo
2. Search for popular tourist attractions
3. Create a simple itinerary document`

	fmt.Println("================================================")
	if useGraph {
		fmt.Println("ReCAP Agent Demo (Graph-based implementation)")
	} else {
		fmt.Println("ReCAP Agent Demo (Direct implementation)")
	}
	fmt.Println("================================================")
	fmt.Printf("\nTask: %s\n", task)
	fmt.Println("\n------------------------------------------------")

	var result string

	if useGraph {
		// Use Graph-based ReCAP agent
		graphAgent, err := recap.NewGraphAgent(ctx, config)
		if err != nil {
			log.Fatalf("Failed to create graph agent: %v", err)
		}
		result, err = graphAgent.Run(ctx, task)
		if err != nil {
			log.Fatalf("Graph agent execution failed: %v", err)
		}
	} else {
		// Use direct ReCAP agent
		agent, err := recap.NewAgent(ctx, config)
		if err != nil {
			log.Fatalf("Failed to create agent: %v", err)
		}
		result, err = agent.Run(ctx, task, recap.WithCallbacks(&LoggerCallback{}))
		if err != nil {
			log.Fatalf("Agent execution failed: %v", err)
		}
	}

	fmt.Println("\n================================================")
	fmt.Println("Final Result:")
	fmt.Println("================================================")
	fmt.Println(result)
}
