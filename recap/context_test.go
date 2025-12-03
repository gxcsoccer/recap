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
 * Tests for context management
 */

package recap

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestNewContextManager(t *testing.T) {
	tests := []struct {
		name         string
		windowSize   int
		rootTask     string
		systemPrompt string
		wantMsgCount int
	}{
		{
			name:         "with system prompt",
			windowSize:   64,
			rootTask:     "Test task",
			systemPrompt: "You are a helpful assistant",
			wantMsgCount: 1,
		},
		{
			name:         "without system prompt",
			windowSize:   32,
			rootTask:     "Another task",
			systemPrompt: "",
			wantMsgCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewContextManager(tt.windowSize, tt.rootTask, tt.systemPrompt)

			if cm == nil {
				t.Fatal("NewContextManager returned nil")
			}

			if cm.windowSize != tt.windowSize {
				t.Errorf("windowSize = %d, want %d", cm.windowSize, tt.windowSize)
			}

			if cm.rootTask != tt.rootTask {
				t.Errorf("rootTask = %s, want %s", cm.rootTask, tt.rootTask)
			}

			if len(cm.messages) != tt.wantMsgCount {
				t.Errorf("message count = %d, want %d", len(cm.messages), tt.wantMsgCount)
			}

			if tt.systemPrompt != "" && len(cm.messages) > 0 {
				if cm.messages[0].Role != schema.System {
					t.Errorf("first message role = %s, want %s", cm.messages[0].Role, schema.System)
				}
				if cm.messages[0].Content != tt.systemPrompt {
					t.Errorf("system prompt content mismatch")
				}
			}
		})
	}
}

func TestContextManager_AddMessages(t *testing.T) {
	cm := NewContextManager(64, "Test", "")

	// Test AddUserMessage
	cm.AddUserMessage("Hello")
	if len(cm.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cm.messages))
	}
	if cm.messages[0].Role != schema.User {
		t.Errorf("expected user role, got %s", cm.messages[0].Role)
	}
	if cm.messages[0].Content != "Hello" {
		t.Errorf("expected 'Hello', got %s", cm.messages[0].Content)
	}

	// Test AddAssistantMessage
	cm.AddAssistantMessage("Hi there")
	if len(cm.messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(cm.messages))
	}
	if cm.messages[1].Role != schema.Assistant {
		t.Errorf("expected assistant role, got %s", cm.messages[1].Role)
	}

	// Test AddToolResult
	cm.AddToolResult("search", "Found 5 results")
	if len(cm.messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(cm.messages))
	}
	if cm.messages[2].Role != schema.Tool {
		t.Errorf("expected tool role, got %s", cm.messages[2].Role)
	}
	if !strings.Contains(cm.messages[2].Content, "search") {
		t.Error("tool result should contain tool name")
	}
}

func TestContextManager_CurrentDepth(t *testing.T) {
	cm := NewContextManager(64, "Test", "")

	if cm.CurrentDepth() != 0 {
		t.Errorf("initial depth = %d, want 0", cm.CurrentDepth())
	}

	// Push frames and check depth
	plan := &Plan{
		Thinking: "Test thinking",
		Subtasks: []Subtask{
			{Description: "Subtask 1", IsPrimitive: true, ToolName: "search"},
			{Description: "Subtask 2", IsPrimitive: false},
		},
	}
	subtask := &Subtask{Description: "Current subtask", IsPrimitive: false}

	cm.PushFrame(plan, subtask)
	if cm.CurrentDepth() != 1 {
		t.Errorf("after first push depth = %d, want 1", cm.CurrentDepth())
	}

	cm.PushFrame(plan, subtask)
	if cm.CurrentDepth() != 2 {
		t.Errorf("after second push depth = %d, want 2", cm.CurrentDepth())
	}
}

func TestContextManager_PushPopFrame(t *testing.T) {
	cm := NewContextManager(64, "Test", "System prompt")

	plan := &Plan{
		Thinking: "Planning to do task",
		Subtasks: []Subtask{
			{Description: "First subtask", IsPrimitive: true, ToolName: "search"},
			{Description: "Second subtask", IsPrimitive: false},
			{Description: "Third subtask", IsPrimitive: true, ToolName: "calculate"},
		},
	}
	currentSubtask := &Subtask{Description: "First subtask", IsPrimitive: true}

	// Push frame
	cm.PushFrame(plan, currentSubtask)

	if cm.CurrentDepth() != 1 {
		t.Errorf("depth after push = %d, want 1", cm.CurrentDepth())
	}

	frame := cm.CurrentFrame()
	if frame == nil {
		t.Fatal("CurrentFrame returned nil after push")
	}

	if frame.Depth != 0 {
		t.Errorf("frame depth = %d, want 0", frame.Depth)
	}

	if len(frame.RemainingSubtasks) != 2 {
		t.Errorf("remaining subtasks = %d, want 2", len(frame.RemainingSubtasks))
	}

	// Pop frame
	result := &ExecutionResult{
		Success:            true,
		Output:             "Task completed",
		SubtaskDescription: "First subtask",
	}

	poppedFrame := cm.PopFrame(result)
	if poppedFrame == nil {
		t.Fatal("PopFrame returned nil")
	}

	if cm.CurrentDepth() != 0 {
		t.Errorf("depth after pop = %d, want 0", cm.CurrentDepth())
	}

	if cm.CurrentFrame() != nil {
		t.Error("CurrentFrame should be nil after popping all frames")
	}
}

func TestContextManager_SlidingWindow(t *testing.T) {
	windowSize := 5
	cm := NewContextManager(windowSize, "Test", "System")

	// Add more messages than window size
	for i := 0; i < 10; i++ {
		cm.AddUserMessage("Message " + string(rune('0'+i)))
	}

	messages := cm.GetMessages()

	// Should have system message + windowSize messages
	expectedLen := windowSize + 1 // system message is preserved
	if len(messages) != expectedLen {
		t.Errorf("message count = %d, want %d", len(messages), expectedLen)
	}

	// First message should still be system message
	if messages[0].Role != schema.System {
		t.Error("first message should be system message")
	}

	// Last message should be the most recent
	if !strings.Contains(messages[len(messages)-1].Content, "Message 9") {
		t.Errorf("last message should be most recent, got: %s", messages[len(messages)-1].Content)
	}
}

func TestContextManager_GetContextSummary(t *testing.T) {
	cm := NewContextManager(64, "Root task", "System")

	summary := cm.GetContextSummary()

	if !strings.Contains(summary, "Root task") {
		t.Error("summary should contain root task")
	}

	if !strings.Contains(summary, "Current Depth: 0") {
		t.Error("summary should show current depth")
	}

	// Push a frame
	plan := &Plan{
		Thinking: "Test",
		Subtasks: []Subtask{{Description: "Sub", IsPrimitive: true, ToolName: "test"}},
	}
	cm.PushFrame(plan, &Subtask{Description: "Current", IsPrimitive: false})

	summary = cm.GetContextSummary()
	if !strings.Contains(summary, "Recursion Stack") {
		t.Error("summary should contain recursion stack info")
	}
}

func TestContextManager_UpdateFramePlan(t *testing.T) {
	cm := NewContextManager(64, "Test", "")

	plan := &Plan{
		Thinking: "Original thinking",
		Subtasks: []Subtask{
			{Description: "Task 1", IsPrimitive: true, ToolName: "search"},
			{Description: "Task 2", IsPrimitive: false},
		},
	}

	cm.PushFrame(plan, &Subtask{Description: "Task 1", IsPrimitive: true})

	// Update with new plan
	newPlan := &Plan{
		Thinking: "Updated thinking",
		Subtasks: []Subtask{
			{Description: "Task 1", IsPrimitive: true, ToolName: "search"},
			{Description: "New Task 2", IsPrimitive: true, ToolName: "calculate"},
			{Description: "New Task 3", IsPrimitive: false},
		},
	}

	cm.UpdateFramePlan(newPlan)

	frame := cm.CurrentFrame()
	if frame.ParentPlan.Thinking != "Updated thinking" {
		t.Error("plan thinking should be updated")
	}

	// Remaining should be subtasks[1:] = 2 tasks
	if len(frame.RemainingSubtasks) != 2 {
		t.Errorf("remaining subtasks = %d, want 2", len(frame.RemainingSubtasks))
	}
}

func TestContextManager_NestedFrames(t *testing.T) {
	cm := NewContextManager(64, "Root", "System")

	// Simulate 3 levels of nesting
	plan1 := &Plan{
		Thinking: "Level 1",
		Subtasks: []Subtask{{Description: "L1 Sub", IsPrimitive: false}},
	}
	plan2 := &Plan{
		Thinking: "Level 2",
		Subtasks: []Subtask{{Description: "L2 Sub", IsPrimitive: false}},
	}
	plan3 := &Plan{
		Thinking: "Level 3",
		Subtasks: []Subtask{{Description: "L3 Sub", IsPrimitive: true, ToolName: "search"}},
	}

	cm.PushFrame(plan1, &Subtask{Description: "L1 Sub", IsPrimitive: false})
	if cm.CurrentDepth() != 1 {
		t.Errorf("depth = %d, want 1", cm.CurrentDepth())
	}

	cm.PushFrame(plan2, &Subtask{Description: "L2 Sub", IsPrimitive: false})
	if cm.CurrentDepth() != 2 {
		t.Errorf("depth = %d, want 2", cm.CurrentDepth())
	}

	cm.PushFrame(plan3, &Subtask{Description: "L3 Sub", IsPrimitive: true})
	if cm.CurrentDepth() != 3 {
		t.Errorf("depth = %d, want 3", cm.CurrentDepth())
	}

	// Pop all frames
	result := &ExecutionResult{Success: true, Output: "Done"}

	cm.PopFrame(result)
	if cm.CurrentDepth() != 2 {
		t.Errorf("depth after first pop = %d, want 2", cm.CurrentDepth())
	}

	cm.PopFrame(result)
	if cm.CurrentDepth() != 1 {
		t.Errorf("depth after second pop = %d, want 1", cm.CurrentDepth())
	}

	cm.PopFrame(result)
	if cm.CurrentDepth() != 0 {
		t.Errorf("depth after third pop = %d, want 0", cm.CurrentDepth())
	}
}
