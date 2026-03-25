package app

import "strings"

var TOOL_GROUPS = map[string][]string{
	"edit":    {"edit", "write", "apply_patch", "multiedit"},
	"read":    {"read", "lsp"},
	"browse":  {"glob", "grep"},
	"exec":    {"bash", "exec", "shell"},
	"network": {"webfetch", "websearch", "codesearch", "http"},
	"task":    {"task", "subagent"},
	"code":    {"lsp"},
	"skill":   {"skill"},
	"plan":    {"plan_enter", "plan_exit", "plan.execute", "plan.step"},
}

var toolAliases = map[string]string{
	"cmd.exec":         "bash",
	"lsp.query":        "lsp",
	"conversation.ask": "question",
	"web.fetch":        "webfetch",
	"todo.write":       "todowrite",
	"agent.run":        "task",
	"agent.coordinate": "task",
	"agent.message":    "task",
}

func ToolPermission(tool string) string {
	tool = strings.ToLower(tool)

	if alias, ok := toolAliases[tool]; ok {
		tool = alias
	}

	for group, tools := range TOOL_GROUPS {
		for _, t := range tools {
			if strings.ToLower(t) == tool {
				return group
			}
		}
	}

	if strings.HasPrefix(tool, "lsp.") {
		return "code"
	}

	if strings.HasPrefix(tool, "web.") {
		return "network"
	}

	if strings.HasPrefix(tool, "agent.") {
		return "task"
	}

	return tool
}

func IsToolInGroup(tool, group string) bool {
	tools, ok := TOOL_GROUPS[strings.ToLower(group)]
	if !ok {
		return false
	}
	tool = strings.ToLower(tool)
	if alias, ok := toolAliases[tool]; ok {
		tool = alias
	}
	for _, t := range tools {
		if strings.ToLower(t) == tool {
			return true
		}
	}
	return false
}

func GetToolGroups(tool string) []string {
	var groups []string
	tool = strings.ToLower(tool)
	if alias, ok := toolAliases[tool]; ok {
		tool = alias
	}

	for group, tools := range TOOL_GROUPS {
		for _, t := range tools {
			if strings.ToLower(t) == tool {
				groups = append(groups, group)
				break
			}
		}
	}
	return groups
}

func GetAllPermissionGroups() []string {
	groups := make([]string, 0, len(TOOL_GROUPS))
	for g := range TOOL_GROUPS {
		groups = append(groups, g)
	}
	return groups
}

func GetGroupTools(group string) []string {
	groupLower := strings.ToLower(group)
	tools, ok := TOOL_GROUPS[groupLower]
	if !ok {
		return nil
	}
	result := make([]string, len(tools))
	copy(result, tools)
	return result
}

type ToolDisabledSet map[string]bool

func DisabledTools(allTools []string, rulesets ...PermissionRuleset) ToolDisabledSet {
	disabled := make(ToolDisabledSet)

	for _, tool := range allTools {
		perm := ToolPermission(tool)
		rule := EvaluatePermission(perm, "*", rulesets...)
		if rule.Action == PermissionDeny {
			disabled[tool] = true
		}
	}

	return disabled
}

func FilterDisabledTools(tools []string, rulesets ...PermissionRuleset) []string {
	disabled := DisabledTools(tools, rulesets...)
	var allowed []string
	for _, tool := range tools {
		if !disabled[tool] {
			allowed = append(allowed, tool)
		}
	}
	return allowed
}

func ToolRequiresApproval(tool, pattern string, rulesets ...PermissionRuleset) bool {
	perm := ToolPermission(tool)
	rule := EvaluatePermission(perm, pattern, rulesets...)
	return rule.Action == PermissionAsk
}

func CanExecuteTool(tool, pattern string, rulesets ...PermissionRuleset) bool {
	perm := ToolPermission(tool)
	rule := EvaluatePermission(perm, pattern, rulesets...)
	return rule.Action == PermissionAllow
}
