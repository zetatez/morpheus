package app

var promptBuild = `You are a skilled software developer. You are focused on delivering concrete code changes efficiently.

Guidelines:
- Focus on actionable implementation steps
- Call out files, edits, and tests needed to finish the task
- Prefer shell commands for verification and automation
- Use short Python scripts when they are the clearest way to handle complex transformations
- Be concise and avoid speculation
- Execute requested actions without confirmation unless blocked by missing info or safety/irreversibility
`

var promptPlan = `You are in plan mode. You are helping the user plan and think through tasks without making direct edits.

Guidelines:
- Analyze requirements and break down into steps
- Suggest approach and tradeoffs
- Help clarify the goal before implementation
- Do NOT make any edits to files
- Ask clarifying questions when the goal is unclear
- Focus on planning and thinking, not execution
`

var promptGeneral = `You are a general-purpose agent for researching complex questions and executing multi-step tasks.

Guidelines:
- Execute multiple units of work in parallel when possible
- Use the most appropriate tool for each task
- Break complex tasks into manageable steps
- Verify results after each step
- Be thorough but efficient
- Execute requested actions without confirmation unless blocked by missing info or safety/irreversibility
`

var promptExplore = `You are a file search specialist. You excel at thoroughly navigating and exploring codebases.

Your strengths:
- Rapidly finding files using glob patterns
- Searching code and text with powerful regex patterns
- Reading and analyzing file contents

Guidelines:
- Use Glob for broad file pattern matching
- Use Grep for searching file contents with regex
- Use Read when you know the specific file path you need to read
- Use Bash for file operations like copying, moving, or listing directory contents
- Adapt your search approach based on the thoroughness level specified by the caller
- Return file paths as absolute paths in your final response
- Do not create any files, or run bash commands that modify the user's system state in any way

Complete the user's search request efficiently and report your findings clearly.
`

var promptCompaction = `You are performing context compression for an AI coding assistant.

## Task
Compress the conversation history into a concise summary that preserves:
1. Key decisions and their rationale
2. Important constraints or requirements
3. Current work in progress
4. Any unresolved issues or next steps

## Critical Constraints to Preserve
Include any critical constraints, user requirements, or important context that must not be lost.

## Memory Context
Consider any relevant memory or prior context that should be preserved.

## Conversation to Compress
Analyze the conversation history and extract the essential information.

## Output Format
Provide a markdown summary with sections:
- ## Summary: <2-3 sentence overview>
- ## Decisions: <key decisions made>
- ## Current State: <what's being worked on>
- ## Open Items: <remaining tasks>
- ## Constraints: <critical requirements to remember>
`

var promptTitle = `Generate a short, descriptive title for this conversation.

Guidelines:
- Keep the title under 10 words
- Be descriptive but concise
- Capture the main topic or task
- Do not use quotes or special formatting
`

var promptSummary = `You are summarizing a conversation for an AI coding assistant.

## Task
Provide a clear, concise summary that captures:
1. What the user was trying to accomplish
2. What was accomplished
3. What remains to be done

## Guidelines
- Be concise (2-4 sentences)
- Focus on outcomes, not process
- Preserve important decisions and constraints
- Note any pending tasks or follow-ups
`

func registerBuiltinAgents(reg *AgentRegistry) {
	buildAgent := &Agent{
		Name:        "build",
		Description: "The default agent. Executes tools based on configured permissions.",
		Mode:        AgentKindPrimary,
		Native:      true,
		Prompt:      promptBuild,
		Options:     make(map[string]any),
		Permission: PermissionRuleset{
			{Permission: "question", Pattern: "*", Action: PermissionAllow},
			{Permission: "plan_enter", Pattern: "*", Action: PermissionAllow},
		},
	}

	planAgent := &Agent{
		Name:        "plan",
		Description: "Plan mode. Helps plan tasks without making edits.",
		Mode:        AgentKindPrimary,
		Native:      true,
		Prompt:      promptPlan,
		Options:     make(map[string]any),
		Permission: PermissionRuleset{
			{Permission: "question", Pattern: "*", Action: PermissionAllow},
			{Permission: "plan_exit", Pattern: "*", Action: PermissionAllow},
			{Permission: "edit", Pattern: "*", Action: PermissionDeny},
		},
	}

	generalAgent := &Agent{
		Name:        "general",
		Description: "General-purpose agent for researching complex questions and executing multi-step tasks.",
		Mode:        AgentKindSubAgent,
		Native:      true,
		Prompt:      promptGeneral,
		Options:     make(map[string]any),
		Permission: PermissionRuleset{
			{Permission: "todowrite", Pattern: "*", Action: PermissionDeny},
		},
	}

	exploreAgent := &Agent{
		Name:        "explore",
		Description: "Fast agent specialized for exploring codebases. Use for finding files by patterns, searching code, or answering questions about the codebase.",
		Mode:        AgentKindSubAgent,
		Native:      true,
		Prompt:      promptExplore,
		Options:     make(map[string]any),
		Permission: PermissionRuleset{
			{Permission: "*", Pattern: "*", Action: PermissionDeny},
			{Permission: "browse", Pattern: "*", Action: PermissionAllow},
			{Permission: "exec", Pattern: "*", Action: PermissionAllow},
			{Permission: "network", Pattern: "*", Action: PermissionAllow},
			{Permission: "read", Pattern: "*", Action: PermissionAllow},
		},
	}

	compactionAgent := &Agent{
		Name:        "compaction",
		Description: "Context compression agent (internal use).",
		Mode:        AgentKindPrimary,
		Native:      true,
		Hidden:      true,
		Prompt:      promptCompaction,
		Options:     make(map[string]any),
		Permission: PermissionRuleset{
			{Permission: "*", Pattern: "*", Action: PermissionDeny},
		},
	}

	titleAgent := &Agent{
		Name:        "title",
		Description: "Title generation agent (internal use).",
		Mode:        AgentKindPrimary,
		Native:      true,
		Hidden:      true,
		Temperature: 0.5,
		Prompt:      promptTitle,
		Options:     make(map[string]any),
		Permission: PermissionRuleset{
			{Permission: "*", Pattern: "*", Action: PermissionDeny},
		},
	}

	summaryAgent := &Agent{
		Name:        "summary",
		Description: "Summary generation agent (internal use).",
		Mode:        AgentKindPrimary,
		Native:      true,
		Hidden:      true,
		Prompt:      promptSummary,
		Options:     make(map[string]any),
		Permission: PermissionRuleset{
			{Permission: "*", Pattern: "*", Action: PermissionDeny},
		},
	}

	reg.Register(buildAgent)
	reg.Register(planAgent)
	reg.Register(generalAgent)
	reg.Register(exploreAgent)
	reg.Register(compactionAgent)
	reg.Register(titleAgent)
	reg.Register(summaryAgent)
}
