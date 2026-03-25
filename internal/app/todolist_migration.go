package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskNode represents a node in a task hierarchy
type TaskNode struct {
	ID          string
	ParentID    string
	Children    []*TaskNode
	Content     string
	Status      TaskStatus
	Tool        string
	Note        string
	CheckpointID string
	SubagentID  string
	Depth       int // Tree depth for display purposes
	CreatedAt  int64
	UpdatedAt  int64
}

// TaskHierarchy manages a tree of tasks
type TaskHierarchy struct {
	mu          sync.RWMutex
	RootTasks   []*TaskNode
	taskMap     map[string]*TaskNode
	maxDepth    int
}

// NewTaskHierarchy creates a new task hierarchy manager
func NewTaskHierarchy() *TaskHierarchy {
	return &TaskHierarchy{
		RootTasks: make([]*TaskNode, 0),
		taskMap:   make(map[string]*TaskNode),
		maxDepth:  10, // Maximum nesting depth
	}
}

// AddTask adds a new task to the hierarchy
func (th *TaskHierarchy) AddTask(content string, parentID string) *TaskNode {
	th.mu.Lock()
	defer th.mu.Unlock()

	node := &TaskNode{
		ID:        uuid.NewString(),
		Content:   content,
		Status:    TaskStatusPending,
		CreatedAt: now(),
		UpdatedAt: now(),
	}

	if parentID == "" {
		// Root task
		th.RootTasks = append(th.RootTasks, node)
	} else {
		// Child task
		if parent, ok := th.taskMap[parentID]; ok {
			if parent.Depth >= th.maxDepth {
				// Max depth exceeded, add to parent instead
				th.RootTasks = append(th.RootTasks, node)
			} else {
				parent.Children = append(parent.Children, node)
				node.ParentID = parentID
				node.Depth = parent.Depth + 1
			}
		} else {
			// Parent not found, add as root
			th.RootTasks = append(th.RootTasks, node)
		}
	}

	th.taskMap[node.ID] = node
	return node
}

// GetTask returns a task by ID
func (th *TaskHierarchy) GetTask(id string) (*TaskNode, bool) {
	th.mu.RLock()
	defer th.mu.RUnlock()
	node, ok := th.taskMap[id]
	return node, ok
}

// UpdateTask updates a task's status and metadata
func (th *TaskHierarchy) UpdateTask(id string, status TaskStatus, note string) error {
	th.mu.Lock()
	defer th.mu.Unlock()

	node, ok := th.taskMap[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	node.Status = status
	if note != "" {
		node.Note = note
	}
	node.UpdatedAt = now()

	// If task completed, update parent if all children are done
	if status == TaskStatusCompleted {
		th.updateParentOnChildComplete(node.ParentID)
	}
	if status == TaskStatusCancelled {
		// When a task is cancelled, mark all children as cancelled too
		th.cancelChildren(node)
	}

	return nil
}

// updateParentOnChildComplete checks if parent should be auto-completed
func (th *TaskHierarchy) updateParentOnChildComplete(parentID string) {
	if parentID == "" {
		return
	}

	parent, ok := th.taskMap[parentID]
	if !ok {
		return
	}

	// Check if all children are completed
	allComplete := true
	for _, child := range parent.Children {
		if child.Status != TaskStatusCompleted && child.Status != TaskStatusCancelled {
			allComplete = false
			break
		}
	}

	if allComplete && len(parent.Children) > 0 {
		parent.Status = TaskStatusCompleted
		parent.UpdatedAt = now()
		// Recursively update grandparent
		th.updateParentOnChildComplete(parent.ParentID)
	}
}

// cancelChildren marks all children of a node as cancelled
func (th *TaskHierarchy) cancelChildren(node *TaskNode) {
	for _, child := range node.Children {
		child.Status = TaskStatusCancelled
		child.UpdatedAt = now()
		th.cancelChildren(child)
	}
}

// GetActiveTasks returns all tasks that are currently active
func (th *TaskHierarchy) GetActiveTasks() []*TaskNode {
	th.mu.RLock()
	defer th.mu.RUnlock()

	var active []*TaskNode
	for _, node := range th.taskMap {
		if node.Status == TaskStatusExecuting {
			active = append(active, node)
		}
	}
	return active
}

// GetPendingTasks returns all pending tasks (including children)
func (th *TaskHierarchy) GetPendingTasks() []*TaskNode {
	th.mu.RLock()
	defer th.mu.RUnlock()

	var pending []*TaskNode
	for _, node := range th.taskMap {
		if node.Status == TaskStatusPending {
			pending = append(pending, node)
		}
	}
	return pending
}

// GetTaskTree returns the task tree in a flattened form suitable for display
func (th *TaskHierarchy) GetTaskTree() [][]*TaskNode {
	th.mu.RLock()
	defer th.mu.RUnlock()

	// Group by depth
	tree := make([][]*TaskNode, th.maxDepth+1)
	for _, node := range th.taskMap {
		if node.Depth <= th.maxDepth {
			tree[node.Depth] = append(tree[node.Depth], node)
		}
	}

	// Remove empty levels
	var result [][]*TaskNode
	for _, level := range tree {
		if len(level) > 0 {
			result = append(result, level)
		}
	}
	return result
}

// SetCheckpointID associates a checkpoint with a task
func (th *TaskHierarchy) SetCheckpointID(taskID, checkpointID string) error {
	th.mu.Lock()
	defer th.mu.Unlock()

	node, ok := th.taskMap[taskID]
	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}
	node.CheckpointID = checkpointID
	return nil
}

// GetProgress returns the progress as (completed, total)
func (th *TaskHierarchy) GetProgress() (completed, total int) {
	th.mu.RLock()
	defer th.mu.RUnlock()

	total = len(th.taskMap)
	for _, node := range th.taskMap {
		if node.Status == TaskStatusCompleted {
			completed++
		}
	}
	return
}

var now = time.Now().Unix
