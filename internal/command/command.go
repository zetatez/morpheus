package command

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Source indicates where a command was defined.
type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceFile    Source = "file"
	SourceTUI     Source = "tui"
)

// Command represents a slash command that can be invoked as /name.
type Command struct {
	Name        string
	Description string
	Agent       string   // Override agent to use
	Model       string   // Override model in "provider/model" format
	Subtask     bool     // Run as subtask (don't block main flow)
	Source      Source   // Where this command came from
	Tools       []string // Tools this command typically uses
	Hints       []string // Positional argument hints (e.g. ["$1", "$2"])
	Prompt      string   // Template text
	Examples    []CommandExample
}

type CommandExample struct {
	Description string
	Command     string
}

type CommandRegistry struct {
	mu       sync.RWMutex
	commands map[string]*Command
	aliases  map[string]string
}

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]*Command),
		aliases:  make(map[string]string),
	}
}

func (r *CommandRegistry) Register(cmd *Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := strings.ToLower(cmd.Name)
	r.commands[name] = cmd
}

func (r *CommandRegistry) Get(name string) (*Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nameLower := strings.ToLower(name)
	if cmd, ok := r.commands[nameLower]; ok {
		return cmd, true
	}

	if aliasTarget, ok := r.aliases[nameLower]; ok {
		if cmd, ok := r.commands[aliasTarget]; ok {
			return cmd, true
		}
	}

	return nil, false
}

func (r *CommandRegistry) List() []*Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []*Command
	for _, cmd := range r.commands {
		list = append(list, cmd)
	}
	return list
}

func (r *CommandRegistry) RegisterAlias(alias, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[strings.ToLower(alias)] = strings.ToLower(target)
}

// DetectCommand extracts a command name and arguments from user input.
// Returns empty string if no command is detected.
func DetectCommand(input string) (name string, args string) {
	input = strings.TrimSpace(input)
	if len(input) < 2 || input[0] != '/' {
		return "", ""
	}

	parts := strings.SplitN(input[1:], " ", 2)
	name = strings.ToLower(strings.TrimSpace(parts[0]))
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return name, args
}

// Loader discovers and loads commands from multiple sources.
type Loader struct {
	registry     *CommandRegistry
	commandPaths []string
}

func NewLoader(paths []string) *Loader {
	return &Loader{
		registry:     NewCommandRegistry(),
		commandPaths: paths,
	}
}

func (l *Loader) Registry() *CommandRegistry {
	return l.registry
}

func (l *Loader) Load() error {
	for _, path := range l.commandPaths {
		if err := l.loadFromPath(path); err != nil {
			continue
		}
	}
	return nil
}

func (l *Loader) loadFromPath(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Support command.md in subdirectories
			for _, fn := range []string{"command.md", "Command.md", "COMMAND.md"} {
				cmdFile := filepath.Join(path, entry.Name(), fn)
				if data, err := os.ReadFile(cmdFile); err == nil {
					cmd, err := parseCommandFile(string(data), entry.Name())
					if err == nil && cmd != nil {
						cmd.Source = SourceFile
						l.registry.Register(cmd)
					}
					break
				}
			}
			continue
		}

		if strings.HasSuffix(entry.Name(), ".md") {
			data, err := os.ReadFile(filepath.Join(path, entry.Name()))
			if err != nil {
				continue
			}
			cmdName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			cmd, err := parseCommandFile(string(data), cmdName)
			if err == nil && cmd != nil {
				cmd.Source = SourceFile
				l.registry.Register(cmd)
			}
		}
	}

	return nil
}

func parseCommandFile(content string, fallbackName string) (*Command, error) {
	frontmatterMatch := frontmatterRegex.FindStringSubmatch(content)
	var manifest commandManifest
	var promptContent string

	if frontmatterMatch != nil {
		frontmatter := frontmatterMatch[1]
		if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
			return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
		}
		promptContent = strings.TrimSpace(content[len(frontmatterMatch[0]):])
	} else {
		manifest.Name = fallbackName
		promptContent = content
	}

	if manifest.Name == "" {
		manifest.Name = fallbackName
	}
	if manifest.Description == "" && promptContent != "" {
		lines := strings.Split(promptContent, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				manifest.Description = strings.TrimSpace(line[:min(100, len(line))])
				break
			}
		}
	}

	// Extract hint placeholders ($1, $2, $ARGUMENTS)
	hints := extractHints(manifest.Hints, promptContent)

	cmd := &Command{
		Name:        manifest.Name,
		Description: manifest.Description,
		Agent:       manifest.Agent,
		Model:       manifest.Model,
		Subtask:     manifest.Subtask,
		Source:      SourceFile,
		Tools:       manifest.Tools,
		Hints:       hints,
		Prompt:      promptContent,
		Examples:    manifest.Examples,
	}

	return cmd, nil
}

type commandManifest struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Agent       string           `yaml:"agent"`
	Model       string           `yaml:"model"`
	Subtask     bool             `yaml:"subtask"`
	Tools       []string         `yaml:"tools"`
	Hints       []string         `yaml:"hints"`
	Examples    []CommandExample `yaml:"examples"`
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\n(.+?)\n---`)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractHints gets positional argument hints from manifest hints or from the prompt template.
func extractHints(manifestHints []string, prompt string) []string {
	if len(manifestHints) > 0 {
		return manifestHints
	}

	// Auto-detect from template
	var hints []string
	if strings.Contains(prompt, "$ARGUMENTS") {
		hints = append(hints, "$ARGUMENTS")
	}
	if strings.Contains(prompt, "$1") {
		hints = append(hints, "$1")
	}
	if strings.Contains(prompt, "$2") {
		hints = append(hints, "$2")
	}
	return hints
}

type CommandContext struct {
	WorkspaceRoot string
	UserInput     string
	SessionID     string
}

// BuildPrompt processes the command template, substituting variables and executing shell commands.
// Supports:
//   - {{input}} -> UserInput
//   - {{workspace}} -> WorkspaceRoot
//   - $ARGUMENTS -> full argument string
//   - $1, $2, etc. -> positional arguments
//   - !`command` -> runs shell command and includes output
func (c *Command) BuildPrompt(ctx *CommandContext) string {
	prompt := c.Prompt

	// Parse positional arguments from UserInput
	args := parseArgs(ctx.UserInput)

	// Variable substitution
	prompt = strings.ReplaceAll(prompt, "{{input}}", ctx.UserInput)
	prompt = strings.ReplaceAll(prompt, "{{workspace}}", ctx.WorkspaceRoot)

	// Positional arguments
	prompt = strings.ReplaceAll(prompt, "$ARGUMENTS", ctx.UserInput)
	if len(args) > 0 {
		prompt = strings.ReplaceAll(prompt, "$1", args[0])
	}
	if len(args) > 1 {
		prompt = strings.ReplaceAll(prompt, "$2", args[1])
	}
	if len(args) > 2 {
		prompt = strings.ReplaceAll(prompt, "$3", args[2])
	}

	// Execute inline shell commands (!`command`)
	prompt = executeInlineCommands(prompt)

	return prompt
}

// parseArgs splits user input into positional arguments (space-separated, quoted-aware).
func parseArgs(input string) []string {
	if input == "" {
		return nil
	}
	parts := strings.Fields(input)
	return parts
}

// executeInlineCommands finds !`command` patterns and replaces them with output.
func executeInlineCommands(prompt string) string {
	re := inlineCmdRegex
	var result bytes.Buffer
	lastEnd := 0

	for _, match := range re.FindAllStringSubmatchIndex(prompt, -1) {
		// Text before this command
		result.WriteString(prompt[lastEnd:match[0]])

		// The command inside backticks
		cmdStr := prompt[match[2]:match[3]]
		cmdStr = strings.TrimSpace(cmdStr)

		if cmdStr != "" {
			output, err := runInlineCommand(cmdStr)
			if err != nil {
				output = fmt.Sprintf("[command failed: %s]", err)
			}
			result.WriteString(output)
		}

		lastEnd = match[1]
	}
	result.WriteString(prompt[lastEnd:])

	return result.String()
}

var inlineCmdRegex = regexp.MustCompile("!`([^`]+)`")

func runInlineCommand(cmdStr string) (string, error) {
	var cmd *exec.Cmd
	if strings.Contains(cmdStr, " ") {
		parts := strings.Fields(cmdStr)
		cmd = exec.Command(parts[0], parts[1:]...)
	} else {
		cmd = exec.Command(cmdStr)
	}

	output, err := cmd.Output()
	if err != nil {
		// Try as shell command
		shCmd := exec.Command("sh", "-c", cmdStr)
		output, err = shCmd.Output()
		if err != nil {
			return "", fmt.Errorf("%w: %s", err, string(output))
		}
	}

	return strings.TrimSpace(string(output)), nil
}

func (r *CommandRegistry) LoadBuiltinCommands() {
	// Direct commands (handled without LLM)
	r.Register(&Command{Name: "help", Description: "Show available commands", Source: SourceBuiltin})
	r.Register(&Command{Name: "skills", Description: "List available skills", Source: SourceBuiltin})
	r.Register(&Command{Name: "session", Description: "List and switch sessions", Source: SourceBuiltin})

	// Agent commands (processed by LLM)
	r.Register(&Command{Name: "init", Description: "Initialize .morpheus.md agent instructions for this project", Source: SourceBuiltin, Prompt: "Guide the user through creating a .morpheus.md file for this project. Check if one exists: !`ls -la .morpheus.md 2>/dev/null || echo \"not found\"`\n\nAsk about project conventions, tools, and preferences to include."})
	r.Register(&Command{Name: "review", Description: "Review code changes with git diff focus", Source: SourceBuiltin, Tools: []string{"read", "grep", "bash"}, Prompt: "Review the git changes. Focus on:\n1. Logic errors\n2. Edge cases\n3. Test coverage\n4. Security issues\n\nCurrent changes: !`git diff --stat`\n\n{{input}}"})
	r.Register(&Command{Name: "commit", Description: "Stage and commit changes with good messages", Agent: "build", Source: SourceBuiltin, Tools: []string{"bash", "question"}, Prompt: "Help commit changes:\n1. Show status: !`git status`\n2. Show diff: !`git diff --stat`\n3. Create commit\n\nCommit message: $ARGUMENTS\n\nFollow conventional commits format."})
	r.Register(&Command{Name: "test", Description: "Run tests with coverage", Source: SourceBuiltin, Tools: []string{"bash", "glob"}, Prompt: "Run tests for the project:\n1. Detect test framework: !`ls *test* *spec* Makefile* 2>/dev/null | head -5`\n2. Run tests: !`npm test 2>/dev/null || go test ./... 2>/dev/null || cargo test 2>/dev/null || pytest 2>/dev/null || echo \"no test framework detected\"`\n3. Show coverage if available\n\nAdditional args: $ARGUMENTS"})
	r.Register(&Command{Name: "fix", Description: "Fix build errors in a loop", Agent: "build", Source: SourceBuiltin, Subtask: true, Tools: []string{"read", "edit", "bash"}, Prompt: "Fix build errors iteratively:\n1. Run build: !`npm run build 2>/dev/null || go build ./... 2>/dev/null || cargo build 2>/dev/null || make 2>/dev/null`\n2. Parse errors\n3. Fix each error\n4. Repeat until clean\n\nCurrent issue: $ARGUMENTS"})
	r.Register(&Command{Name: "cleanup", Description: "Find and remove dead code safely", Source: SourceBuiltin, Tools: []string{"grep", "bash"}, Prompt: "Clean up dead code:\n1. Find unused exports: !`grep -r \"export function\\|export const\\|export class\\|export interface\" --include=\"*.ts\" src/ 2>/dev/null | head -20 || echo \"no TS exports found\"`\n2. Check for unused files: !`find src -name \"*.ts\" -o -name \"*.go\" 2>/dev/null | head -20`\n3. Verify no references before removing\n\nTarget: $ARGUMENTS"})
	r.Register(&Command{Name: "learn", Description: "Extract learnings from this session to AGENTS.md", Source: SourceBuiltin, Subtask: true, Prompt: "Extract key learnings from our conversation and format them as additions to AGENTS.md. Focus on:\n1. Project conventions discovered\n2. Architecture decisions made\n3. Testing patterns\n4. Common pitfalls\n\nFormat as actionable agent instructions."})
	r.Register(&Command{Name: "spellcheck", Description: "Spellcheck markdown and documentation files", Source: SourceBuiltin, Subtask: true, Prompt: "Check spelling in markdown files:\n1. Find markdown files: !`find . -name \"*.md\" -not -path \"*/node_modules/*\" -not -path \"*/vendor/*\" 2>/dev/null | head -20`\n2. Check for common typos\n3. Suggest fixes\n\nFocus on: $ARGUMENTS"})

	// TUI commands (handled by frontend, no LLM)
	r.Register(&Command{Name: "new", Description: "Start a new session", Source: SourceTUI})
	r.Register(&Command{Name: "sessions", Description: "Browse and switch sessions", Source: SourceTUI})
	r.Register(&Command{Name: "models", Description: "Browse and switch models", Source: SourceTUI})
	r.Register(&Command{Name: "monitor", Description: "Toggle server metrics monitor", Source: SourceTUI})
	r.Register(&Command{Name: "plan", Description: "Generate a plan for a prompt", Source: SourceTUI, Hints: []string{"<prompt>"}})
	r.Register(&Command{Name: "vim", Description: "Edit a remote file in vim", Source: SourceTUI, Hints: []string{"<path>"}})
	r.Register(&Command{Name: "ssh", Description: "Open an SSH session to the server", Source: SourceTUI})
	r.Register(&Command{Name: "connect", Description: "Connect to a different API server", Source: SourceTUI, Hints: []string{"<url>"}})
	r.Register(&Command{Name: "exit", Description: "Quit the TUI", Source: SourceTUI})

	r.RegisterAlias("ci", "fix")
	r.RegisterAlias("spell", "spellcheck")
}
