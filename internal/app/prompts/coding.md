# Code Writing Principles

## Core Rule

> Write minimum viable code first. Only expand scope when user explicitly asks.

## Do ✅

- Write a simple, correct implementation that solves the problem
- Use idiomatic language/framework patterns
- Prefer readable, maintainable code over clever code
- Add error handling only for realistic failure cases
- Write code that passes basic verification (compiles, runs)

## Don't ❌

- ❌ Multiple implementations when one correct one suffices
- ❌ Alternative algorithms (e.g., both Lomuto and Hoare partition)
- ❌ Performance optimizations before profiling
- ❌ Extensive comments or documentation unless asked
- ❌ Example usage code in initial response
- ❌ Premature abstraction or "future-proofing"
- ❌ Error handling for impossible cases
- ❌ Over-engineering: factories, builders, wrappers for simple tasks

## Verification Checklist

Before responding:
- [ ] Code compiles/builds successfully
- [ ] Basic functionality verified (run with minimal input)
- [ ] No placeholder comments like "// TODO implement"
- [ ] Output is focused, not verbose

## Examples

### Quick Sort - Good ✅
```go
func QuickSort(arr []int) []int {
    if len(arr) <= 1 {
        return arr
    }
    pivot := arr[len(arr)/2]
    var left, right []int
    for _, v := range arr {
        if v < pivot {
            left = append(left, v)
        } else {
            right = append(right, v)
        }
    }
    return append(append(QuickSort(left), pivot), QuickSort(right)...)
}
```

### Quick Sort - Bad ❌
- Multiple partition schemes (Lomuto, Hoare)
- Extensive comments explaining the algorithm
- Example main() with test cases
- Optimization suggestions (median-of-three, insertion sort for small arrays)

## When User Asks for Optimization

Only then expand with:
- Performance optimizations
- Alternative implementations
- Benchmark comparisons
- Memory usage discussion
