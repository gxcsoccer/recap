/*
 * Copyright 2024 ReCAP Authors
 *
 * Licensed under the MIT License.
 * You may obtain a copy of the License at
 *
 *     https://opensource.org/licenses/MIT
 */

package recap

import (
	"context"
	"fmt"
	"io"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// State represents the internal state of the ReCAP agent
type State struct {
	Messages         []*schema.Message // All messages in the conversation
	Iterations       int               // Number of iterations performed
	CritiqueResult   string            // Result of the last critique
	ShouldContinue   bool              // Whether to continue the loop
	FinalAnswer      string            // The final answer when ready
	ReturnDirectlyID string            // Tool call ID to return directly
}

func init() {
	schema.RegisterName[*State]("_recap_agent_state")
}

const (
	nodeReason   = "reason"
	nodeExecute  = "execute"
	nodeCritique = "critique"
	nodePlan     = "plan"
	nodeFormat   = "format"
)

// AgentConfig is the configuration for ReCAP agent
type AgentConfig struct {
	// ToolCallingModel is the chat model to be used with tool calling capability
	ToolCallingModel model.ToolCallingChatModel

	// Deprecated: Use ToolCallingModel instead
	Model model.ChatModel

	// ToolsConfig is the config for tools node
	ToolsConfig compose.ToolsNodeConfig

	// CritiqueModel is the model used for critiquing results
	// If not provided, uses the same model as ToolCallingModel
	CritiqueModel model.BaseChatModel

	// MaxIterations is the maximum number of ReCAP iterations
	// Default is 5
	MaxIterations int

	// MaxSteps is the maximum number of graph execution steps
	// Default is MaxIterations * 4 + 10
	MaxSteps int

	// CritiquePrompt is the prompt template for critique
	// Default is a standard critique prompt
	CritiquePrompt string

	// GraphName is the name of the ReCAP graph
	// Default is "ReCAP"
	GraphName string
}

// Agent is the ReCAP agent
// ReCAP stands for Reason, Execute, Critique, Adjust/Plan
// It's an enhanced version of ReAct that adds critique and planning steps
type Agent struct {
	runnable         compose.Runnable[[]*schema.Message, *schema.Message]
	graph            *compose.Graph[[]*schema.Message, *schema.Message]
	graphAddNodeOpts []compose.GraphAddNodeOpt
}

// defaultCritiquePrompt provides a template for critiquing tool execution results
const defaultCritiquePrompt = `You are a critical evaluator. Review the tool execution results and provide feedback.

Previous messages:
%s

Current tool execution results:
%s

Analyze:
1. Were the tool calls successful?
2. Do the results answer the user's question?
3. Is additional information needed?

Respond with:
- "COMPLETE: <summary>" if the question is fully answered
- "CONTINUE: <what's missing>" if more work is needed
- "ERROR: <issue>" if there was a problem
`

// NewAgent creates a new ReCAP agent
func NewAgent(ctx context.Context, config *AgentConfig) (*Agent, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Set defaults
	if config.MaxIterations <= 0 {
		config.MaxIterations = 5
	}
	if config.MaxSteps <= 0 {
		config.MaxSteps = config.MaxIterations*4 + 10
	}
	if config.CritiquePrompt == "" {
		config.CritiquePrompt = defaultCritiquePrompt
	}
	if config.GraphName == "" {
		config.GraphName = "ReCAP"
	}

	// Get the reasoning model
	var reasonModel model.BaseChatModel
	var err error
	if config.ToolCallingModel != nil {
		reasonModel = config.ToolCallingModel
	} else if config.Model != nil {
		// Check if Model supports tool calling
		if tcm, ok := config.Model.(model.ToolCallingChatModel); ok {
			config.ToolCallingModel = tcm
			reasonModel = tcm
		} else {
			reasonModel = config.Model
		}
	} else {
		return nil, fmt.Errorf("either ToolCallingModel or Model must be provided")
	}

	// Get tool infos for the model
	toolInfos, err := genToolInfos(ctx, config.ToolsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tool infos: %w", err)
	}

	// Bind tools to the model
	if len(toolInfos) > 0 && config.ToolCallingModel != nil {
		reasonModel, err = config.ToolCallingModel.WithTools(toolInfos)
		if err != nil {
			return nil, fmt.Errorf("failed to bind tools: %w", err)
		}
	}

	// Get critique model
	critiqueModel := config.CritiqueModel
	if critiqueModel == nil {
		if config.Model != nil {
			critiqueModel = config.Model
		} else if config.ToolCallingModel != nil {
			// ToolCallingChatModel is also a BaseChatModel, use it directly
			critiqueModel = config.ToolCallingModel
		}
	}

	// Create tools node
	toolsNode, err := compose.NewToolNode(ctx, &config.ToolsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create tools node: %w", err)
	}

	// Create graph with state
	graph := compose.NewGraph[[]*schema.Message, *schema.Message](
		compose.WithGenLocalState(func(ctx context.Context) *State {
			return &State{
				Messages:       make([]*schema.Message, 0, config.MaxSteps),
				Iterations:     0,
				ShouldContinue: true,
			}
		}),
	)

	// Add Reason node (ChatModel with tools)
	reasonPreHandler := func(ctx context.Context, input []*schema.Message, state *State) ([]*schema.Message, error) {
		if len(state.Messages) == 0 {
			state.Messages = append(state.Messages, input...)
		}
		return state.Messages, nil
	}

	if err = graph.AddChatModelNode(nodeReason, reasonModel,
		compose.WithStatePreHandler(reasonPreHandler),
		compose.WithNodeName("Reason")); err != nil {
		return nil, fmt.Errorf("failed to add reason node: %w", err)
	}

	// Add Execute node (Tools)
	executePreHandler := func(ctx context.Context, input *schema.Message, state *State) (*schema.Message, error) {
		if input != nil {
			state.Messages = append(state.Messages, input)
		}
		return input, nil
	}

	if err = graph.AddToolsNode(nodeExecute, toolsNode,
		compose.WithStatePreHandler(executePreHandler),
		compose.WithNodeName("Execute")); err != nil {
		return nil, fmt.Errorf("failed to add execute node: %w", err)
	}

	// Add Critique node (ChatModel for evaluation)
	critiqueFunc := func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		var result *schema.Message
		err := compose.ProcessState[*State](ctx, func(ctx context.Context, state *State) error {
			// Get recent messages for context
			recentMessages := state.Messages
			if len(recentMessages) > 10 {
				recentMessages = recentMessages[len(recentMessages)-10:]
			}

			// Format messages for critique
			var msgSummary string
			for _, msg := range recentMessages {
				msgSummary += fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content)
			}

			// Format current input
			var currentResults string
			for _, msg := range input {
				if msg.Role == schema.Tool {
					currentResults += fmt.Sprintf("Tool: %s\nResult: %s\n", msg.Name, msg.Content)
				}
			}

			// Create critique prompt
			critiquePrompt := fmt.Sprintf(config.CritiquePrompt, msgSummary, currentResults)

			// Call critique model
			critiqueResp, err := critiqueModel.Generate(ctx, []*schema.Message{
				schema.SystemMessage("You are a critical evaluator of AI agent actions."),
				schema.UserMessage(critiquePrompt),
			})
			if err != nil {
				return fmt.Errorf("critique generation failed: %w", err)
			}

			state.CritiqueResult = critiqueResp.Content
			state.Messages = append(state.Messages, input...)

			// Determine if we should continue based on critique
			if len(state.CritiqueResult) >= 8 {
				prefix := state.CritiqueResult[:8]
				if prefix == "COMPLETE" {
					state.ShouldContinue = false
					// Extract the answer
					if len(state.CritiqueResult) > 10 {
						state.FinalAnswer = state.CritiqueResult[10:]
					}
				} else if prefix == "CONTINUE" {
					state.Iterations++
					state.ShouldContinue = state.Iterations < config.MaxIterations
				}
			}

			result = critiqueResp
			return nil
		})

		return result, err
	}

	if err = graph.AddLambdaNode(nodeCritique, compose.InvokableLambda(critiqueFunc),
		compose.WithNodeName("Critique")); err != nil {
		return nil, fmt.Errorf("failed to add critique node: %w", err)
	}

	// Add Plan node (decides next action based on critique)
	// This node returns []*schema.Message so it can loop back to Reason
	planFunc := func(ctx context.Context, input *schema.Message) ([]*schema.Message, error) {
		var planMessages []*schema.Message
		var shouldContinue bool
		
		err := compose.ProcessState[*State](ctx, func(ctx context.Context, state *State) error {
			shouldContinue = state.ShouldContinue
			
			if shouldContinue {
				// Add critique as a system message to guide next reasoning
				planMsg := schema.SystemMessage(fmt.Sprintf("Critique feedback: %s\nContinue reasoning to address the gaps.", state.CritiqueResult))
				state.Messages = append(state.Messages, planMsg)
				// Return the updated messages for next iteration
				planMessages = state.Messages
			} else {
				// Create final message
				finalMsg := schema.AssistantMessage(state.FinalAnswer, nil)
				state.Messages = append(state.Messages, finalMsg)
				planMessages = []*schema.Message{finalMsg}
			}
			return nil
		})
		return planMessages, err
	}

	if err = graph.AddLambdaNode(nodePlan, compose.InvokableLambda(planFunc),
		compose.WithNodeName("Plan")); err != nil {
		return nil, fmt.Errorf("failed to add plan node: %w", err)
	}

	// Add Format node to extract the final message from the messages array
	formatFunc := func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		if len(input) == 0 {
			return schema.AssistantMessage("No response", nil), nil
		}
		// Return the last message
		return input[len(input)-1], nil
	}

	if err = graph.AddLambdaNode(nodeFormat, compose.InvokableLambda(formatFunc),
		compose.WithNodeName("Format")); err != nil {
		return nil, fmt.Errorf("failed to add format node: %w", err)
	}

	// Build the graph edges
	if err = graph.AddEdge(compose.START, nodeReason); err != nil {
		return nil, err
	}

	// Reason -> Execute (if tool calls) or END (if no tool calls)
	reasonBranch := func(ctx context.Context, sr *schema.StreamReader[*schema.Message]) (string, error) {
		defer sr.Close()
		
		var hasToolCalls bool
		for {
			msg, err := sr.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if len(msg.ToolCalls) > 0 {
				hasToolCalls = true
				break
			}
		}

		if hasToolCalls {
			return nodeExecute, nil
		}
		return compose.END, nil
	}

	if err = graph.AddBranch(nodeReason, compose.NewStreamGraphBranch(reasonBranch,
		map[string]bool{nodeExecute: true, compose.END: true})); err != nil {
		return nil, err
	}

	// Execute -> Critique
	if err = graph.AddEdge(nodeExecute, nodeCritique); err != nil {
		return nil, err
	}

	// Critique -> Plan
	if err = graph.AddEdge(nodeCritique, nodePlan); err != nil {
		return nil, err
	}

	// Plan -> Reason (continue) or Format -> END (complete)
	planBranch := func(ctx context.Context, sr *schema.StreamReader[[]*schema.Message]) (string, error) {
		defer sr.Close()
		
		var shouldContinue bool
		err := compose.ProcessState[*State](ctx, func(ctx context.Context, state *State) error {
			shouldContinue = state.ShouldContinue
			return nil
		})
		if err != nil {
			return "", err
		}

		if shouldContinue {
			return nodeReason, nil
		}
		return nodeFormat, nil
	}

	if err = graph.AddBranch(nodePlan, compose.NewStreamGraphBranch(planBranch,
		map[string]bool{nodeReason: true, nodeFormat: true})); err != nil {
		return nil, err
	}

	if err = graph.AddEdge(nodeFormat, compose.END); err != nil {
		return nil, err
	}

	// Compile the graph
	compileOpts := []compose.GraphCompileOption{
		compose.WithMaxRunSteps(config.MaxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
		compose.WithGraphName(config.GraphName),
	}

	runnable, err := graph.Compile(ctx, compileOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to compile graph: %w", err)
	}

	return &Agent{
		runnable:         runnable,
		graph:            graph,
		graphAddNodeOpts: []compose.GraphAddNodeOpt{compose.WithGraphCompileOptions(compileOpts...)},
	}, nil
}

// Generate generates a response from the agent
func (a *Agent) Generate(ctx context.Context, input []*schema.Message, opts ...compose.Option) (*schema.Message, error) {
	return a.runnable.Invoke(ctx, input, opts...)
}

// Stream returns a streaming response from the agent
func (a *Agent) Stream(ctx context.Context, input []*schema.Message, opts ...compose.Option) (*schema.StreamReader[*schema.Message], error) {
	return a.runnable.Stream(ctx, input, opts...)
}

// ExportGraph exports the underlying graph
func (a *Agent) ExportGraph() (compose.AnyGraph, []compose.GraphAddNodeOpt) {
	return a.graph, a.graphAddNodeOpts
}

// genToolInfos generates tool information from tools config
func genToolInfos(ctx context.Context, config compose.ToolsNodeConfig) ([]*schema.ToolInfo, error) {
	toolInfos := make([]*schema.ToolInfo, 0, len(config.Tools))
	for _, t := range config.Tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, err
		}
		toolInfos = append(toolInfos, info)
	}
	return toolInfos, nil
}
