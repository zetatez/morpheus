package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/internal/tools/agenttool"
)

const (
	coordinatorMaxTasks    = 6
	coordinatorMinWords    = 12
	coordinatorMaxAttempts = 2
)

type coordinatorPlan struct {
	Summary string            `json:"summary"`
	Tasks   []coordinatorTask `json:"tasks"`
}

type coordinatorTask struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	Prompt    string   `json:"prompt"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type coordinatorResult struct {
	Role    string `json:"role"`
	Prompt  string `json:"prompt"`
	Summary string `json:"summary"`
	Error   string `json:"error,omitempty"`
}

func (rt *Runtime) maybeCoordinate(ctx context.Context, sessionID, input string) (string, bool) {
	if !shouldCoordinate(input) {
		return "", false
	}
	if rt.cfg.Planner.Provider == "builtin" || rt.cfg.Planner.APIKey == "" {
		return "", false
	}
	plan, err := rt.buildCoordinatorPlan(ctx, sessionID, input)
	if err != nil || len(plan.Tasks) == 0 {
		return "", false
	}
	results := rt.runCoordinatorTasks(ctx, sessionID, plan.Tasks)
	return renderCoordinatorSummary(plan, results), true
}

func shouldCoordinate(input string) bool {
	words := strings.Fields(input)
	if len(words) < coordinatorMinWords {
		return false
	}
	lower := strings.ToLower(input)
	if strings.Contains(lower, "\n") || strings.Contains(lower, " then ") || strings.Contains(lower, " and ") || strings.Contains(lower, " also ") {
		return true
	}
	if strings.Contains(lower, "plan") || strings.Contains(lower, "architecture") || strings.Contains(lower, "review") {
		return true
	}
	return false
}

func (rt *Runtime) buildCoordinatorPlan(ctx context.Context, sessionID, input string) (coordinatorPlan, error) {
	var messages []map[string]any
	messages = append(messages, map[string]any{"role": "system", "content": coordinatorSystemPrompt})
	if context := rt.coordinatorContext(sessionID, input); context != "" {
		messages = append(messages, map[string]any{"role": "system", "content": context})
	}
	messages = append(messages, map[string]any{"role": "user", "content": input})

	var respContent string
	var err error
	for attempt := 0; attempt < coordinatorMaxAttempts; attempt++ {
		resp, callErr := rt.callChatWithTools(ctx, messages, nil, nil, nil)
		if callErr != nil {
			return coordinatorPlan{}, callErr
		}
		respContent = strings.TrimSpace(resp.Content)
		plan, parseErr := parseCoordinatorPlan(respContent)
		if parseErr == nil {
			return normalizeCoordinatorPlan(plan), nil
		}
		err = parseErr
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": "Return only valid JSON matching the schema. No extra text.",
		})
	}
	return coordinatorPlan{}, err
}

func (rt *Runtime) coordinatorContext(sessionID, input string) string {
	msgs := rt.conversation.Messages(sessionID)
	if len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "user" && last.Content == input {
			msgs = msgs[:len(msgs)-1]
		}
	}
	systemPrompt := rt.systemPrompt(sessionID)
	context := rt.buildHistoryContext(sessionID, systemPrompt, rt.conversation.Summary(sessionID), msgs)
	if context == "" {
		return ""
	}
	return "Context:\n" + context
}

func parseCoordinatorPlan(content string) (coordinatorPlan, error) {
	if content == "" {
		return coordinatorPlan{}, fmt.Errorf("empty coordinator response")
	}
	jsonPayload := extractJSON(content)
	var plan coordinatorPlan
	if err := json.Unmarshal([]byte(jsonPayload), &plan); err != nil {
		return coordinatorPlan{}, err
	}
	return plan, nil
}

func normalizeCoordinatorPlan(plan coordinatorPlan) coordinatorPlan {
	plan.Summary = strings.TrimSpace(plan.Summary)
	if len(plan.Tasks) > coordinatorMaxTasks {
		plan.Tasks = plan.Tasks[:coordinatorMaxTasks]
	}
	for i := range plan.Tasks {
		plan.Tasks[i].Role = normalizeCoordinatorRole(plan.Tasks[i].Role)
		plan.Tasks[i].Prompt = strings.TrimSpace(plan.Tasks[i].Prompt)
		plan.Tasks[i].ID = strings.TrimSpace(plan.Tasks[i].ID)
		if plan.Tasks[i].ID == "" {
			plan.Tasks[i].ID = fmt.Sprintf("task-%d", i+1)
		}
	}
	return plan
}

func normalizeCoordinatorRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return "implementer"
	}
	profiles := agenttoolDefaultProfiles()
	if _, ok := profiles[role]; ok {
		return role
	}
	return role
}

func (rt *Runtime) getAgentProfile(sessionID, role string) agenttool.AgentProfile {
	if rt.agentRegistry != nil {
		if profile, ok := rt.agentRegistry.GetProfile(role); ok {
			return profile
		}
	}
	if rt.subagents != nil && rt.isSubagentAllowed(sessionID, role) {
		if def, ok, err := rt.subagents.LoadByName(role); err == nil && ok {
			if rt.agentRegistry != nil {
				rt.agentRegistry.AddProfile(def.Profile, def.Tools)
			}
			return def.Profile
		}
	}
	if profile, ok := agenttoolDefaultProfiles()[role]; ok {
		return profile
	}
	return agenttoolDefaultProfiles()["implementer"]
}

func (rt *Runtime) runCoordinatorTasks(ctx context.Context, sessionID string, tasks []coordinatorTask) []coordinatorResult {
	if !hasDependencies(tasks) {
		return runTasksParallel(ctx, rt, sessionID, tasks)
	}
	return runTasksDAG(ctx, rt, sessionID, tasks)
}

func hasDependencies(tasks []coordinatorTask) bool {
	for _, task := range tasks {
		if len(task.DependsOn) > 0 {
			return true
		}
	}
	return false
}

func runTasksParallel(ctx context.Context, rt *Runtime, sessionID string, tasks []coordinatorTask) []coordinatorResult {
	results := make([]coordinatorResult, len(tasks))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 3)
	for i, task := range tasks {
		wg.Add(1)
		limit <- struct{}{}
		go func(idx int, task coordinatorTask) {
			defer wg.Done()
			defer func() { <-limit }()
			profile := rt.getAgentProfile(sessionID, task.Role)
			summary, err := rt.RunSubAgentWithProfile(ctx, profile, task.Prompt)
			res := coordinatorResult{Role: task.Role, Prompt: task.Prompt, Summary: summary}
			if err != nil {
				res.Error = err.Error()
			}
			results[idx] = res
		}(i, task)
	}
	wg.Wait()
	return results
}

func runTasksDAG(ctx context.Context, rt *Runtime, sessionID string, tasks []coordinatorTask) []coordinatorResult {
	taskMap := make(map[string]int)
	for i, task := range tasks {
		taskMap[task.ID] = i
	}

	results := make([]coordinatorResult, len(tasks))
	completed := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	executeTask := func(task coordinatorTask) {
		defer wg.Done()
		profile := rt.getAgentProfile(sessionID, task.Role)

		// Wait for dependencies
		for _, depID := range task.DependsOn {
			mu.Lock()
			for !completed[depID] {
				mu.Unlock()
				time.Sleep(100 * time.Millisecond)
				mu.Lock()
			}
			mu.Unlock()
		}

		// Execute task
		summary, err := rt.RunSubAgentWithProfile(ctx, profile, task.Prompt)
		res := coordinatorResult{Role: task.Role, Prompt: task.Prompt, Summary: summary}
		if err != nil {
			res.Error = err.Error()
		}

		mu.Lock()
		idx := taskMap[task.ID]
		results[idx] = res
		completed[task.ID] = true
		mu.Unlock()
	}

	// Find tasks with no dependencies (start nodes)
	inDegree := make(map[string]int)
	for _, task := range tasks {
		inDegree[task.ID] = len(task.DependsOn)
	}

	// Execute start tasks in parallel
	startTasks := make([]coordinatorTask, 0)
	for _, task := range tasks {
		if inDegree[task.ID] == 0 {
			startTasks = append(startTasks, task)
		}
	}

	for _, task := range startTasks {
		wg.Add(1)
		go executeTask(task)
	}

	// Wait for all tasks
	wg.Wait()

	// Execute remaining tasks that may have been missed
	for _, task := range tasks {
		if !completed[task.ID] {
			wg.Add(1)
			go executeTask(task)
		}
	}
	wg.Wait()

	return results
}

func renderCoordinatorSummary(plan coordinatorPlan, results []coordinatorResult) string {
	var b strings.Builder
	b.WriteString("Coordinator summary:\n")
	if plan.Summary != "" {
		b.WriteString("Goal: ")
		b.WriteString(plan.Summary)
		b.WriteString("\n")
	}
	for _, res := range results {
		label := res.Role
		if label == "" {
			label = "agent"
		}
		if res.Error != "" {
			b.WriteString(fmt.Sprintf("- [%s] error: %s\n", label, res.Error))
			continue
		}
		summary := strings.TrimSpace(res.Summary)
		if summary == "" {
			summary = "no summary returned"
		}
		b.WriteString(fmt.Sprintf("- [%s] %s\n", label, summary))
	}
	return strings.TrimSpace(b.String())
}

func extractJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func agenttoolDefaultProfiles() map[string]agenttool.AgentProfile {
	return map[string]agenttool.AgentProfile{
		"implementer": {
			Name:        "Implementer",
			Description: "Deliver concrete code changes efficiently.",
			Instructions: "Focus on actionable implementation steps. " +
				"Call out files, edits, and tests needed to finish the task. " +
				"Be concise and avoid speculation.",
		},
		"explorer": {
			Name:        "Explorer",
			Description: "Investigate codebase details and surface key context.",
			Instructions: "Locate relevant files, APIs, and behaviors. " +
				"Summarize findings and point to exact paths or modules. " +
				"Avoid making changes.",
		},
		"reviewer": {
			Name:        "Reviewer",
			Description: "Review changes or plans for risks and improvements.",
			Instructions: "Identify correctness issues, edge cases, and test gaps. " +
				"Recommend fixes and highlight any risky areas.",
		},
		"architect": {
			Name:        "Architect",
			Description: "Design high-level approach and tradeoffs.",
			Instructions: "Propose architecture or system-level approach. " +
				"Call out tradeoffs, sequencing, and integration points.",
		},
		"tester": {
			Name:        "Tester",
			Description: "Write and verify tests for code changes.",
			Instructions: "Identify relevant test files, write unit tests and integration tests. " +
				"Ensure test coverage for new functionality. Run tests and report results.",
		},
		"devops": {
			Name:        "DevOps",
			Description: "Handle deployment, CI/CD, and infrastructure tasks.",
			Instructions: "Focus on deployment scripts, Dockerfiles, CI/CD pipelines, " +
				"infrastructure as code, and environment configuration.",
		},
		"data": {
			Name:        "Data Engineer",
			Description: "Handle data pipelines, queries, and transformations.",
			Instructions: "Work with databases, SQL queries, data transformations, " +
				"ETL pipelines, and data modeling. Suggest optimizations.",
		},
		"security": {
			Name:        "Security Engineer",
			Description: "Review code for security vulnerabilities.",
			Instructions: "Identify security vulnerabilities, secret leaks, " +
				"authentication issues, and provide secure coding recommendations.",
		},
		"docs": {
			Name:        "Technical Writer",
			Description: "Create and maintain documentation.",
			Instructions: "Write clear documentation, API docs, README updates, " +
				"and code comments. Focus on clarity and completeness.",
		},
	}
}

const coordinatorSystemPrompt = "You are a coordinator subagent. Decompose the user request into up to 6 specialized tasks, assign each to a role (implementer, explorer, reviewer, architect, tester, devops, data, security, docs), and return ONLY valid JSON. Schema: {\"summary\": string, \"tasks\": [{\"id\": string, \"role\": string, \"prompt\": string, \"depends_on\": [string]}]}. Keep prompts concise and actionable."
