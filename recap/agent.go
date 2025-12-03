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
 * ReCAP Agent: Recursive Context-Aware Reasoning and Planning
 * Based on paper: https://arxiv.org/abs/2510.23822
 *
 * Key features:
 * 1. Plan-ahead task decomposition: Generate complete subtask list, execute first, refine rest
 * 2. Structured re-injection of parent plans: Maintain consistent multi-level context
 * 3. Memory-efficient execution: Sliding window context management
 */

package recap

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const defaultSystemPrompt = `You are a helpful AI assistant that can plan and execute complex tasks.
You break down tasks into manageable subtasks and execute them systematically.
When a task is too complex, you decompose it into smaller, more manageable pieces.
You maintain awareness of the overall goal while working on individual subtasks.`

// NewAgent creates a new ReCAP Agent with the given configuration
func NewAgent(ctx context.Context, config *AgentConfig) (*Agent, error) {
	if config.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Set defaults
	if config.MaxDepth <= 0 {
		config.MaxDepth = 5
	}
	if config.MaxStepsPerLevel <= 0 {
		config.MaxStepsPerLevel = 10
	}
	if config.SlidingWindowSize <= 0 {
		config.SlidingWindowSize = 64
	}
	if config.SystemPrompt == "" {
		config.SystemPrompt = defaultSystemPrompt
	}

	// Build tools map and info
	toolsMap := make(map[string]tool.BaseTool)
	toolInfos := make([]*schema.ToolInfo, 0, len(config.Tools))

	for _, t := range config.Tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get tool info: %w", err)
		}
		toolsMap[info.Name] = t
		toolInfos = append(toolInfos, info)
	}

	return &Agent{
		config:    config,
		toolsMap:  toolsMap,
		toolInfos: toolInfos,
	}, nil
}

// Run executes the ReCAP agent loop for a given task.
// This is the main entry point implementing Algorithm 1 from the paper.
func (a *Agent) Run(ctx context.Context, task string, opts ...RunOption) (string, error) {
	// Apply run options
	runCfg := &runConfig{
		callbacks: make([]Callback, 0),
		maxDepth:  a.config.MaxDepth,
	}
	for _, opt := range opts {
		opt(runCfg)
	}

	// Initialize context manager
	ctxMgr := NewContextManager(a.config.SlidingWindowSize, task, a.config.SystemPrompt)

	// Add the initial task to context
	ctxMgr.AddUserMessage(task)

	// Execute the recursive ReCAP algorithm
	result, err := a.executeRecursive(ctx, task, ctxMgr, 0, runCfg)
	if err != nil {
		return "", err
	}

	return result.Output, nil
}

// executeRecursive implements the core recursive ReCAP algorithm.
// Algorithm 1: ReCAP(C, task)
//
//	T, S ← π(C, task)           # Generate initial plan
//	while S ≠ ∅ do
//	    if S[0] is primitive then
//	        result ← execute(S[0])
//	    else
//	        result ← ReCAP(C ∥ ⟨T, S, S[0]⟩, S[0])  # Recursive call
//	    end
//	    T, S ← ρ(C, T, S, result)   # Refine plan
//	end
//	return C ∥ ⟨T, S[1:]⟩          # Return with remaining context
func (a *Agent) executeRecursive(ctx context.Context, task string, ctxMgr *ContextManager, depth int, runCfg *runConfig) (*ExecutionResult, error) {
	// Check depth limit
	if depth >= runCfg.maxDepth {
		return &ExecutionResult{
			Success:            false,
			Output:             fmt.Sprintf("Maximum recursion depth (%d) reached", runCfg.maxDepth),
			SubtaskDescription: task,
		}, nil
	}

	if a.config.Debug {
		log.Printf("[ReCAP] Depth %d: Starting task: %s", depth, task)
	}

	// Step 1: Generate initial plan (T, S ← π(C, task))
	plan, err := a.generatePlan(ctx, task, ctxMgr.GetMessages())
	if err != nil {
		return &ExecutionResult{
			Success:            false,
			Output:             fmt.Sprintf("Failed to generate plan: %v", err),
			SubtaskDescription: task,
		}, nil
	}

	// Notify callbacks
	for _, cb := range runCfg.callbacks {
		cb.OnPlanGenerated(ctx, depth, plan)
	}

	if a.config.Debug {
		log.Printf("[ReCAP] Depth %d: Generated plan with %d subtasks", depth, len(plan.Subtasks))
		log.Printf("[ReCAP] Depth %d: Thinking: %s", depth, plan.Thinking)
	}

	// Add plan to context
	ctxMgr.AddAssistantMessage(fmt.Sprintf("Plan for '%s':\nThinking: %s\nSubtasks: %d", task, plan.Thinking, len(plan.Subtasks)))

	// Step 2: Execute subtasks in order (while S ≠ ∅)
	stepCount := 0
	var lastResult *ExecutionResult

	for len(plan.Subtasks) > 0 {
		stepCount++
		if stepCount > a.config.MaxStepsPerLevel {
			return &ExecutionResult{
				Success:            false,
				Output:             fmt.Sprintf("Maximum steps per level (%d) exceeded", a.config.MaxStepsPerLevel),
				SubtaskDescription: task,
			}, nil
		}

		currentSubtask := &plan.Subtasks[0]

		// Notify callbacks
		for _, cb := range runCfg.callbacks {
			cb.OnSubtaskStart(ctx, depth, currentSubtask)
		}

		if a.config.Debug {
			log.Printf("[ReCAP] Depth %d, Step %d: Executing subtask: %s (primitive: %v)",
				depth, stepCount, currentSubtask.Description, currentSubtask.IsPrimitive)
		}

		var result *ExecutionResult

		if currentSubtask.IsPrimitive {
			// Execute primitive task directly with tool
			result = a.executePrimitive(ctx, currentSubtask)
		} else {
			// Recursive call for non-primitive task
			// C ← ReCAP(C ∥ ⟨T, S, S[0]⟩, S[0])

			// Notify callbacks about recursion
			for _, cb := range runCfg.callbacks {
				cb.OnRecursionEnter(ctx, depth, currentSubtask)
			}

			// Push context frame for descent
			ctxMgr.PushFrame(plan, currentSubtask)

			// Recursive call
			result, err = a.executeRecursive(ctx, currentSubtask.Description, ctxMgr, depth+1, runCfg)
			if err != nil {
				result = &ExecutionResult{
					Success:            false,
					Output:             fmt.Sprintf("Recursive execution failed: %v", err),
					SubtaskDescription: currentSubtask.Description,
				}
			}

			// Pop context frame for return (C ← C ∥ ⟨T, S[1:]⟩)
			ctxMgr.PopFrame(result)

			// Notify callbacks about recursion exit
			for _, cb := range runCfg.callbacks {
				cb.OnRecursionExit(ctx, depth, result)
			}
		}

		lastResult = result

		// Notify callbacks
		for _, cb := range runCfg.callbacks {
			cb.OnSubtaskEnd(ctx, depth, currentSubtask, result)
		}

		// Add result to context
		if result.Success {
			ctxMgr.AddAssistantMessage(fmt.Sprintf("Completed: %s\nResult: %s", currentSubtask.Description, result.Output))
		} else {
			ctxMgr.AddAssistantMessage(fmt.Sprintf("Failed: %s\nError: %s", currentSubtask.Description, result.Output))
		}

		// Step 3: Refine plan based on result (T, S ← ρ(C, T, S, result))
		if len(plan.Subtasks) > 1 || !result.Success {
			oldPlan := plan
			plan, err = a.refinePlan(ctx, task, plan, currentSubtask, result, ctxMgr.GetMessages())
			if err != nil {
				if a.config.Debug {
					log.Printf("[ReCAP] Depth %d: Plan refinement failed: %v, continuing with remaining subtasks", depth, err)
				}
				// If refinement fails, just remove the completed subtask
				plan.Subtasks = plan.Subtasks[1:]
			} else {
				// Notify callbacks
				for _, cb := range runCfg.callbacks {
					cb.OnPlanRefined(ctx, depth, oldPlan, plan)
				}

				if a.config.Debug {
					log.Printf("[ReCAP] Depth %d: Refined plan, now %d subtasks", depth, len(plan.Subtasks))
				}
			}
		} else {
			// No more subtasks after this one
			plan.Subtasks = nil
		}

		// Update context manager with refined plan
		ctxMgr.UpdateFramePlan(plan)
	}

	// Build final result
	if lastResult == nil {
		return &ExecutionResult{
			Success:            true,
			Output:             "Task completed (no subtasks needed)",
			SubtaskDescription: task,
		}, nil
	}

	return &ExecutionResult{
		Success:            lastResult.Success,
		Output:             lastResult.Output,
		SubtaskDescription: task,
	}, nil
}

// executePrimitive executes a primitive task using the appropriate tool
func (a *Agent) executePrimitive(ctx context.Context, subtask *Subtask) *ExecutionResult {
	toolName := subtask.ToolName
	if toolName == "" {
		return &ExecutionResult{
			Success:            false,
			Output:             "No tool specified for primitive task",
			SubtaskDescription: subtask.Description,
		}
	}

	t, exists := a.toolsMap[toolName]
	if !exists {
		return &ExecutionResult{
			Success:            false,
			Output:             fmt.Sprintf("Tool '%s' not found", toolName),
			SubtaskDescription: subtask.Description,
		}
	}

	// Check if tool is invokable
	invokable, ok := t.(tool.InvokableTool)
	if !ok {
		return &ExecutionResult{
			Success:            false,
			Output:             fmt.Sprintf("Tool '%s' is not invokable", toolName),
			SubtaskDescription: subtask.Description,
		}
	}

	// Serialize arguments to JSON
	argsJSON, err := json.Marshal(subtask.ToolArgs)
	if err != nil {
		return &ExecutionResult{
			Success:            false,
			Output:             fmt.Sprintf("Failed to serialize tool arguments: %v", err),
			SubtaskDescription: subtask.Description,
		}
	}

	if a.config.Debug {
		log.Printf("[ReCAP] Executing tool '%s' with args: %s", toolName, string(argsJSON))
	}

	// Execute the tool (no options needed for basic execution)
	output, err := invokable.InvokableRun(ctx, string(argsJSON))
	if err != nil {
		return &ExecutionResult{
			Success:            false,
			Output:             fmt.Sprintf("Tool execution failed: %v", err),
			SubtaskDescription: subtask.Description,
		}
	}

	return &ExecutionResult{
		Success:            true,
		Output:             output,
		SubtaskDescription: subtask.Description,
	}
}

// WithCallbacks adds callbacks to a run
func WithCallbacks(callbacks ...Callback) RunOption {
	return func(rc *runConfig) {
		rc.callbacks = append(rc.callbacks, callbacks...)
	}
}

// WithMaxDepth overrides the maximum recursion depth for a run
func WithMaxDepth(depth int) RunOption {
	return func(rc *runConfig) {
		rc.maxDepth = depth
	}
}

// Configuration options for NewAgent

// WithMaxRecursionDepth sets the maximum recursion depth
func WithMaxRecursionDepth(depth int) AgentOption {
	return func(c *AgentConfig) {
		c.MaxDepth = depth
	}
}

// WithMaxStepsPerLevel sets the maximum steps per recursion level
func WithMaxStepsPerLevel(steps int) AgentOption {
	return func(c *AgentConfig) {
		c.MaxStepsPerLevel = steps
	}
}

// WithSlidingWindowSize sets the context sliding window size
func WithSlidingWindowSize(size int) AgentOption {
	return func(c *AgentConfig) {
		c.SlidingWindowSize = size
	}
}

// WithSystemPrompt sets a custom system prompt
func WithSystemPrompt(prompt string) AgentOption {
	return func(c *AgentConfig) {
		c.SystemPrompt = prompt
	}
}

// WithPlanPrompt sets a custom plan generation prompt
func WithPlanPrompt(prompt string) AgentOption {
	return func(c *AgentConfig) {
		c.PlanPrompt = prompt
	}
}

// WithRefinePrompt sets a custom plan refinement prompt
func WithRefinePrompt(prompt string) AgentOption {
	return func(c *AgentConfig) {
		c.RefinePrompt = prompt
	}
}

// WithDebug enables debug logging
func WithDebug(debug bool) AgentOption {
	return func(c *AgentConfig) {
		c.Debug = debug
	}
}
