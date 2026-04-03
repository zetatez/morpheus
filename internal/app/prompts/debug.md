# Debugging Workflow

Use when: user reports a bug, unexpected output, or flaky behavior.

## Debugging Steps

### 1. Frame the Problem
- Write one-sentence bug statement
- Define acceptance test (how we know it's fixed)

### 2. Reproduce
- Create minimal repro (smallest input, clean env, single command)
- If flaky: quantify (e.g., 1/20), then stabilize

### 3. Localize (Fast Narrowing)
- Binary search by disabling halves of system
- Reduce degrees of freedom: hardcode inputs, freeze time
- Compare baseline vs current: recent changes, config diffs

### 4. Instrument
- Add single high-signal log near boundary (inputs/outputs, invariants)
- Use assertions for invariants

### 5. Fix
- **Make one change at a time; keep it minimal**
- Add/adjust test that fails before and passes after

### 6. Verify
- Run narrowest tests first, then broader suite
- Add regression test or guardrail

## Tactics

- Use `grep` to locate call sites and error strings
- Use `glob` to find candidate files
- Use `read` to inspect files and `apply_patch` for small edits
- Use `bash` for running repro commands and tests

## Common Pitfalls ❌

- Random changes without hypothesis
- Multiple changes at once (can't attribute effect)
- "Fixed" without executable reproduction
