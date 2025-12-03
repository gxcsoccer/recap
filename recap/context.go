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
 * ReCAP Context Management: Implements structured parent plan re-injection
 * and memory-efficient context management
 */

package recap

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ContextManager handles the recursive context tree in ReCAP.
// It implements:
// 1. Structured re-injection of parent plans during descent
// 2. Context restoration during recursive return
// 3. Sliding window for memory efficiency
type ContextManager struct {
	// frames is the stack of context frames (recursion stack)
	frames []*ContextFrame

	// messages contains the shared LLM context
	messages []*schema.Message

	// windowSize is the sliding window size K
	windowSize int

	// rootTask is the original task description
	rootTask string
}

// NewContextManager creates a new context manager
func NewContextManager(windowSize int, rootTask string, systemPrompt string) *ContextManager {
	cm := &ContextManager{
		frames:     make([]*ContextFrame, 0),
		messages:   make([]*schema.Message, 0),
		windowSize: windowSize,
		rootTask:   rootTask,
	}

	// Add system prompt as first message
	if systemPrompt != "" {
		cm.messages = append(cm.messages, schema.SystemMessage(systemPrompt))
	}

	return cm
}

// PushFrame pushes a new context frame for entering a recursion level.
// This implements the descent operation: C ← ReCAP(C ∥ ⟨T, S, S[0]⟩)
func (cm *ContextManager) PushFrame(plan *Plan, currentSubtask *Subtask) {
	depth := len(cm.frames)

	frame := &ContextFrame{
		Depth:             depth,
		ParentPlan:        plan,
		CurrentSubtask:    currentSubtask,
		RemainingSubtasks: make([]Subtask, 0),
		ExecutionHistory:  make([]ExecutionResult, 0),
	}

	// Copy remaining subtasks (all except the first one being executed)
	if len(plan.Subtasks) > 1 {
		frame.RemainingSubtasks = append(frame.RemainingSubtasks, plan.Subtasks[1:]...)
	}

	cm.frames = append(cm.frames, frame)

	// Inject parent plan context into messages
	cm.injectParentContext(frame, plan)
}

// PopFrame pops the current context frame when exiting a recursion level.
// This implements the return operation: C ← C ∥ ⟨T, S[1:]⟩
func (cm *ContextManager) PopFrame(result *ExecutionResult) *ContextFrame {
	if len(cm.frames) == 0 {
		return nil
	}

	// Pop the frame
	frame := cm.frames[len(cm.frames)-1]
	cm.frames = cm.frames[:len(cm.frames)-1]

	// Add execution result to messages
	cm.addExecutionResult(frame, result)

	// Re-inject remaining plan context (S[1:])
	if len(cm.frames) > 0 {
		parentFrame := cm.frames[len(cm.frames)-1]
		cm.injectRemainingPlanContext(parentFrame)
	}

	return frame
}

// CurrentFrame returns the current context frame
func (cm *ContextManager) CurrentFrame() *ContextFrame {
	if len(cm.frames) == 0 {
		return nil
	}
	return cm.frames[len(cm.frames)-1]
}

// CurrentDepth returns the current recursion depth
func (cm *ContextManager) CurrentDepth() int {
	return len(cm.frames)
}

// GetMessages returns the current message context with sliding window applied
func (cm *ContextManager) GetMessages() []*schema.Message {
	if len(cm.messages) <= cm.windowSize {
		return cm.messages
	}

	// Keep the system message and apply sliding window to the rest
	result := make([]*schema.Message, 0, cm.windowSize+1)
	if len(cm.messages) > 0 && cm.messages[0].Role == schema.System {
		result = append(result, cm.messages[0])
		startIdx := len(cm.messages) - cm.windowSize
		if startIdx < 1 {
			startIdx = 1
		}
		result = append(result, cm.messages[startIdx:]...)
	} else {
		startIdx := len(cm.messages) - cm.windowSize
		if startIdx < 0 {
			startIdx = 0
		}
		result = append(result, cm.messages[startIdx:]...)
	}
	return result
}

// AddUserMessage adds a user message to the context
func (cm *ContextManager) AddUserMessage(content string) {
	cm.messages = append(cm.messages, schema.UserMessage(content))
}

// AddAssistantMessage adds an assistant message to the context
func (cm *ContextManager) AddAssistantMessage(content string) {
	cm.messages = append(cm.messages, schema.AssistantMessage(content, nil))
}

// AddToolResult adds a tool execution result to the context
func (cm *ContextManager) AddToolResult(toolName string, result string) {
	// Create a tool message showing the result
	content := fmt.Sprintf("[Tool: %s]\nResult: %s", toolName, result)
	cm.messages = append(cm.messages, &schema.Message{
		Role:    schema.Tool,
		Content: content,
	})
}

// injectParentContext adds parent plan context when entering recursion
func (cm *ContextManager) injectParentContext(frame *ContextFrame, plan *Plan) {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n[Entering subtask at depth %d]\n", frame.Depth))
	sb.WriteString(fmt.Sprintf("Current goal: %s\n", frame.CurrentSubtask.Description))
	sb.WriteString(fmt.Sprintf("Plan thinking: %s\n", plan.Thinking))
	sb.WriteString("Planned subtasks:\n")

	for i, st := range plan.Subtasks {
		marker := "  "
		if i == 0 {
			marker = "→ "
		}
		primitiveStr := "non-primitive"
		if st.IsPrimitive {
			primitiveStr = "primitive"
		}
		sb.WriteString(fmt.Sprintf("%s%d. [%s] %s\n", marker, i+1, primitiveStr, st.Description))
	}

	cm.messages = append(cm.messages, schema.AssistantMessage(sb.String(), nil))
}

// addExecutionResult adds the result of subtask execution to context
func (cm *ContextManager) addExecutionResult(frame *ContextFrame, result *ExecutionResult) {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n[Completed subtask at depth %d]\n", frame.Depth))
	sb.WriteString(fmt.Sprintf("Subtask: %s\n", result.SubtaskDescription))
	if result.Success {
		sb.WriteString("Status: SUCCESS\n")
	} else {
		sb.WriteString("Status: FAILED\n")
	}
	sb.WriteString(fmt.Sprintf("Output: %s\n", result.Output))

	cm.messages = append(cm.messages, schema.AssistantMessage(sb.String(), nil))
}

// injectRemainingPlanContext re-injects remaining subtasks after returning from recursion
func (cm *ContextManager) injectRemainingPlanContext(frame *ContextFrame) {
	if len(frame.RemainingSubtasks) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n[Returning to depth %d - remaining plan]\n", frame.Depth))
	sb.WriteString("Remaining subtasks:\n")

	for i, st := range frame.RemainingSubtasks {
		primitiveStr := "non-primitive"
		if st.IsPrimitive {
			primitiveStr = "primitive"
		}
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, primitiveStr, st.Description))
	}

	cm.messages = append(cm.messages, schema.AssistantMessage(sb.String(), nil))
}

// UpdateFramePlan updates the current frame's remaining subtasks after refinement
func (cm *ContextManager) UpdateFramePlan(newPlan *Plan) {
	if len(cm.frames) == 0 {
		return
	}

	frame := cm.frames[len(cm.frames)-1]
	frame.ParentPlan = newPlan

	// Update remaining subtasks (exclude the first one which is being executed)
	frame.RemainingSubtasks = make([]Subtask, 0)
	if len(newPlan.Subtasks) > 1 {
		frame.RemainingSubtasks = append(frame.RemainingSubtasks, newPlan.Subtasks[1:]...)
	}
}

// GetContextSummary returns a summary of the current context state
func (cm *ContextManager) GetContextSummary() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Root Task: %s\n", cm.rootTask))
	sb.WriteString(fmt.Sprintf("Current Depth: %d\n", cm.CurrentDepth()))
	sb.WriteString(fmt.Sprintf("Message Count: %d\n", len(cm.messages)))

	if len(cm.frames) > 0 {
		sb.WriteString("\nRecursion Stack:\n")
		for _, frame := range cm.frames {
			sb.WriteString(fmt.Sprintf("  Depth %d: %s (remaining: %d subtasks)\n",
				frame.Depth, frame.CurrentSubtask.Description, len(frame.RemainingSubtasks)))
		}
	}

	return sb.String()
}
