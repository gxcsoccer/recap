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
 * ReCAP Planner: Plan generation and refinement functions
 */

package recap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

const defaultPlanPrompt = `You are a planning assistant that decomposes tasks into subtasks.

Given a task description, generate a plan with:
1. Your thinking/reasoning about how to approach the task
2. A list of subtasks to accomplish the goal

For each subtask, determine if it's:
- PRIMITIVE: Can be directly executed using available tools
- NON-PRIMITIVE: Needs further decomposition into smaller subtasks

Available tools:
{{TOOLS}}

Respond in the following JSON format:
{
  "thinking": "Your reasoning about the task and approach...",
  "subtasks": [
    {
      "description": "Description of subtask 1",
      "is_primitive": true,
      "tool_name": "tool_name_if_primitive",
      "tool_args": {"arg1": "value1"}
    },
    {
      "description": "Description of subtask 2",
      "is_primitive": false
    }
  ]
}

Important guidelines:
- Generate a COMPLETE list of subtasks needed to accomplish the goal
- Order subtasks logically (dependencies first)
- Mark a task as primitive ONLY if it can be done with a single tool call
- Complex tasks requiring multiple steps should be non-primitive
- Be specific in subtask descriptions`

const defaultRefinePrompt = `You are a planning assistant that refines plans based on execution results.

The original task: {{TASK}}

Previous thinking: {{THINKING}}

Completed subtask: {{COMPLETED_SUBTASK}}
Result: {{RESULT}}

Remaining subtasks:
{{REMAINING_SUBTASKS}}

Based on the execution result, refine the remaining plan:
1. Update your thinking based on what was learned
2. Adjust, add, or remove subtasks as needed
3. Keep subtasks that are still relevant unchanged

Available tools:
{{TOOLS}}

Respond in the following JSON format:
{
  "thinking": "Updated reasoning based on execution result...",
  "subtasks": [
    {
      "description": "Description of next subtask",
      "is_primitive": true/false,
      "tool_name": "tool_name_if_primitive",
      "tool_args": {"arg1": "value1"}
    }
  ]
}

Important:
- If the result indicates failure, consider alternative approaches
- If successful, proceed with remaining tasks
- You may add new subtasks if the result reveals additional requirements
- Remove subtasks that are no longer needed`

// generatePlan creates an initial plan for a given task using the LLM.
// This implements the planning function π in the ReCAP algorithm.
func (a *Agent) generatePlan(ctx context.Context, task string, messages []*schema.Message) (*Plan, error) {
	prompt := a.config.PlanPrompt
	if prompt == "" {
		prompt = defaultPlanPrompt
	}

	// Replace tool placeholder with actual tool descriptions
	toolsDesc := a.formatToolDescriptions()
	prompt = strings.ReplaceAll(prompt, "{{TOOLS}}", toolsDesc)

	// Build the planning messages
	planMessages := make([]*schema.Message, 0, len(messages)+2)

	// Add system message with planning prompt
	planMessages = append(planMessages, schema.SystemMessage(prompt))

	// Add conversation history (with sliding window)
	windowSize := a.config.SlidingWindowSize
	if windowSize <= 0 {
		windowSize = 64
	}
	startIdx := 0
	if len(messages) > windowSize {
		startIdx = len(messages) - windowSize
	}
	planMessages = append(planMessages, messages[startIdx:]...)

	// Add the task as user message
	planMessages = append(planMessages, schema.UserMessage(fmt.Sprintf("Task: %s\n\nGenerate a plan to accomplish this task.", task)))

	// Call the model
	response, err := a.config.Model.Generate(ctx, planMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	// Parse the response
	plan, err := a.parsePlanResponse(response.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan response: %w", err)
	}

	return plan, nil
}

// refinePlan updates the plan based on execution results.
// This implements the refinement function ρ in the ReCAP algorithm.
func (a *Agent) refinePlan(ctx context.Context, task string, currentPlan *Plan, completedSubtask *Subtask, result *ExecutionResult, messages []*schema.Message) (*Plan, error) {
	prompt := a.config.RefinePrompt
	if prompt == "" {
		prompt = defaultRefinePrompt
	}

	// Replace placeholders
	toolsDesc := a.formatToolDescriptions()
	prompt = strings.ReplaceAll(prompt, "{{TOOLS}}", toolsDesc)
	prompt = strings.ReplaceAll(prompt, "{{TASK}}", task)
	prompt = strings.ReplaceAll(prompt, "{{THINKING}}", currentPlan.Thinking)
	prompt = strings.ReplaceAll(prompt, "{{COMPLETED_SUBTASK}}", completedSubtask.Description)

	resultStr := fmt.Sprintf("Success: %v\nOutput: %s", result.Success, result.Output)
	prompt = strings.ReplaceAll(prompt, "{{RESULT}}", resultStr)

	// Format remaining subtasks
	var remainingStr strings.Builder
	if len(currentPlan.Subtasks) > 1 {
		for i, st := range currentPlan.Subtasks[1:] {
			primitiveStr := "non-primitive"
			if st.IsPrimitive {
				primitiveStr = "primitive"
			}
			remainingStr.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, primitiveStr, st.Description))
		}
	} else {
		remainingStr.WriteString("(no remaining subtasks)")
	}
	prompt = strings.ReplaceAll(prompt, "{{REMAINING_SUBTASKS}}", remainingStr.String())

	// Build the refinement messages
	refineMessages := make([]*schema.Message, 0, len(messages)+2)
	refineMessages = append(refineMessages, schema.SystemMessage(prompt))

	// Add conversation history with sliding window
	windowSize := a.config.SlidingWindowSize
	if windowSize <= 0 {
		windowSize = 64
	}
	startIdx := 0
	if len(messages) > windowSize {
		startIdx = len(messages) - windowSize
	}
	refineMessages = append(refineMessages, messages[startIdx:]...)

	// Add refinement request
	refineMessages = append(refineMessages, schema.UserMessage("Based on the execution result, provide the refined plan."))

	// Call the model
	response, err := a.config.Model.Generate(ctx, refineMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to refine plan: %w", err)
	}

	// Parse the response
	plan, err := a.parsePlanResponse(response.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse refined plan response: %w", err)
	}

	return plan, nil
}

// parsePlanResponse extracts a Plan from the LLM response
func (a *Agent) parsePlanResponse(content string) (*Plan, error) {
	// Try to find JSON in the response
	content = strings.TrimSpace(content)

	// Look for JSON block markers
	if idx := strings.Index(content, "```json"); idx != -1 {
		content = content[idx+7:]
		if endIdx := strings.Index(content, "```"); endIdx != -1 {
			content = content[:endIdx]
		}
	} else if idx := strings.Index(content, "```"); idx != -1 {
		content = content[idx+3:]
		if endIdx := strings.Index(content, "```"); endIdx != -1 {
			content = content[:endIdx]
		}
	}

	// Find JSON object boundaries
	startIdx := strings.Index(content, "{")
	endIdx := strings.LastIndex(content, "}")
	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		return nil, fmt.Errorf("no valid JSON found in response: %s", content)
	}
	content = content[startIdx : endIdx+1]

	var plan Plan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("failed to unmarshal plan JSON: %w, content: %s", err, content)
	}

	return &plan, nil
}

// formatToolDescriptions creates a formatted string of available tools
func (a *Agent) formatToolDescriptions() string {
	var sb strings.Builder
	for _, info := range a.toolInfos {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", info.Name, info.Desc))
		if info.ParamsOneOf != nil {
			// Try to get JSON schema for detailed parameter info
			jsonSchema, err := info.ParamsOneOf.ToJSONSchema()
			if err == nil && jsonSchema != nil && jsonSchema.Properties != nil {
				sb.WriteString("  Parameters:\n")
				// Iterate over the ordered map using its iterator
				for pair := jsonSchema.Properties.Oldest(); pair != nil; pair = pair.Next() {
					name := pair.Key
					prop := pair.Value
					required := ""
					for _, req := range jsonSchema.Required {
						if req == name {
							required = " (required)"
							break
						}
					}
					propType := "any"
					if prop.Type != "" {
						propType = prop.Type
					}
					desc := prop.Description
					if desc == "" {
						desc = "No description"
					}
					sb.WriteString(fmt.Sprintf("    - %s (%s)%s: %s\n", name, propType, required, desc))
				}
			}
		}
	}
	return sb.String()
}
