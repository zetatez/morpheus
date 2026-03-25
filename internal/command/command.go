package command

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Command struct {
	Name        string
	Description string
	Agent       string
	Model       *ModelConfig
	Tools       []string
	Prompt      string
	Examples    []CommandExample
}

type ModelConfig struct {
	ProviderID string `yaml:"provider_id"`
	ModelID    string `yaml:"model_id"`
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
			cmdFile := filepath.Join(path, entry.Name(), "command.md")
			if data, err := os.ReadFile(cmdFile); err == nil {
				cmd, err := parseCommandFile(string(data), entry.Name())
				if err == nil && cmd != nil {
					l.registry.Register(cmd)
				}
			}
			continue
		}

		if strings.HasSuffix(entry.Name(), ".md") || strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml") {
			data, err := os.ReadFile(filepath.Join(path, entry.Name()))
			if err != nil {
				continue
			}
			cmd, err := parseCommandFile(string(data), strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
			if err == nil && cmd != nil {
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

	cmd := &Command{
		Name:        manifest.Name,
		Description: manifest.Description,
		Agent:       manifest.Agent,
		Model:       manifest.Model,
		Tools:       manifest.Tools,
		Prompt:      promptContent,
		Examples:    manifest.Examples,
	}

	return cmd, nil
}

type commandManifest struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Agent       string           `yaml:"agent"`
	Model       *ModelConfig     `yaml:"model"`
	Tools       []string         `yaml:"tools"`
	Examples    []CommandExample `yaml:"examples"`
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\n(.+?)\n---`)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type CommandContext struct {
	WorkspaceRoot string
	UserInput     string
	SessionID     string
}

func (c *Command) BuildPrompt(ctx *CommandContext) string {
	prompt := c.Prompt
	prompt = strings.ReplaceAll(prompt, "{{input}}", ctx.UserInput)
	prompt = strings.ReplaceAll(prompt, "{{workspace}}", ctx.WorkspaceRoot)
	return prompt
}

type CommandResult struct {
	Command *Command
	Success bool
	Output  string
	Error   error
	Steps   []CommandStep
}

type CommandStep struct {
	Tool     string
	Inputs   map[string]any
	Output   string
	Approved bool
}

func (r *CommandRegistry) LoadBuiltinCommands() {
	gitReview := &Command{
		Name:        "git-code-review",
		Description: "Review code changes with git diff focus",
		Tools:       []string{"read", "grep", "bash"},
		Prompt:      "Review the git changes. Focus on:\n1. Logic errors\n2. Edge cases\n3. Test coverage\n4. Security issues\n\nUse git diff to see changes: !`git diff --stat`\n\n{{input}}",
	}
	r.Register(gitReview)

	gitCommit := &Command{
		Name:        "git-commit",
		Description: "Stage and commit changes with good messages",
		Agent:       "build",
		Tools:       []string{"bash", "question"},
		Prompt:      "Help commit changes:\n1. Show git status: !`git status`\n2. Stage files: !`git add -p`\n3. Create commit: !`git commit -m \"{{input}}\"`\n\nFollow conventional commits format.",
	}
	r.Register(gitCommit)

	test := &Command{
		Name:        "test",
		Description: "Run tests with coverage",
		Tools:       []string{"bash", "glob"},
		Prompt:      "Run tests:\n1. Detect test framework: !`ls *test* *spec* 2>/dev/null || echo \"checking...\"`\n2. Run tests: !`npm test 2>/dev/null || go test ./... 2>/dev/null || pytest 2>/dev/null || echo \"no test framework detected\"`\n3. Show coverage: !`npm run coverage 2>/dev/null || go cover ./... 2>/dev/null || echo \"no coverage\"`",
	}
	r.Register(test)

	fixBuild := &Command{
		Name:        "fix-build-errors",
		Description: "Fix build errors in a loop",
		Agent:       "build",
		Tools:       []string{"read", "edit", "bash"},
		Prompt:      "Fix build errors:\n1. Run build: !`npm run build 2>/dev/null || go build ./... 2>/dev/null || cargo build 2>/dev/null`\n2. Parse errors: !`last_build_output`\n3. Fix each error\n4. Repeat until clean\n\nCurrent issue: {{input}}",
	}
	r.Register(fixBuild)

	cleanup := &Command{
		Name:        "cleanup-dead-code",
		Description: "Find and remove dead code safely",
		Tools:       []string{"grep", "lsp", "bash"},
		Prompt:      "Clean up dead code:\n1. Find unused functions: !`grep -r \"func.*unused\" --include=\"*.go\" 2>/dev/null || echo \"no unused funcs\"`\n2. Check LSP for unused symbols\n3. Verify no references: !`grep -r \"FunctionName\" --include=\"*.go\" 2>/dev/null`\n4. Remove safely\n\nTarget: {{input}}",
	}
	r.Register(cleanup)

	r.RegisterAlias("review", "git-code-review")
	r.RegisterAlias("commit", "git-commit")
	r.RegisterAlias("fix", "fix-build-errors")
	r.RegisterAlias("cleanup", "cleanup-dead-code")
}
