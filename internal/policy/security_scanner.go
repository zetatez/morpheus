package policy

import (
	"regexp"
	"strings"
)

type ScanLevel int

const (
	LevelPattern ScanLevel = iota
	LevelSemantic
	LevelAST
	LevelApproval
)

func (s ScanLevel) String() string {
	switch s {
	case LevelPattern:
		return "pattern"
	case LevelSemantic:
		return "semantic"
	case LevelAST:
		return "ast"
	case LevelApproval:
		return "approval"
	default:
		return "unknown"
	}
}

type ScanResult struct {
	Level       ScanLevel
	Allowed     bool
	RiskLevel   string
	Matches     []string
	Reason      string
	Suggestions []string
}

type SecurityScanner struct {
	patternScanner  *PatternScanner
	semanticScanner *SemanticScanner
	astScanner      *ASTScanner
}

func NewSecurityScanner() *SecurityScanner {
	return &SecurityScanner{
		patternScanner:  NewPatternScanner(),
		semanticScanner: NewSemanticScanner(),
		astScanner:      NewASTScanner(),
	}
}

func (s *SecurityScanner) Scan(command string) ScanResult {
	result := s.patternScanner.Scan(command)
	if !result.Allowed || result.RiskLevel == "critical" || result.RiskLevel == "high" {
		return result
	}

	semanticResult := s.semanticScanner.Scan(command)
	if !semanticResult.Allowed {
		return semanticResult
	}
	if semanticResult.RiskLevel == "high" || semanticResult.RiskLevel == "medium" {
		result = semanticResult
	}

	return result
}

type PatternScanner struct {
	dangerousPatterns []*DangerousPattern
	ssrfPatterns      []*DangerousPattern
	pathTraversal     []*DangerousPattern
}

type DangerousPattern struct {
	Name       string
	Pattern    *regexp.Regexp
	Level      string
	Message    string
	Category   string
	Suggestion string
}

func NewPatternScanner() *PatternScanner {
	return &PatternScanner{
		dangerousPatterns: getDangerousPatterns(),
		ssrfPatterns:      getSSRFPatterns(),
		pathTraversal:     getPathTraversalPatterns(),
	}
}

func (s *PatternScanner) Scan(command string) ScanResult {
	result := ScanResult{
		Level:     LevelPattern,
		Allowed:   true,
		RiskLevel: "low",
	}

	command = stripSafeRedirections(command)

	var maxLevel int
	var matched []string
	var suggestions []string

	for _, pattern := range s.dangerousPatterns {
		if pattern.Pattern.MatchString(command) {
			levelVal, ok := levelOrder[pattern.Level]
			if !ok {
				levelVal = 1
			}
			if levelVal > maxLevel {
				maxLevel = levelVal
			}
			matched = append(matched, pattern.Name)
			if pattern.Suggestion != "" {
				suggestions = append(suggestions, pattern.Suggestion)
			}
		}
	}

	for _, pattern := range s.ssrfPatterns {
		if pattern.Pattern.MatchString(command) {
			levelVal, ok := levelOrder[pattern.Level]
			if !ok {
				levelVal = 1
			}
			if levelVal > maxLevel {
				maxLevel = levelVal
			}
			matched = append(matched, pattern.Name)
			if pattern.Suggestion != "" {
				suggestions = append(suggestions, pattern.Suggestion)
			}
		}
	}

	for _, pattern := range s.pathTraversal {
		if pattern.Pattern.MatchString(command) {
			levelVal, ok := levelOrder[pattern.Level]
			if !ok {
				levelVal = 1
			}
			if levelVal > maxLevel {
				maxLevel = levelVal
			}
			matched = append(matched, pattern.Name)
			if pattern.Suggestion != "" {
				suggestions = append(suggestions, pattern.Suggestion)
			}
		}
	}

	if matched != nil {
		switch maxLevel {
		case 4:
			result.Level = LevelPattern
			result.RiskLevel = "critical"
			result.Allowed = false
		case 3:
			result.Level = LevelPattern
			result.RiskLevel = "high"
			result.Allowed = false
		case 2:
			result.Level = LevelPattern
			result.RiskLevel = "medium"
			result.Allowed = true
		default:
			result.Level = LevelPattern
			result.RiskLevel = "low"
		}
		result.Matches = matched
		result.Suggestions = suggestions
		result.Reason = "pattern match: " + strings.Join(matched, ", ")
	}

	return result
}

func getDangerousPatterns() []*DangerousPattern {
	type patternDef struct {
		name    string
		pattern string
		level   string
		message string
		suggest string
	}

	patterns := []patternDef{
		{"dd_of_device", `(?i)\bdd\s+.*\bof=.*/dev/`, "critical", "dd with device file", "Use a regular file instead of device file"},
		{"mkfs", `(?i)\bmkfs\b`, "critical", "mkfs command", "Avoid formatting filesystems"},
		{"fdisk", `(?i)\bfdisk\b`, "critical", "fdisk command", "Avoid disk partitioning tools"},
		{"shred", `(?i)\bshred\b`, "critical", "shred command", "Use rm instead for file deletion"},
		{"rm_rf_protected", `(?i)\brm\s+-rf\s+(/etc|/usr|/var|/boot|/sys|/proc|/dev)`, "critical", "recursive rm on protected path", "Specify exact paths instead of broad patterns"},
		{"curl_pipe_sh", `\bcurl\s+[^\|]*\|\s*(?:sh|bash|ksh|zsh)`, "critical", "curl piping to shell", "Download script first and inspect before running"},
		{"wget_pipe_sh", `\bwget\s+[^\|]*\|\s*(?:sh|bash|ksh|zsh)`, "critical", "wget piping to shell", "Download script first and inspect before running"},
		{"chmod_777", `(?i)\bchmod\s+[47]777\b`, "high", "chmod 4777 or 777", "Use more restrictive permissions like 755 or 644"},
		{"sudo_su", `(?i)\bsudo\s+su\b`, "high", "sudo su", "Use sudo -i or specify the target user"},
		{"eval_command", `(?i)\beval\s+\$\(`, "high", "eval with command substitution", "Avoid eval, use direct command execution"},
		{"exec_command", `(?i)\bexec\s+\$\(`, "high", "exec with command substitution", "Avoid exec, use subprocess instead"},
	}

	var result []*DangerousPattern
	for _, p := range patterns {
		result = append(result, &DangerousPattern{
			Name:       p.name,
			Pattern:    regexp.MustCompile(p.pattern),
			Level:      p.level,
			Message:    p.message,
			Suggestion: p.suggest,
		})
	}
	return result
}

func getSSRFPatterns() []*DangerousPattern {
	type patternDef struct {
		name    string
		pattern string
		level   string
		message string
		suggest string
	}

	patterns := []patternDef{
		{"curl_localhost", `(?i)\bcurl\s+.*(?:localhost|127\.0\.0\.1)`, "medium", "curl to localhost", "Verify the target service is intended"},
		{"wget_localhost", `(?i)\bwget\s+.*(?:localhost|127\.0\.0\.1)`, "medium", "wget to localhost", "Verify the target service is intended"},
		{"nc_reverse_shell", `(?i)\bnc\s+.*-e\s+`, "critical", "netcat reverse shell", "This pattern may indicate malicious activity"},
		{"nc_connect_back", `(?i)\bnc\s+.*-c\s+`, "critical", "netcat connect back", "This pattern may indicate malicious activity"},
	}

	var result []*DangerousPattern
	for _, p := range patterns {
		result = append(result, &DangerousPattern{
			Name:       p.name,
			Pattern:    regexp.MustCompile(p.pattern),
			Level:      p.level,
			Message:    p.message,
			Suggestion: p.suggest,
		})
	}
	return result
}

func getPathTraversalPatterns() []*DangerousPattern {
	type patternDef struct {
		name    string
		pattern string
		level   string
		message string
		suggest string
	}

	patterns := []patternDef{
		{"path_traversal_dotdot", `\.\.[/\\]`, "high", "path traversal detected", "Avoid using .. in file paths"},
		{"absolute_path_etc", `/etc/passwd`, "high", "accessing /etc/passwd", "System files should not be modified directly"},
		{"absolute_path_shadow", `/etc/shadow`, "critical", "accessing /etc/shadow", "Shadow file access is not allowed"},
	}

	var result []*DangerousPattern
	for _, p := range patterns {
		result = append(result, &DangerousPattern{
			Name:       p.name,
			Pattern:    regexp.MustCompile(p.pattern),
			Level:      p.level,
			Message:    p.message,
			Suggestion: p.suggest,
		})
	}
	return result
}

type SemanticScanner struct{}

func NewSemanticScanner() *SemanticScanner {
	return &SemanticScanner{}
}

func (s *SemanticScanner) Scan(command string) ScanResult {
	result := ScanResult{
		Level:     LevelSemantic,
		Allowed:   true,
		RiskLevel: "low",
	}

	if CheckZshExtensions(command) != nil {
		result.Allowed = false
		result.RiskLevel = "high"
		result.Reason = "zsh dangerous extensions detected"
		return result
	}

	if CheckIFSInjection(command) {
		result.Allowed = false
		result.RiskLevel = "high"
		result.Reason = "IFS injection detected"
		return result
	}

	return result
}

type ASTScanner struct{}

func NewASTScanner() *ASTScanner {
	return &ASTScanner{}
}

func (s *ASTScanner) Analyze(command string) (bool, []string) {
	var issues []string

	if containsHiddenCharacters(command) {
		issues = append(issues, "hidden unicode characters detected")
	}

	if hasSuspiciousEncoding(command) {
		issues = append(issues, "suspicious encoding patterns")
	}

	return len(issues) == 0, issues
}

func containsHiddenCharacters(command string) bool {
	hidden := []string{
		"\x00", "\x1b", "\x200b", "\x200c", "\x200d", "\ufeff",
	}
	for _, char := range hidden {
		if strings.Contains(command, char) {
			return true
		}
	}
	return false
}

func hasSuspiciousEncoding(command string) bool {
	suspicious := []string{
		"%0a", "%0d", "%27", "%22", "%3c", "%3e",
	}
	for _, pattern := range suspicious {
		if strings.Contains(command, pattern) {
			return true
		}
	}
	return false
}

type ApprovalMode int

const (
	ApprovalNone ApprovalMode = iota
	ApprovalTool
	ApprovalCommand
	ApprovalAlways
)

func (s ApprovalMode) String() string {
	switch s {
	case ApprovalNone:
		return "none"
	case ApprovalTool:
		return "tool"
	case ApprovalCommand:
		return "command"
	case ApprovalAlways:
		return "always"
	default:
		return "unknown"
	}
}

func DetermineApprovalLevel(command string, autoApprove bool) ApprovalMode {
	if autoApprove {
		return ApprovalNone
	}

	if isReadOnlyCommand(command) {
		return ApprovalNone
	}

	if isSystemModification(command) {
		return ApprovalAlways
	}

	if isNetworkOperation(command) {
		return ApprovalCommand
	}

	return ApprovalTool
}

func isReadOnlyCommand(command string) bool {
	readOnlyPrefixes := []string{
		"ls ", "cat ", "head ", "tail ", "grep ", "rg ", "ag ",
		"find ", "fd ", "git status", "git diff", "git log",
		"pwd", "tree ", "which ", "file ",
	}

	trimmed := strings.TrimSpace(command)
	for _, prefix := range readOnlyPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func isSystemModification(command string) bool {
	systemMods := []string{
		"rm -rf", "mkfs", "fdisk", "parted", "chmod 0",
		"useradd", "userdel", "passwd ", "systemctl disable",
		"iptables", "ufw ", "firewalld",
	}

	lower := strings.ToLower(command)
	for _, mod := range systemMods {
		if strings.Contains(lower, mod) {
			return true
		}
	}
	return false
}

func isNetworkOperation(command string) bool {
	networkOps := []string{
		"curl ", "wget ", "fetch ", "nc ", "ncat ",
		"netcat ", "ssh ", "scp ", "rsync ",
	}

	lower := strings.ToLower(command)
	for _, op := range networkOps {
		if strings.HasPrefix(lower, op) {
			return true
		}
	}
	return false
}
