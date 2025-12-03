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
 * ReCAP Graph Agent: Graph-based implementation using eino compose
 * This implementation uses eino's Graph orchestration for the ReCAP loop
 */

package recap

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// GraphAgentState represents the state maintained during graph execution
type GraphAgentState struct {
	// Task is the current task being executed
	Task string `json:"task"`

	// Messages is the conversation history
	Messages []*schema.Message `json:"messages"`

	// CurrentPlan is the active plan
	CurrentPlan *Plan `json:"current_plan"`

	// CurrentDepth is the recursion depth
	CurrentDepth int `json:"current_depth"`

	// MaxDepth is the maximum allowed depth
	MaxDepth int `json:"max_depth"`

	// StepCount is the number of steps executed at current level
	StepCount int `json:"step_count"`

	// MaxSteps is the maximum steps per level
	MaxSteps int `json:"max_steps"`

	// Results contains execution results
	Results []*ExecutionResult `json:"results"`

	// RecursionStack maintains context for recursive calls
	RecursionStack []*RecursionFrame `json:"recursion_stack"`

	// FinalOutput is the final result
	FinalOutput string `json:"final_output"`

	// Done indicates if execution is complete
	Done bool `json:"done"`

	// Error contains any error message
	Error string `json:"error"`
}

// RecursionFrame represents a frame in the recursion stack
type RecursionFrame struct {
	Task              string    `json:"task"`
	Plan              *Plan     `json:"plan"`
	RemainingSubtasks []Subtask `json:"remaining_subtasks"`
	Depth             int       `json:"depth"`
}

// Node names for the graph
const (
	NodePlan       = "plan"
	NodeExecute    = "execute"
	NodeRefine     = "refine"
	NodeRoute      = "route"
	NodeCheckLimit = "check_limit"
	NodePushFrame  = "push_frame"
	NodePopFrame   = "pop_frame"
	NodeFinalize   = "finalize"
)

// GraphAgent implements ReCAP using eino's Graph orchestration
type GraphAgent struct {
	agent    *Agent
	graph    compose.Runnable[*GraphAgentState, *GraphAgentState]
	compiled bool
}

// NewGraphAgent creates a new Graph-based ReCAP agent
func NewGraphAgent(ctx context.Context, config *AgentConfig) (*GraphAgent, error) {
	// Create the base agent first
	agent, err := NewAgent(ctx, config)
	if err != nil {
		return nil, err
	}

	ga := &GraphAgent{
		agent: agent,
	}

	// Build and compile the graph
	if err := ga.buildGraph(ctx); err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	return ga, nil
}

// buildGraph constructs the ReCAP execution graph
func (ga *GraphAgent) buildGraph(ctx context.Context) error {
	// Create a new graph with state
	graph := compose.NewGraph[*GraphAgentState, *GraphAgentState]()

	// Add nodes
	// Plan node: generates or retrieves the current plan
	err := graph.AddLambdaNode(NodePlan, compose.InvokableLambda(ga.planNode))
	if err != nil {
		return fmt.Errorf("failed to add plan node: %w", err)
	}

	// CheckLimit node: checks depth and step limits
	err = graph.AddLambdaNode(NodeCheckLimit, compose.InvokableLambda(ga.checkLimitNode))
	if err != nil {
		return fmt.Errorf("failed to add check limit node: %w", err)
	}

	// Execute node: executes the current subtask
	err = graph.AddLambdaNode(NodeExecute, compose.InvokableLambda(ga.executeNode))
	if err != nil {
		return fmt.Errorf("failed to add execute node: %w", err)
	}

	// Refine node: refines the plan based on execution result
	err = graph.AddLambdaNode(NodeRefine, compose.InvokableLambda(ga.refineNode))
	if err != nil {
		return fmt.Errorf("failed to add refine node: %w", err)
	}

	// Route node: determines next action based on state
	err = graph.AddLambdaNode(NodeRoute, compose.InvokableLambda(ga.routeNode))
	if err != nil {
		return fmt.Errorf("failed to add route node: %w", err)
	}

	// Push frame node: pushes a recursion frame
	err = graph.AddLambdaNode(NodePushFrame, compose.InvokableLambda(ga.pushFrameNode))
	if err != nil {
		return fmt.Errorf("failed to add push frame node: %w", err)
	}

	// Pop frame node: pops a recursion frame
	err = graph.AddLambdaNode(NodePopFrame, compose.InvokableLambda(ga.popFrameNode))
	if err != nil {
		return fmt.Errorf("failed to add pop frame node: %w", err)
	}

	// Finalize node: prepares the final output
	err = graph.AddLambdaNode(NodeFinalize, compose.InvokableLambda(ga.finalizeNode))
	if err != nil {
		return fmt.Errorf("failed to add finalize node: %w", err)
	}

	// Add edges
	// START -> Plan
	err = graph.AddEdge(compose.START, NodePlan)
	if err != nil {
		return fmt.Errorf("failed to add START->Plan edge: %w", err)
	}

	// Plan -> CheckLimit
	err = graph.AddEdge(NodePlan, NodeCheckLimit)
	if err != nil {
		return fmt.Errorf("failed to add Plan->CheckLimit edge: %w", err)
	}

	// CheckLimit -> Route
	err = graph.AddEdge(NodeCheckLimit, NodeRoute)
	if err != nil {
		return fmt.Errorf("failed to add CheckLimit->Route edge: %w", err)
	}

	// Add conditional edges from Route using GraphBranch
	// The branch determines: Execute, PushFrame, PopFrame, or Finalize
	routeBranch := compose.NewGraphBranch(
		ga.routeBranchCondition,
		map[string]bool{
			NodeExecute:   true, // Execute primitive tasks
			NodePushFrame: true, // Push frame for recursion
			NodePopFrame:  true, // Pop frame when returning
			NodeFinalize:  true, // Finalize when done
		},
	)
	err = graph.AddBranch(NodeRoute, routeBranch)
	if err != nil {
		return fmt.Errorf("failed to add route branch: %w", err)
	}

	// Execute -> Refine
	err = graph.AddEdge(NodeExecute, NodeRefine)
	if err != nil {
		return fmt.Errorf("failed to add Execute->Refine edge: %w", err)
	}

	// Refine -> CheckLimit
	err = graph.AddEdge(NodeRefine, NodeCheckLimit)
	if err != nil {
		return fmt.Errorf("failed to add Refine->CheckLimit edge: %w", err)
	}

	// PushFrame -> Plan (for recursion)
	err = graph.AddEdge(NodePushFrame, NodePlan)
	if err != nil {
		return fmt.Errorf("failed to add PushFrame->Plan edge: %w", err)
	}

	// PopFrame -> CheckLimit
	err = graph.AddEdge(NodePopFrame, NodeCheckLimit)
	if err != nil {
		return fmt.Errorf("failed to add PopFrame->CheckLimit edge: %w", err)
	}

	// Finalize -> END
	err = graph.AddEdge(NodeFinalize, compose.END)
	if err != nil {
		return fmt.Errorf("failed to add Finalize->END edge: %w", err)
	}

	// Compile the graph
	compiled, err := graph.Compile(ctx)
	if err != nil {
		return fmt.Errorf("failed to compile graph: %w", err)
	}

	ga.graph = compiled
	ga.compiled = true
	return nil
}

// checkLimitNode checks depth and step limits and sets error state if exceeded
func (ga *GraphAgent) checkLimitNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	// Check depth limit
	if state.CurrentDepth >= state.MaxDepth {
		state.Error = fmt.Sprintf("Maximum recursion depth (%d) reached", state.MaxDepth)
		return state, nil
	}

	// Check step limit
	if state.StepCount >= state.MaxSteps {
		state.Error = fmt.Sprintf("Maximum steps (%d) exceeded", state.MaxSteps)
		return state, nil
	}

	return state, nil
}

// routeBranchCondition determines the next node based on state
// This function performs routing logic and may set state.Done when detecting task completion.
// Limit checking and error state setting is handled by checkLimitNode.
func (ga *GraphAgent) routeBranchCondition(ctx context.Context, state *GraphAgentState) (string, error) {
	// Check for errors
	if state.Error != "" {
		return NodeFinalize, nil
	}

	// Check if done
	if state.Done {
		return NodeFinalize, nil
	}

	// Check if we have subtasks to execute
	if state.CurrentPlan == nil || len(state.CurrentPlan.Subtasks) == 0 {
		// No more subtasks at this level
		if len(state.RecursionStack) > 0 {
			// Pop back to parent level
			return NodePopFrame, nil
		}
		// All done - mark as complete
		state.Done = true
		return NodeFinalize, nil
	}

	// Get current subtask
	currentSubtask := &state.CurrentPlan.Subtasks[0]

	if currentSubtask.IsPrimitive {
		// Execute primitive task
		return NodeExecute, nil
	} else {
		// Non-primitive task - need to recurse
		return NodePushFrame, nil
	}
}

// planNode generates or updates the plan
func (ga *GraphAgent) planNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	// Generate plan for current task
	plan, err := ga.agent.generatePlan(ctx, state.Task, state.Messages)
	if err != nil {
		state.Error = fmt.Sprintf("Failed to generate plan: %v", err)
		return state, nil
	}

	state.CurrentPlan = plan

	// Add plan context to messages
	planMsg := fmt.Sprintf("[Depth %d] Plan for '%s':\nThinking: %s\nSubtasks: %d",
		state.CurrentDepth, state.Task, plan.Thinking, len(plan.Subtasks))
	state.Messages = append(state.Messages, schema.AssistantMessage(planMsg, nil))

	return state, nil
}

// executeNode executes a primitive subtask
func (ga *GraphAgent) executeNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	if state.CurrentPlan == nil || len(state.CurrentPlan.Subtasks) == 0 {
		state.Error = "No subtask to execute"
		return state, nil
	}

	currentSubtask := &state.CurrentPlan.Subtasks[0]
	state.StepCount++

	// Execute the primitive task
	result := ga.agent.executePrimitive(ctx, currentSubtask)
	state.Results = append(state.Results, result)

	// Add result to messages
	var resultMsg string
	if result.Success {
		resultMsg = fmt.Sprintf("[Depth %d] Completed: %s\nResult: %s",
			state.CurrentDepth, currentSubtask.Description, result.Output)
	} else {
		resultMsg = fmt.Sprintf("[Depth %d] Failed: %s\nError: %s",
			state.CurrentDepth, currentSubtask.Description, result.Output)
	}
	state.Messages = append(state.Messages, schema.AssistantMessage(resultMsg, nil))

	return state, nil
}

// refineNode refines the plan after execution
func (ga *GraphAgent) refineNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	if state.CurrentPlan == nil || len(state.CurrentPlan.Subtasks) == 0 {
		return state, nil
	}

	// Get the last result
	var lastResult *ExecutionResult
	if len(state.Results) > 0 {
		lastResult = state.Results[len(state.Results)-1]
	}

	currentSubtask := &state.CurrentPlan.Subtasks[0]

	// Check if refinement is needed
	if len(state.CurrentPlan.Subtasks) > 1 || (lastResult != nil && !lastResult.Success) {
		// Refine the plan
		refinedPlan, err := ga.agent.refinePlan(ctx, state.Task, state.CurrentPlan, currentSubtask, lastResult, state.Messages)
		if err != nil {
			// If refinement fails, just remove the completed subtask
			state.CurrentPlan.Subtasks = state.CurrentPlan.Subtasks[1:]
		} else {
			state.CurrentPlan = refinedPlan
		}
	} else {
		// No more subtasks
		state.CurrentPlan.Subtasks = nil
	}

	return state, nil
}

// routeNode is a pass-through node that triggers routing
func (ga *GraphAgent) routeNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	// This node just passes through - routing logic is in routeBranch
	return state, nil
}

// pushFrameNode pushes a new recursion frame for non-primitive task
func (ga *GraphAgent) pushFrameNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	if state.CurrentPlan == nil || len(state.CurrentPlan.Subtasks) == 0 {
		state.Error = "No subtask to recurse into"
		return state, nil
	}

	currentSubtask := &state.CurrentPlan.Subtasks[0]

	// Save current state to recursion stack
	frame := &RecursionFrame{
		Task:              state.Task,
		Plan:              state.CurrentPlan,
		RemainingSubtasks: make([]Subtask, 0),
		Depth:             state.CurrentDepth,
	}
	if len(state.CurrentPlan.Subtasks) > 1 {
		frame.RemainingSubtasks = append(frame.RemainingSubtasks, state.CurrentPlan.Subtasks[1:]...)
	}
	state.RecursionStack = append(state.RecursionStack, frame)

	// Add recursion context to messages
	contextMsg := fmt.Sprintf("[Entering recursion: Depth %d -> %d]\nSubtask: %s",
		state.CurrentDepth, state.CurrentDepth+1, currentSubtask.Description)
	state.Messages = append(state.Messages, schema.AssistantMessage(contextMsg, nil))

	// Update state for recursion
	state.Task = currentSubtask.Description
	state.CurrentPlan = nil
	state.CurrentDepth++
	state.StepCount = 0

	return state, nil
}

// popFrameNode pops a recursion frame when returning from recursion
func (ga *GraphAgent) popFrameNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	if len(state.RecursionStack) == 0 {
		state.Done = true
		return state, nil
	}

	// Pop the frame
	frame := state.RecursionStack[len(state.RecursionStack)-1]
	state.RecursionStack = state.RecursionStack[:len(state.RecursionStack)-1]

	// Get last result for context
	var lastOutput string
	if len(state.Results) > 0 {
		lastOutput = state.Results[len(state.Results)-1].Output
	}

	// Add return context to messages
	contextMsg := fmt.Sprintf("[Returning from recursion: Depth %d -> %d]\nSubtask completed: %s\nResult: %s",
		state.CurrentDepth, frame.Depth, state.Task, lastOutput)
	state.Messages = append(state.Messages, schema.AssistantMessage(contextMsg, nil))

	// Restore state from frame
	state.Task = frame.Task
	state.CurrentDepth = frame.Depth
	state.StepCount = 0

	// Restore plan with remaining subtasks (skip the one we just completed)
	if len(frame.RemainingSubtasks) > 0 {
		state.CurrentPlan = &Plan{
			Thinking: frame.Plan.Thinking,
			Subtasks: frame.RemainingSubtasks,
		}
	} else {
		state.CurrentPlan = &Plan{
			Thinking: frame.Plan.Thinking,
			Subtasks: nil,
		}
	}

	return state, nil
}

// finalizeNode prepares the final output
func (ga *GraphAgent) finalizeNode(ctx context.Context, state *GraphAgentState) (*GraphAgentState, error) {
	state.Done = true

	if state.Error != "" {
		state.FinalOutput = fmt.Sprintf("Error: %s", state.Error)
		return state, nil
	}

	// Build final output from results
	if len(state.Results) > 0 {
		lastResult := state.Results[len(state.Results)-1]
		state.FinalOutput = lastResult.Output
	} else {
		state.FinalOutput = "Task completed (no execution results)"
	}

	return state, nil
}

// Run executes the graph-based ReCAP agent
func (ga *GraphAgent) Run(ctx context.Context, task string, opts ...RunOption) (string, error) {
	if !ga.compiled {
		return "", fmt.Errorf("graph not compiled")
	}

	// Apply run options
	runCfg := &runConfig{
		maxDepth: ga.agent.config.MaxDepth,
	}
	for _, opt := range opts {
		opt(runCfg)
	}

	// Initialize state
	initialState := &GraphAgentState{
		Task:           task,
		Messages:       []*schema.Message{schema.SystemMessage(ga.agent.config.SystemPrompt)},
		CurrentDepth:   0,
		MaxDepth:       runCfg.maxDepth,
		MaxSteps:       ga.agent.config.MaxStepsPerLevel,
		Results:        make([]*ExecutionResult, 0),
		RecursionStack: make([]*RecursionFrame, 0),
	}

	// Add initial task to messages
	initialState.Messages = append(initialState.Messages, schema.UserMessage(task))

	// Run the graph
	finalState, err := ga.graph.Invoke(ctx, initialState)
	if err != nil {
		return "", fmt.Errorf("graph execution failed: %w", err)
	}

	return finalState.FinalOutput, nil
}

// GetAgent returns the underlying Agent
func (ga *GraphAgent) GetAgent() *Agent {
	return ga.agent
}

// StateToJSON converts state to JSON for debugging
func StateToJSON(state *GraphAgentState) string {
	data, _ := json.MarshalIndent(state, "", "  ")
	return string(data)
}
