package policy

import (
	"regexp"
	"strings"
)

type StaticRule struct {
	Name    string
	Pattern *regexp.Regexp
	Level   string
	Message string
}

var bashStaticRules = []StaticRule{
	{
		Name:    "dd_of_device",
		Pattern: regexp.MustCompile(`(?i)\bdd\s+.*\bof=.*/dev/`),
		Level:   "critical",
		Message: "dd with device file target",
	},
	{
		Name:    "dd_of_null",
		Pattern: regexp.MustCompile(`(?i)\bdd\s+.*\bof=/dev/null\b`),
		Level:   "high",
		Message: "dd writing to /dev/null",
	},
	{
		Name:    "mkfs",
		Pattern: regexp.MustCompile(`(?i)\bmkfs\b`),
		Level:   "critical",
		Message: "mkfs command detected",
	},
	{
		Name:    "fdisk",
		Pattern: regexp.MustCompile(`(?i)\bfdisk\b`),
		Level:   "critical",
		Message: "fdisk command detected",
	},
	{
		Name:    "parted",
		Pattern: regexp.MustCompile(`(?i)\bparted\b`),
		Level:   "critical",
		Message: "parted command detected",
	},
	{
		Name:    "shred",
		Pattern: regexp.MustCompile(`(?i)\bshred\b`),
		Level:   "critical",
		Message: "shred command detected",
	},
	{
		Name:    "chmod_777",
		Pattern: regexp.MustCompile(`(?i)\bchmod\s+[47]777\b`),
		Level:   "high",
		Message: "chmod 4777 or 777 detected",
	},
	{
		Name:    "curl_pipe_sh",
		Pattern: regexp.MustCompile(`\bcurl\s+[^\|]*\|\s*(?:sh|bash|ksh|zsh)`),
		Level:   "critical",
		Message: "curl piping to shell",
	},
	{
		Name:    "wget_pipe_sh",
		Pattern: regexp.MustCompile(`\bwget\s+[^\|]*\|\s*(?:sh|bash|ksh|zsh)`),
		Level:   "critical",
		Message: "wget piping to shell",
	},
	{
		Name:    "fetch_pipe_sh",
		Pattern: regexp.MustCompile(`\bfetch\s+[^\|]*\|\s*(?:sh|bash|ksh|zsh)`),
		Level:   "critical",
		Message: "fetch piping to shell",
	},
	{
		Name:    "chmod_suid",
		Pattern: regexp.MustCompile(`(?i)\bchmod\s+[ug=s].*s\b.*-R`),
		Level:   "high",
		Message: "recursive chmod with suid bit",
	},
	{
		Name:    "zsh_modload",
		Pattern: regexp.MustCompile(`(?i)\bzmodload\b.*\bzsh/(system|zpty|net/tcp)`),
		Level:   "critical",
		Message: "zmodload loading dangerous modules",
	},
	{
		Name:    "zsh_zsysopen",
		Pattern: regexp.MustCompile(`(?i)\bzsysopen\b`),
		Level:   "high",
		Message: "zsysopen command detected",
	},
	{
		Name:    "zsh_ztcp",
		Pattern: regexp.MustCompile(`(?i)\bztcp\b`),
		Level:   "critical",
		Message: "ztcp command detected",
	},
	{
		Name:    "zsh_zsocket",
		Pattern: regexp.MustCompile(`(?i)\bzsocket\b`),
		Level:   "critical",
		Message: "zsocket command detected",
	},
	{
		Name:    "zsh_sysopen",
		Pattern: regexp.MustCompile(`(?i)\bsysopen\b`),
		Level:   "high",
		Message: "sysopen command detected",
	},
	{
		Name:    "unicode_zero_width",
		Pattern: regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{FEFF}]`),
		Level:   "high",
		Message: "unicode zero-width characters detected",
	},
	{
		Name:    "rm_rf_protected",
		Pattern: regexp.MustCompile(`(?i)\brm\s+-rf\s+(/etc|/usr|/var|/boot|/sys|/proc|/dev)`),
		Level:   "critical",
		Message: "recursive rm on protected path",
	},
	{
		Name:    "sudo_su",
		Pattern: regexp.MustCompile(`(?i)\bsudo\s+su\b`),
		Level:   "high",
		Message: "sudo su detected",
	},
	{
		Name:    "passwd_root",
		Pattern: regexp.MustCompile(`(?i)\bpasswd\s+root\b`),
		Level:   "critical",
		Message: "changing root password",
	},
	{
		Name:    "useradd",
		Pattern: regexp.MustCompile(`(?i)\buseradd\b`),
		Level:   "high",
		Message: "useradd command detected",
	},
	{
		Name:    "userdel",
		Pattern: regexp.MustCompile(`(?i)\buserdel\b`),
		Level:   "high",
		Message: "userdel command detected",
	},
	{
		Name:    "systemctl_enable",
		Pattern: regexp.MustCompile(`(?i)\bsystemctl\s+enable\b.*@`),
		Level:   "high",
		Message: "enabling systemd service",
	},
	{
		Name:    "curl_upload",
		Pattern: regexp.MustCompile(`(?i)\bcurl\s+.*-T\s+`),
		Level:   "medium",
		Message: "curl file upload detected",
	},
	{
		Name:    "nc_listen",
		Pattern: regexp.MustCompile(`(?i)\bnc\s+.*-l\s+.*-p\b`),
		Level:   "high",
		Message: "netcat listener detected",
	},
	{
		Name:    "ncat_listen",
		Pattern: regexp.MustCompile(`(?i)\bncat\s+.*-l\b`),
		Level:   "high",
		Message: "ncat listener detected",
	},
	{
		Name:    "proc_write",
		Pattern: regexp.MustCompile(`(?i)>\s*/proc/`),
		Level:   "high",
		Message: "writing to /proc",
	},
	{
		Name:    "eval_command",
		Pattern: regexp.MustCompile(`(?i)\beval\s+\$\(`),
		Level:   "high",
		Message: "eval with command substitution",
	},
	{
		Name:    "exec_command",
		Pattern: regexp.MustCompile(`(?i)\bexec\s+\$\(`),
		Level:   "high",
		Message: "exec with command substitution",
	},
}

type BashSecurityResult struct {
	Allowed    bool
	Level      string
	Matches    []string
	SafePrefix string
}

func CheckBashSecurity(command string) BashSecurityResult {
	result := BashSecurityResult{
		Allowed: true,
		Level:   "low",
		Matches: []string{},
	}

	command = stripSafeRedirections(command)

	var maxLevel int
	var matchedRules []string

	for _, rule := range bashStaticRules {
		if rule.Pattern.MatchString(command) {
			levelVal, ok := levelOrder[rule.Level]
			if !ok {
				levelVal = 1
			}
			if levelVal > maxLevel {
				maxLevel = levelVal
			}
			matchedRules = append(matchedRules, rule.Name)
		}
	}

	if matchedRules != nil {
		switch maxLevel {
		case 4:
			result.Level = "critical"
			result.Allowed = false
		case 3:
			result.Level = "high"
			result.Allowed = false
		case 2:
			result.Level = "medium"
			result.Allowed = true
		default:
			result.Level = "low"
		}
		result.Matches = matchedRules
	}

	return result
}

func stripSafeRedirections(cmd string) string {
	cmd = regexp.MustCompile(`>\s*/dev/null\b`).ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(`2>\s*&1\b`).ReplaceAllString(cmd, "")
	cmd = regexp.MustCompile(`\s+\|\s*cat\b`).ReplaceAllString(cmd, "")
	return cmd
}

func CheckZshExtensions(command string) []string {
	var dangerous []string

	zshPatterns := map[string]bool{
		`=curl`:      true,
		`=wget`:      true,
		`=fetch`:     true,
		`=${TMPDIR}`: true,
		`=${TEMP}`:   true,
	}

	for pattern := range zshPatterns {
		if strings.Contains(command, pattern) {
			dangerous = append(dangerous, pattern+" expansion")
		}
	}

	return dangerous
}

func CheckIFSInjection(command string) bool {
	if strings.Contains(command, "IFS=") || strings.Contains(command, "IFS=") {
		return true
	}
	return false
}

func CheckCommandInjection(command string) bool {
	dangerous := []string{
		`$(`,
		"`",
		`${`,
		`||`,
		`&&`,
	}

	for _, pattern := range dangerous {
		if strings.Contains(command, pattern) {
			return true
		}
	}

	return false
}

type ReadOnlyCommand struct {
	Command      string
	AllowedFlags []string
}

var readOnlyCommands = []ReadOnlyCommand{
	{Command: "ls", AllowedFlags: []string{"-l", "-a", "-h", "-R", "-t", "-S", "-1", "-d"}},
	{Command: "cat", AllowedFlags: []string{"-n", "-b", "-A"}},
	{Command: "head", AllowedFlags: []string{"-n", "-c"}},
	{Command: "tail", AllowedFlags: []string{"-n", "-c", "-f"}},
	{Command: "grep", AllowedFlags: []string{"-i", "-n", "-r", "-l", "-c", "-v", "-E", "-F"}},
	{Command: "rg", AllowedFlags: []string{"-i", "-n", "-l", "-c", "-v", "--type"}},
	{Command: "ag", AllowedFlags: []string{"-i", "-n", "-l", "-c", "-v"}},
	{Command: "find", AllowedFlags: []string{"-name", "-type", "-mtime", "-size", "-not", "-and", "-or", "-exec"}},
	{Command: "fd", AllowedFlags: []string{"-t", "-n", "-I", "-H", "-L", "-d"}},
	{Command: "git", AllowedFlags: []string{"status", "diff", "log", "show", "branch", "remote", "tag", "stash", " submodule", "grep"}},
	{Command: "pwd", AllowedFlags: []string{}},
	{Command: "cd", AllowedFlags: []string{}},
	{Command: "tree", AllowedFlags: []string{"-L", "-d", "-a", "-I"}},
}

func IsReadOnlyCommand(command string) bool {
	trimmed := strings.TrimSpace(command)

	for _, roc := range readOnlyCommands {
		if strings.HasPrefix(trimmed, roc.Command+" ") || trimmed == roc.Command {
			return true
		}
	}

	return false
}

func HasDangerousFlags(command string, allowedFlags []string) (bool, string) {
	dangerousFlagPatterns := []string{
		`-x\b`, `-X\b`, `--exec\b`,
		`-delete\b`,
		`-exec\b.*\;`,
		`-I\b`,
	}

	for _, pattern := range dangerousFlagPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			for _, allowed := range allowedFlags {
				if strings.Contains(command, allowed) {
					continue
				}
			}
			return true, pattern
		}
	}

	return false, ""
}
