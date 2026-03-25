# Refactoring Principles

## Golden Rule

> Refactoring changes code structure without changing behavior

## Before Refactoring

- [ ] Tests exist and pass
- [ ] Code committed to version control
- [ ] Understand current behavior
- [ ] Have clear refactoring goal

## During Refactoring

- [ ] Make one change at a time
- [ ] Run tests after each change
- [ ] Commit after each successful change
- [ ] Don't mix refactoring with feature changes

## Common Refactorings

### Extract Method
Split long methods into focused, single-purpose functions.

### Replace Magic Numbers
```go
// Bad
if status == 1 { ... }

// Good
const StatusActive = 1
if status == StatusActive { ... }
```

### Introduce Parameter Object
```go
// Bad
func createUser(name, email, age, address, phone string) { }

// Good
func createUser(user UserData) { }
```

## Anti-Patterns ❌

- ❌ Refactor without tests
- ❌ Refactor without clear benefit
- ❌ Change behavior while refactoring (that's a feature)
- ❌ Huge changes without commits
- ❌ Optimize prematurely
- ❌ Refactor code you don't understand

## Strangler Fig Pattern

For legacy code: replace piece by piece, don't rewrite everything at once.
