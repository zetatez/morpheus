# Testing Principles

## TDD Cycle (Red-Green-Refactor)

1. **Red**: Write failing test
2. **Green**: Write minimum code to pass
3. **Refactor**: Improve code while tests pass

## What to Test

- Public API / interface
- Business logic
- Edge cases (null, empty, boundaries)
- Error handling
- State changes

## What NOT to Test

- ❌ Private methods
- ❌ Third-party code
- ❌ Framework internals
- ❌ Trivial getters/setters

## Test Structure

```
describe('function name', () => {
  it('should do X when Y', () => {
    // Arrange
    const input = ...
    // Act
    const result = functionUnderTest(input)
    // Assert
    expect(result).toBe(...)
  })
})
```

## Edge Cases to Test

- Null/undefined inputs
- Empty arrays/strings
- Boundary values (0, -1, max, min)
- Network failures
- Invalid data

## Anti-Patterns ❌

- ❌ Test without verifying the fix works
- ❌ Over-mocking real behavior
- ❌ Tests that depend on each other
- ❌ 100% coverage as goal (focus on critical paths)
