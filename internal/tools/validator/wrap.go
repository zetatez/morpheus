package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type ValidatedTool struct {
	Tool     sdk.Tool
	Spec     sdk.ToolSpec
	validate bool
}

func NewValidatedTool(tool sdk.Tool, spec sdk.ToolSpec) *ValidatedTool {
	return &ValidatedTool{
		Tool:     tool,
		Spec:     spec,
		validate: spec != nil && spec.Schema() != nil,
	}
}

func (t *ValidatedTool) Name() string {
	return t.Tool.Name()
}

func (t *ValidatedTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	if t.validate && t.Spec != nil {
		schema := t.Spec.Schema()
		if schema != nil {
			validator := NewValidator(schema)
			if err := validator.Validate(input); err != nil {
				return sdk.ToolResult{
					Success: false,
					Error:   err.Error(),
				}, err
			}
		}
	}
	return t.Tool.Invoke(ctx, input)
}

type ToolWithSchema interface {
	sdk.Tool
	sdk.ToolSpec
}

func WrapIfNeeded(tool sdk.Tool) sdk.Tool {
	if ts, ok := tool.(ToolWithSchema); ok {
		return NewValidatedTool(tool, ts)
	}
	if spec, ok := tool.(sdk.ToolSpec); ok {
		return NewValidatedTool(tool, spec)
	}
	return tool
}

type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
}

func ValidateInput(schema map[string]any, input map[string]any) ValidationResult {
	if schema == nil {
		return ValidationResult{Valid: true}
	}
	v := NewValidator(schema)
	err := v.Validate(input)
	if err == nil {
		return ValidationResult{Valid: true}
	}
	result := ValidationResult{Valid: false}
	if errs, ok := err.(ValidationErrors); ok {
		result.Errors = errs
	} else {
		result.Errors = []ValidationError{{Message: err.Error()}}
	}
	return result
}

func FormatValidationError(err error) string {
	if errs, ok := err.(ValidationErrors); ok {
		var parts []string
		for _, e := range errs {
			parts = append(parts, e.Error())
		}
		return fmt.Sprintf("Validation failed:\n  - %s", strings.Join(parts, "\n  - "))
	}
	return fmt.Sprintf("Validation failed: %s", err.Error())
}
