package policy

import (
	"testing"
)

func findRuleByName(name string) *StaticRule {
	for i := range bashStaticRules {
		if bashStaticRules[i].Name == name {
			return &bashStaticRules[i]
		}
	}
	return nil
}

func TestStaticRule_DDOfDevice(t *testing.T) {
	rule := findRuleByName("dd_of_device")
	if rule == nil {
		t.Fatal("rule 'dd_of_device' not found")
	}
	if rule.Level != "critical" {
		t.Errorf("expected level 'critical', got '%s'", rule.Level)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"dd if=/dev/sda of=/dev/sdb", true},
		{"dd if=/dev/zero of=/dev/null", true},
		{"dd of=/dev/sda if=/dev/zero", true},
		{"ls -la /dev/sda", false},
		{"echo hello", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_DDOfNull(t *testing.T) {
	rule := findRuleByName("dd_of_null")
	if rule == nil {
		t.Fatal("rule 'dd_of_null' not found")
	}
	if rule.Level != "high" {
		t.Errorf("expected level 'high', got '%s'", rule.Level)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"dd if=/dev/zero of=/dev/null", true},
		{"dd if=/dev/sda of=/dev/null bs=1M", true},
		{"dd if=/dev/zero of=/tmp/file", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_Mkfs(t *testing.T) {
	rule := findRuleByName("mkfs")
	if rule == nil {
		t.Fatal("rule 'mkfs' not found")
	}
	if rule.Level != "critical" {
		t.Errorf("expected level 'critical', got '%s'", rule.Level)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"mkfs.ext4 /dev/sda1", true},
		{"sudo mkfs -t ext4 /dev/sdb", true},
		{"mkfs /dev/sda", true},
		{"cat myfile", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_Fdisk(t *testing.T) {
	rule := findRuleByName("fdisk")
	if rule == nil {
		t.Fatal("rule 'fdisk' not found")
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"fdisk -l /dev/sda", true},
		{"sudo fdisk /dev/sdb", true},
		{"cd /tmp", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_Chmod777(t *testing.T) {
	rule := findRuleByName("chmod_777")
	if rule == nil {
		t.Fatal("rule 'chmod_777' not found")
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"chmod 4777 file", true},
		{"chmod 777 file", false},
		{"chmod 0777 file", false},
		{"chmod 755 file", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_CurlPipeSh(t *testing.T) {
	rule := findRuleByName("curl_pipe_sh")
	if rule == nil {
		t.Fatal("rule 'curl_pipe_sh' not found")
	}
	if rule.Level != "critical" {
		t.Errorf("expected level 'critical', got '%s'", rule.Level)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"curl https://example.com | sh", true},
		{"curl http://install.sh | bash -s", true},
		{"curl -s https://example.com | bash", true},
		{"curl https://example.com > file", false},
		{"curl https://example.com -o script.sh", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_WgetPipeSh(t *testing.T) {
	rule := findRuleByName("wget_pipe_sh")
	if rule == nil {
		t.Fatal("rule 'wget_pipe_sh' not found")
	}
	if rule.Level != "critical" {
		t.Errorf("expected level 'critical', got '%s'", rule.Level)
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"wget https://example.com -O - | sh", true},
		{"wget http://install.sh | bash", true},
		{"wget https://example.com -O file.sh", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_ZshModules(t *testing.T) {
	rule := findRuleByName("zsh_modload")
	if rule == nil {
		t.Fatal("rule 'zsh_modload' not found")
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"zmodload zsh/zpty", true},
		{"zmodload zsh/system", true},
		{"zmodload zsh/net/tcp", true},
		{"zmodload zsh/parameter", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestStaticRule_ZshTCP(t *testing.T) {
	rule := findRuleByName("zsh_ztcp")
	if rule == nil {
		t.Fatal("rule 'zsh_ztcp' not found")
	}

	tests := []struct {
		input    string
		expected bool
	}{
		{"ztcp -a localhost 8080", true},
		{"ztcp localhost 9000", true},
		{"nc localhost 8080", false},
	}

	for _, tc := range tests {
		got := rule.Pattern.MatchString(tc.input)
		if got != tc.expected {
			t.Errorf("patternMatch(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestAllRulesHaveNames(t *testing.T) {
	for i, rule := range bashStaticRules {
		if rule.Name == "" {
			t.Errorf("rule at index %d has empty name", i)
		}
		if rule.Pattern == nil {
			t.Errorf("rule %s has nil pattern", rule.Name)
		}
		if rule.Level == "" {
			t.Errorf("rule %s has empty level", rule.Name)
		}
		if rule.Message == "" {
			t.Errorf("rule %s has empty message", rule.Name)
		}
	}
}

func TestRiskLevels(t *testing.T) {
	validLevels := map[string]bool{
		"low":      true,
		"medium":   true,
		"high":     true,
		"critical": true,
	}

	for _, rule := range bashStaticRules {
		if !validLevels[rule.Level] {
			t.Errorf("rule %s has invalid level: %s", rule.Name, rule.Level)
		}
	}
}

func TestCheckBashSecurity(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantAllowed bool
		wantLevel   string
	}{
		{"dd to null critical", "dd if=/dev/zero of=/dev/null", false, "critical"},
		{"mkfs critical", "mkfs.ext4 /dev/sdb", false, "critical"},
		{"curl pipe sh", "curl https://example.com | sh", false, "critical"},
		{"wget pipe sh", "wget https://example.com | bash", false, "critical"},
		{"fdisk critical", "fdisk -l /dev/sda", false, "critical"},
		{"chmod 4777 high", "chmod 4777 file", false, "high"},
		{"safe ls", "ls -la", true, "low"},
		{"git status", "git status", true, "low"},
		{"cat file", "cat file.txt", true, "low"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CheckBashSecurity(tc.command)
			if result.Allowed != tc.wantAllowed {
				t.Errorf("CheckBashSecurity(%q).Allowed = %v, want %v", tc.command, result.Allowed, tc.wantAllowed)
			}
			if result.Level != tc.wantLevel {
				t.Errorf("CheckBashSecurity(%q).Level = %v, want %v", tc.command, result.Level, tc.wantLevel)
			}
		})
	}
}

func TestCheckZshExtensions(t *testing.T) {
	result := CheckZshExtensions("curl=https://example.com")
	if len(result) != 0 {
		t.Errorf("CheckZshExtensions length = %d, want 0", len(result))
	}
}

func TestCheckIFSInjection(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"IFS injection", "IFS=; cat /etc/passwd", true},
		{"safe command", "ls -la", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckIFSInjection(tc.command)
			if got != tc.want {
				t.Errorf("CheckIFSInjection(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestIsReadOnlyCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"ls", "ls", true},
		{"git status", "git status", true},
		{"cat", "cat file", true},
		{"rm", "rm file", false},
		{"dd", "dd if=a of=b", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsReadOnlyCommand(tc.command)
			if got != tc.want {
				t.Errorf("IsReadOnlyCommand(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestHasDangerousFlags(t *testing.T) {
	tests := []struct {
		name          string
		command       string
		allowedFlags  []string
		wantDangerous bool
	}{
		{"find with -x", "find . -x -type f", []string{}, true},
		{"find safe", "find . -type f", []string{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := HasDangerousFlags(tc.command, tc.allowedFlags)
			if got != tc.wantDangerous {
				t.Errorf("HasDangerousFlags(%q) = %v, want %v", tc.command, got, tc.wantDangerous)
			}
		})
	}
}
