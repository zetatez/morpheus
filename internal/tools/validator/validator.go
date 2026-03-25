package validator

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

type ValidationErrors []ValidationError

func (es ValidationErrors) Error() string {
	if len(es) == 0 {
		return ""
	}
	if len(es) == 1 {
		return es[0].Error()
	}
	var msgs []string
	for _, e := range es {
		msgs = append(msgs, e.Error())
	}
	return strings.Join(msgs, "; ")
}

func (es ValidationErrors) HasErrors() bool {
	return len(es) > 0
}

type Validator struct {
	schema map[string]any
}

func NewValidator(schema map[string]any) *Validator {
	return &Validator{schema: schema}
}

func (v *Validator) Validate(input map[string]any) error {
	var errs ValidationErrors

	properties, _ := v.schema["properties"].(map[string]any)
	if properties == nil {
		return nil
	}

	required, _ := v.schema["required"].([]any)
	requiredFields := make(map[string]bool)
	if required != nil {
		for _, r := range required {
			if s, ok := r.(string); ok {
				requiredFields[s] = true
			}
		}
	}

	for field, schema := range properties {
		value, exists := input[field]
		if !exists || value == nil {
			if requiredFields[field] {
				errs = append(errs, ValidationError{Path: field, Message: "required"})
			}
			continue
		}
		if err := v.validateValue(field, value, schema); err != nil {
			errs = append(errs, err.(ValidationError))
		}
	}

	if errs.HasErrors() {
		return errs
	}
	return nil
}

func (v *Validator) validateValue(path string, value any, schema any) error {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return nil
	}

	fieldType, _ := schemaMap["type"].(string)
	enum, hasEnum := schemaMap["enum"].([]any)

	switch fieldType {
	case "string":
		if err := v.validateString(path, value, schemaMap); err != nil {
			return err
		}
	case "integer", "number":
		if err := v.validateNumber(path, value, schemaMap); err != nil {
			return err
		}
	case "boolean":
		if err := v.validateBoolean(path, value); err != nil {
			return err
		}
	case "array":
		if err := v.validateArray(path, value, schemaMap); err != nil {
			return err
		}
	case "object":
		if err := v.validateObject(path, value, schemaMap); err != nil {
			return err
		}
	}

	if hasEnum {
		if !v.isInEnum(value, enum) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must be one of: %v", enum)}
		}
	}

	return nil
}

func (v *Validator) validateString(path string, value any, schema map[string]any) error {
	str, ok := value.(string)
	if !ok {
		return ValidationError{Path: path, Message: fmt.Sprintf("expected string, got %T", value)}
	}

	if minLen, ok := schema["minLength"].(float64); ok {
		if int64(len(str)) < int64(minLen) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must be at least %d characters", int64(minLen))}
		}
	}

	if maxLen, ok := schema["maxLength"].(float64); ok {
		if int64(len(str)) > int64(maxLen) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must be at most %d characters", int64(maxLen))}
		}
	}

	if pattern, ok := schema["pattern"].(string); ok {
		if !matchPattern(str, pattern) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must match pattern: %s", pattern)}
		}
	}

	return nil
}

func (v *Validator) validateNumber(path string, value any, schema map[string]any) error {
	num, err := toFloat64(value)
	if err != nil {
		if schema["type"] == "integer" {
			return ValidationError{Path: path, Message: fmt.Sprintf("expected integer, got %T", value)}
		}
		return ValidationError{Path: path, Message: fmt.Sprintf("expected number, got %T", value)}
	}

	if schema["type"] == "integer" {
		if num != float64(int64(num)) {
			return ValidationError{Path: path, Message: "must be an integer"}
		}
	}

	if min, ok := schema["minimum"].(float64); ok {
		if num < min {
			return ValidationError{Path: path, Message: fmt.Sprintf("must be >= %v", min)}
		}
	}

	if min, ok := schema["exclusiveMinimum"].(float64); ok {
		if num <= min {
			return ValidationError{Path: path, Message: fmt.Sprintf("must be > %v", min)}
		}
	}

	if max, ok := schema["maximum"].(float64); ok {
		if num > max {
			return ValidationError{Path: path, Message: fmt.Sprintf("must be <= %v", max)}
		}
	}

	if max, ok := schema["exclusiveMaximum"].(float64); ok {
		if num >= max {
			return ValidationError{Path: path, Message: fmt.Sprintf("must be < %v", max)}
		}
	}

	return nil
}

func (v *Validator) validateBoolean(path string, value any) error {
	if _, ok := value.(bool); !ok {
		return ValidationError{Path: path, Message: fmt.Sprintf("expected boolean, got %T", value)}
	}
	return nil
}

func (v *Validator) validateArray(path string, value any, schema map[string]any) error {
	arr, ok := value.([]any)
	if !ok {
		return ValidationError{Path: path, Message: fmt.Sprintf("expected array, got %T", value)}
	}

	if minItems, ok := schema["minItems"].(float64); ok {
		if int64(len(arr)) < int64(minItems) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must have at least %d items", int64(minItems))}
		}
	}

	if maxItems, ok := schema["maxItems"].(float64); ok {
		if int64(len(arr)) > int64(maxItems) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must have at most %d items", int64(maxItems))}
		}
	}

	if items, ok := schema["items"].(map[string]any); ok {
		for i, item := range arr {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			if err := v.validateValue(itemPath, item, items); err != nil {
				return err
			}
		}
	}

	return nil
}

func (v *Validator) validateObject(path string, value any, schema map[string]any) error {
	obj, ok := value.(map[string]any)
	if !ok {
		return ValidationError{Path: path, Message: fmt.Sprintf("expected object, got %T", value)}
	}

	if minProps, ok := schema["minProperties"].(float64); ok {
		if int64(len(obj)) < int64(minProps) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must have at least %d properties", int64(minProps))}
		}
	}

	if maxProps, ok := schema["maxProperties"].(float64); ok {
		if int64(len(obj)) > int64(maxProps) {
			return ValidationError{Path: path, Message: fmt.Sprintf("must have at most %d properties", int64(maxProps))}
		}
	}

	if properties, ok := schema["properties"].(map[string]any); ok {
		for propName, propSchema := range properties {
			if propValue, exists := obj[propName]; exists {
				propPath := path + "." + propName
				if err := v.validateValue(propPath, propValue, propSchema); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (v *Validator) isInEnum(value any, enum []any) bool {
	for _, e := range enum {
		if reflect.DeepEqual(e, value) {
			return true
		}
	}
	return false
}

func toFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case int:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", value)
	}
}

func matchPattern(s, pattern string) bool {
	regex, err := regexpCompile(pattern)
	if err != nil {
		return strings.Contains(s, pattern)
	}
	return regex.MatchString(s)
}

var regexpCompile = func(pattern string) (interface{ MatchString(string) bool }, error) {
	return &simplePattern{pattern: pattern}, nil
}

type simplePattern struct {
	pattern string
}

func (p *simplePattern) MatchString(s string) bool {
	return strings.Contains(s, p.pattern)
}
