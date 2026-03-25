package policy

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/pkg/sdk"
)

var levelOrder = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

type Engine struct {
	cfg                 config.Config
	compiledRiskFactors []compiledFactor
	compiledPaths       []compiledPath
}

type compiledFactor struct {
	pattern *regexp.Regexp
	level   string
}

type compiledPath struct {
	prefix  string
	pattern *regexp.Regexp
}

func NewPolicyEngine(cfg config.Config) *Engine {
	e := &Engine{cfg: cfg}
	e.compile()
	return e
}

func (e *Engine) compile() {
	for level, patterns := range e.cfg.Permissions.RiskFactors {
		for _, p := range patterns {
			if m, err := regexp.Compile("(?i)" + p); err == nil {
				e.compiledRiskFactors = append(e.compiledRiskFactors, compiledFactor{
					pattern: m,
					level:   level,
				})
			}
		}
	}

	home, _ := os.UserHomeDir()
	for _, p := range e.cfg.Permissions.ConfirmProtectedPaths {
		p = strings.ReplaceAll(p, "~", home)
		if m, err := regexp.Compile("^" + regexp.QuoteMeta(p)); err == nil {
			e.compiledPaths = append(e.compiledPaths, compiledPath{
				prefix:  p,
				pattern: m,
			})
		}
	}
}

func (e *Engine) EvaluateCommand(ctx context.Context, command, workdir string) sdk.PolicyDecision {
	if e.cfg.Permissions.AutoApprove {
		return sdk.PolicyDecision{
			Allowed:         true,
			RiskLevel:       sdk.RiskLow,
			Action:          sdk.ActionAllow,
			RequiresConfirm: false,
		}
	}

	factorLevel, matchedFactors := e.evaluateCommand(command)
	pathLevel := e.evaluatePath(workdir)

	decision := sdk.PolicyDecision{
		MatchedFactors: matchedFactors,
	}

	actualLevel := factorLevel
	if levelOrder[pathLevel] > levelOrder[actualLevel] {
		actualLevel = pathLevel
	}

	decision.RiskLevel = e.stringToLevel(actualLevel)

	if pathLevel != "" {
		decision.RequiresConfirm = true
		decision.Reason = "path in protected list: " + workdir
		decision.Action = sdk.ActionConfirm
		decision.Allowed = true
		return decision
	}

	if factorLevel == "" {
		decision.Allowed = true
		decision.Action = sdk.ActionAllow
		decision.RiskLevel = sdk.RiskLow
		return decision
	}

	threshold := e.cfg.Permissions.ConfirmAbove
	if threshold == "" {
		threshold = "high"
	}

	if levelOrder[factorLevel] >= levelOrder[threshold] {
		decision.RequiresConfirm = true
		decision.Reason = "risk level " + factorLevel + " >= " + threshold
		decision.Action = sdk.ActionConfirm
		decision.Allowed = true
	} else {
		decision.Allowed = true
		decision.Action = sdk.ActionAllow
	}

	return decision
}

func (e *Engine) evaluateCommand(command string) (string, []string) {
	var maxLevel string
	var matched []string

	for _, f := range e.compiledRiskFactors {
		if f.pattern.MatchString(command) {
			if levelOrder[f.level] > levelOrder[maxLevel] {
				maxLevel = f.level
			}
			matched = append(matched, f.pattern.String())
		}
	}

	return maxLevel, matched
}

func (e *Engine) evaluatePath(path string) string {
	if path == "" {
		return ""
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return ""
	}

	for _, p := range e.compiledPaths {
		if p.pattern.MatchString(absPath) || strings.HasPrefix(absPath, p.prefix) {
			return "critical"
		}
	}

	return ""
}

func (e *Engine) stringToLevel(level string) sdk.RiskLevel {
	switch level {
	case "critical":
		return sdk.RiskCritical
	case "high":
		return sdk.RiskHigh
	case "medium":
		return sdk.RiskMedium
	case "low":
		return sdk.RiskLow
	default:
		return sdk.RiskLow
	}
}
