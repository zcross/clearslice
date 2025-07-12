# clearslice

`clearslice` is a Go static analysis tool that detects slice resizing that can lead to memory leaks.

## Motivation

In Go, `s = s[:0]` sets a slice's length to zero but does not clear elements in the underlying array. If these elements are or transitively contain reference types (like pointers, maps, channels, interfaces), their references remain in the backing array. This prevents the garbage collector from reclaiming the objects they point to, potentially leading to memory leaks or unexpectedly long object liveness.

This issue is particularly relevant in performance-sensitive systems where buffer reuse is common. For a more detailed explanation and visuals, refer to the Go blog post: [Robust generic functions on slices](https://go.dev/blog/generic-slice-functions).

## What it does

The `clearslice` analyzer identifies assignments of the form `s = s[:0]` where `s` is a slice of a type that is or contains a reference type. This includes:
- Slices of pointers (`[]*T`)
- Slices of maps (`[]map[K]V`)
- Slices of channels (`[]chan T`)
- Slices of interfaces (`[]interface{}`)
- Slices of functions (`[]func()`)
- Slices of strings (`[]string`)
- Slices of structs that transitively contain any reference type fields.

The tool flags these occurrences and suggests a safer alternative. It correctly ignores slices of primitive types (e.g., `[]int`, `[]bool`) and structs composed solely of primitive types, for which this pattern is safe. The recommended replacement, `s = slices.Delete(s, 0, len(s))`, is chosen for its suitability as a one-line fix.

## Known Limitations and Future Improvements

The current detection pattern is simplistic. Potential areas for improvement include:
1.  Detecting when a slice is declared and used within a scope but does not escape (i.e., not directly or indirectly returned).
2.  Generalizing beyond the `x = x[:0]` pattern to cover more variations, if trivially detectable and fixable.

## Recommended Fixes

**Note: The recommended replacement using `slices.Delete` is only for Go 1.22+ environments.**

To fix the detected issue, the elements of the backing array must be explicitly cleared. The analyzer recommends `slices.Delete` from Go's standard library, which correctly clears the elements. Be aware that this operation is O(n) in the current length of the cleared slice.

If maintainers are certain about the safety of length-based resetting in specific cases, they can use `//nolint` to suppress the linter warning. Otherwise, performing the linear work with `slices.Delete` provides peace of mind regarding memory management.

## Example

When a slice of a reference type is resized without clearing, the underlying objects may not be garbage collected.

Before:
```go
package main

type MyStruct struct {
    data *[1024]byte // A field that holds a reference
}

func main() {
    // s contains pointers to large objects
    s := []MyStruct{{new([1024]byte)}, {new([1024]byte)}}

    // This sets the length to 0, but the backing array still holds
    // pointers to the MyStruct objects, preventing GC.
    s = s[:0]

    // ... do other work
    // The memory for the [1024]byte arrays is not reclaimed.
}
```

After:
```go
package main

import "slices"

type MyStruct struct {
    data *[1024]byte
}

func main() {
    s := []MyStruct{{new([1024]byte)}, {new([1024]byte)}}

    // This clears the slice elements from the backing array,
    // allowing the GC to reclaim the memory.
    s = slices.Delete(s, 0, len(s))

    // ... do other work
}
```