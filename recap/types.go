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
 * ReCAP: Recursive Context-Aware Reasoning and Planning for LLM Agents
 * Based on paper: https://arxiv.org/abs/2510.23822
 */

package recap

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Subtask represents a subtask in the ReCAP planning framework.
// A subtask can either be a primitive task (executable by tools) or
// a non-primitive task (needs further decomposition).
type Subtask struct {
	// Description is the natural language description of the subtask
	Description string `json:"description"`

	// IsPrimitive indicates whether this task can be directly executed
	// by available tools (true) or needs further decomposition (false)
	IsPrimitive bool `json:"is_primitive"`

	// ToolName is the name of the tool to use if this is a primitive task
	ToolName string `json:"tool_name,omitempty"`

	// ToolArgs contains the arguments to pass to the tool
	ToolArgs map[string]any `json:"tool_args,omitempty"`
}

// Plan represents a complete plan with thinking and subtask list.
// This is the core data structure in ReCAP's plan-ahead decomposition.
type Plan struct {
	// Thinking contains the reasoning/chain-of-thought for generating this plan
	Thinking string `json:"thinking"`

	// Subtasks is the ordered list of subtasks to accomplish the goal
	Subtasks []Subtask `json:"subtasks"`
}

// ExecutionResult represents the result of executing a subtask
type ExecutionResult struct {
	// Success indicates whether the execution was successful
	Success bool `json:"success"`

	// Output contains the result or error message
	Output string `json:"output"`

	// SubtaskDescription is the description of the executed subtask
	SubtaskDescription string `json:"subtask_description"`
}

// ContextFrame represents a frame in the recursive context tree.
// Each frame maintains the state at a particular recursion level.
type ContextFrame struct {
	// Depth is the recursion depth (0 = root)
	Depth int

	// ParentPlan contains the plan from the parent frame
	ParentPlan *Plan

	// CurrentSubtask is the subtask being executed at this level
	CurrentSubtask *Subtask

	// RemainingSubtasks contains subtasks yet to be executed
	RemainingSubtasks []Subtask

	// ExecutionHistory contains results from executed subtasks
	ExecutionHistory []ExecutionResult
}

// AgentConfig contains configuration for the ReCAP Agent
type AgentConfig struct {
	// Model is the chat model used for planning and reasoning
	Model model.ChatModel

	// Tools contains the available tools for primitive task execution
	Tools []tool.BaseTool

	// MaxDepth is the maximum recursion depth (default: 5)
	MaxDepth int

	// MaxStepsPerLevel is the maximum steps per recursion level (default: 10)
	MaxStepsPerLevel int

	// SlidingWindowSize is the context window size K (default: 64)
	SlidingWindowSize int

	// SystemPrompt is the base system prompt for the agent
	SystemPrompt string

	// PlanPrompt is the prompt template for plan generation
	PlanPrompt string

	// RefinePrompt is the prompt template for plan refinement
	RefinePrompt string

	// Debug enables verbose logging
	Debug bool
}

// Agent represents a ReCAP Agent instance
type Agent struct {
	config    *AgentConfig
	toolsMap  map[string]tool.BaseTool
	toolInfos []*schema.ToolInfo
}

// AgentOption is a function that configures the agent
type AgentOption func(*AgentConfig)

// RunOption is a function that configures a single run
type RunOption func(*runConfig)

type runConfig struct {
	callbacks []Callback
	maxDepth  int
}

// Callback provides hooks into the agent's execution
type Callback interface {
	// OnPlanGenerated is called when a new plan is generated
	OnPlanGenerated(ctx context.Context, depth int, plan *Plan)

	// OnSubtaskStart is called before executing a subtask
	OnSubtaskStart(ctx context.Context, depth int, subtask *Subtask)

	// OnSubtaskEnd is called after executing a subtask
	OnSubtaskEnd(ctx context.Context, depth int, subtask *Subtask, result *ExecutionResult)

	// OnRecursionEnter is called when entering a recursive call
	OnRecursionEnter(ctx context.Context, depth int, subtask *Subtask)

	// OnRecursionExit is called when exiting a recursive call
	OnRecursionExit(ctx context.Context, depth int, result *ExecutionResult)

	// OnPlanRefined is called when a plan is refined after subtask completion
	OnPlanRefined(ctx context.Context, depth int, oldPlan, newPlan *Plan)
}

// DefaultCallback provides a no-op implementation of Callback
type DefaultCallback struct{}

func (d *DefaultCallback) OnPlanGenerated(ctx context.Context, depth int, plan *Plan)        {}
func (d *DefaultCallback) OnSubtaskStart(ctx context.Context, depth int, subtask *Subtask)   {}
func (d *DefaultCallback) OnSubtaskEnd(ctx context.Context, depth int, subtask *Subtask, result *ExecutionResult) {
}
func (d *DefaultCallback) OnRecursionEnter(ctx context.Context, depth int, subtask *Subtask) {}
func (d *DefaultCallback) OnRecursionExit(ctx context.Context, depth int, result *ExecutionResult) {
}
func (d *DefaultCallback) OnPlanRefined(ctx context.Context, depth int, oldPlan, newPlan *Plan) {}
