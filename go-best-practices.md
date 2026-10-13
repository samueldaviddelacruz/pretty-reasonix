# Go Best Practices — the 100 mistakes, as rules

Distilled from *100 Go Mistakes and How to Avoid Them* (Teiva Harsanyi, Manning 2022).
Each rule is one documented mistake, in the book's own numbering (`[#37]`). Apply these
as defaults when writing or reviewing Go code.

How to use this file: treat every bullet as a default. When it conflicts with the
existing codebase's established style, follow the codebase and flag the conflict rather
than silently diverging. Rules marked *(perf)* only matter on hot paths — never trade
readability for them without a measurement.

Before reporting a violation, check that the code doesn't already satisfy the rule a few
lines away. The most common false positive is flagging work that has already been done,
and a report padded with those is a report nobody finishes reading.

The numbers are stable and are meant to be cited ("is `[#54]` a finding here?"). Rules may
be reworded or given exceptions, but a number is never reused or renumbered; new rules are
appended past 100.

## 2 · Code and project organization

- **[#1] Don't shadow variables by accident.** Use `=` to assign to an outer variable; `:=` in an inner block silently declares a new one. Declare `err` once and reuse it.
- **[#2] Avoid unnecessary nesting.** Return early and keep the happy path at the left edge. When an `if` block returns, omit the `else` entirely.
- **[#3] Avoid `init` functions.** Prefer explicit initialization from `main` via a dedicated function. `init` hides ordering/side effects, complicates testing, and can't return an error.
- **[#4] Don't add getters/setters that only relay a field.** If you add them, name them `Balance()` / `SetBalance()` — never `GetBalance()`. Be pragmatic; Go favors simplicity over ceremony.
- **[#5] Don't create interfaces speculatively.** Abstractions should be discovered, not designed up front. Create an interface when a real need appears; if it isn't clear how it improves the code, delete it.
- **[#6] Define interfaces on the consumer side, not the producer.** Expose concrete types from your package and let clients decide what abstraction they need. If you must export one, keep it minimal.
- **[#7] Return concrete types, not interfaces.** Returning an interface restricts all clients to one abstraction and creates odd package dependencies. Exceptions: `error`, and up-front abstractions such as `io.Reader`.
- **[#8] Avoid `any` where it says nothing.** Make signatures explicit with a concrete type or a meaningful interface. It is justified when the openness is real: marshaling/formatting, satisfying a generic constraint, or matching an existing signature you must interoperate with (`json.Marshal`, `sort.Slice`).
- **[#9] Don't reach for generics by default.** Use type parameters when they remove real duplication or enforce a constraint; a concrete `io.Writer` beats a generic one. Unnecessary abstraction is just complexity.
- **[#10] Embed types deliberately.** Embedding promotes fields and methods into your public API. Don't embed purely to shorten `Foo.Bar.Baz()` to `Foo.Baz()`, and remember that adding a method to an embedded type later is a breaking change.
- **[#11] Use the functional options pattern for optional config.** A config struct forces callers through zero-value traps; a builder adds ceremony. Variadic `func(*options)` is the idiomatic Go answer.
- **[#12] Don't over-package early.** Being unsure whether to export something means don't export it. Keep naming consistent, and favor fewer, well-named packages over a speculative layout.
- **[#13] Never create `utils`, `common`, or `base` packages.** The name tells the reader nothing. Use an expressive package name (`stringset`) or a specific type with methods.
- **[#14] Avoid collisions between variable names and package names or builtins.** Rename the variable, or use an import alias.
- **[#15] Document every exported element.** Start package docs with `// Package x ...` in `doc.go` or a same-named file. Document what it does and why, not how.
- **[#16] Always lint and format.** Use `gofmt`/`goimports` plus `golangci-lint`, automated in CI or a pre-commit hook. Don't hand over unlinted code.

## 3 · Data types

- **[#17] Write octal literals explicitly.** Use `0o644`, never `0644`. A bare leading zero means octal and reads as a typo to everyone else.
- **[#18] Integer overflow is silent in Go.** When a value can overflow, check bounds with `math.MaxInt`/`math.MinInt` before the operation, and use `math/big` for genuinely large numbers.
- **[#19] Never compare floats with `==`.** Compare the absolute difference against an epsilon, and note that the order of operations changes the result — group operands of similar magnitude.
- **[#20] Know slice length vs capacity.** `make([]T, len, cap)`: `len` is mandatory and is how many elements get initialized; `cap` is optional and is how much is allocated. `append` uses spare capacity, and reallocates+**copies** once it's exhausted.
- **[#21] Preallocate slices when you know the size.** `make([]T, 0, n)` with `append`, or `make([]T, n)` and assign by index — the latter avoids the per-call `append` overhead. Growing by repeated `append` is what's expensive.
- **[#22] Prefer a nil slice over an empty one when returning nothing.** `var s []T` needs no allocation; `[]T{}` does. `len(s) == 0` holds for both, but some libraries distinguish them — check when consuming them.
- **[#23] Check emptiness with `len(s) == 0`,** never `s == nil`. A callee may legitimately have returned an empty non-nil slice.
- **[#24] `copy(dst, src)` takes the destination first,** and copies `min(len(dst), len(src))` elements. Size the destination as `make([]T, len(src))` unless you deliberately want a partial copy.
- **[#25] `append` can mutate slices you didn't mean to touch.** Two slices can share a backing array, so appending to one can overwrite the other. Use the full slice expression `s[a:b:b]` or an explicit copy to isolate.
- **[#26] Reslicing pins the whole backing array.** Keeping a small sub-slice of a large one keeps all of it alive; copy out what you need. Same for pointer fields you no longer use — nil them so the GC can collect.
- **[#27] Preallocate maps too.** `make(map[K]V, n)` when the entry count is known avoids repeated growth (the book measures ~60% faster for 1M inserts).
- **[#28] A map only grows — removing entries doesn't free the buckets.** If a map grows large and then shrinks, recreate it. Using `map[K]*V` shrinks per-bucket cost when values are large.
- **[#29] `==` is limited and will panic on slices, maps, and funcs,** even when it compiles for structs containing them. Use `reflect.DeepEqual` for arbitrary values, or a purpose-built comparison (`bytes.Compare`) — don't hand-roll one before checking the stdlib.

## 4 · Control structures

- **[#30] The range loop's value is a copy.** Mutating it doesn't touch the collection. Iterate by index (`for i := range s { s[i] = ... }`) to modify, or store pointers.
- **[#31] The ranged expression is evaluated once, before the loop** (by copy, for every type). Reassigning the variable inside the loop has no effect; `for range ch` keeps the channel captured at loop start.
- **[#32] Don't take the address of a range variable.** All iterations reuse one variable with a single address, so `&v` is the same pointer every time. Copy it (`v := v`) or use the index.
- **[#33] Never rely on map iteration order** — it is unspecified and deliberately randomized. Sort the keys, or use an ordered structure. Entries added during iteration may or may not be visited.
- **[#34] `break` exits the innermost `for`, `switch`, or `select`,** not an enclosing loop. Use a label when you need to break out of the loop itself.
- **[#35] Don't `defer` inside a loop.** Deferred calls run at *function* return, so they pile up (open file descriptors, held locks). Extract the loop body into a function, or close explicitly.

## 5 · Strings

- **[#36] A rune is a Unicode code point (`int32`); a Go string is bytes.** Strings are usually — but not guaranteed to be — UTF-8, and `len(s)` counts bytes, not characters.
- **[#37] Iterate string runes with `for _, r := range s`.** `s[i]` is a byte and the index is a byte offset, not a rune index. To get the i-th rune, convert to `[]rune` first.
- **[#38] Don't confuse trim functions.** `TrimRight`/`TrimLeft` strip a *set* of characters; `TrimSuffix`/`TrimPrefix` strip one exact string. They are not interchangeable.
- **[#39] Use `strings.Builder` for concatenation in loops** (with `Grow` when the final size is known). For two or three strings, `+=` or `fmt.Sprintf` is clearer and the difference is noise.
- **[#40] Don't bounce between `string` and `[]byte`.** If a workflow is byte-oriented (I/O, parsing), keep it as `[]byte` end to end instead of paying for conversions at each step.
- **[#41] A substring shares the original backing array.** A small substring of a huge string keeps the whole string alive. Copy it (`strings.Clone` since Go 1.18) when you keep only a small part.

## 6 · Functions and methods

- **[#42] Use a pointer receiver when the method mutates the receiver,** the receiver is large, or it holds a non-copyable field (`sync.Mutex`) — and keep that choice consistent for the methods that share the reason. A type whose mutating methods take pointers while its read-only methods take values is not a defect; the mix itself is not a finding.
- **[#43] Use named results sparingly and only for clarity** (e.g. you `append` to a result, or a `defer` must set the error). Avoid naked returns in anything but very short functions, and be consistent within a function.
- **[#44] Named results are zero-initialized and mutable,** so a reassigned or shadowed named result escapes unexpectedly, and `return x` overwrites it. Be deliberate about which you mean.
- **[#45] A nil pointer in an interface is not a nil interface.** Returning a typed nil pointer makes `err != nil` true. Return an untyped nil, or handle it explicitly.
- **[#46] Accept `io.Reader`/`io.Writer`, not filenames.** Opening files inside a function makes it untestable in memory. Let the caller own the I/O.
- **[#47] `defer` evaluates its arguments immediately,** at the `defer` statement — including method receivers — while a deferred *closure* reads variables when it finally runs. Use a closure or a pointer when you need the later value.

## 7 · Error management

- **[#48] `panic` is for programmer errors only** (invalid literal, failing to initialize a required dependency, registering a driver twice). Return `error` for everything else, including startup problems you can report. `recover` is only useful inside a `defer`.
- **[#49] Choose `%w` vs `%v` deliberately — when formatting an error.** `fmt.Errorf("...: %w", err)` adds context and keeps the chain matchable; `%v` *transforms* and deliberately breaks it. Use a custom type when callers must match on the error. This applies to error values only: `%v` on an ordinary value is not a finding.
- **[#50] Check error types with `errors.As`,** not a type assertion or `%T` switch — assertions fail on wrapped errors.
- **[#51] Compare error *identity* with `errors.Is`,** never `==` — `==` fails as soon as the error is wrapped with `%w`. The nil check is the deliberate exception: `err == nil` / `err != nil` is the correct, idiomatic form here, and rewriting it is not a fix.
- **[#52] Handle an error exactly once: log it or return it, never both.** Doing both produces duplicate logs and loses the original context.
- **[#53] Never swallow an error implicitly** (`_ = f()` with no comment is a smell only when accidental). If an error can truly be ignored, ignore it *explicitly* and say why.
- **[#54] `defer`'d `Close`/`Flush` can fail.** Don't discard that error: return it through a named result, or handle it explicitly. Watch for a `defer` silently overwriting the error you meant to return.

## 8 · Concurrency: foundations

- **[#55] Concurrency is not parallelism.** Concurrency is how you structure independent work; parallelism is executing it simultaneously. They solve different problems — adding parallelism won't fix a bad structure.
- **[#56] Concurrency is not automatically faster.** A concurrent solution pays for scheduling and coordination and is often slower than the sequential one. Measure before assuming.
- **[#57] Channels and mutexes are complementary, not rivals.** Use channels to signal and to transfer ownership; use a mutex to guard shared state. Pick per problem, not per preference.
- **[#58] Distinguish data races from race conditions.** A data race is unsynchronized concurrent access with at least one write, and corrupts memory regardless of intent; a race condition is a timing/logic bug. Synchronization via channels, atomics, or mutexes is what establishes the happens-before guarantees of the Go memory model.
- **[#59] Derive the goroutine count from the workload type,** not habit: CPU-bound work is limited by parallel capacity, I/O-bound work by latency. Validate your assumption with a benchmark.
- **[#60] Understand `context.Context` before using it.** It carries deadlines, cancellation, and request-scoped values down the call chain; close the internal channel on cancellation or deadline, since closing is the only signal every consumer receives. When unsure which to use, prefer `context.TODO()` over an empty `context.Background`.

## 9 · Concurrency: practice

- **[#61] Propagate contexts deliberately.** Keep them request-scoped and flowing down the call chain — don't store them in structs or use them for optional parameters.
- **[#62] Every goroutine needs a known exit.** If you start one without a plan for stopping it, it leaks. Give it a cancellation signal (channel or context) and make sure resources are released.
- **[#63] Capture the loop variable before using it in a goroutine.** In Go < 1.22 all iterations share one variable, so a closure can read the wrong value; shadow it (`v := v`) or pass it as an argument. (Go 1.22+ makes loop variables per-iteration, and `go vet`'s `loopclosure` catches this — know which Go version the module targets.)
- **[#64] `select` picks randomly among ready cases.** Source order never wins, so don't build priority on it. Use a single channel/unbuffered channel when only one sender exists, or an inner `select` with `default` for real prioritization.
- **[#65] Use `chan struct{}` when a channel carries no data** — it signals only, and costs no allocation. Same idea for sets: `map[K]struct{}`.
- **[#66] A nil channel blocks forever** — deliberately. Setting a closed channel to nil removes its `select` case, which makes for a clean channel-merging state machine.
- **[#67] Default to unbuffered channels; use size 1 if you need buffering at all.** Any larger buffer needs a measured rationale and a comment, because buffering hides deadlocks and weakens back-pressure.
- **[#68] Be careful formatting values in concurrent code.** Reaching a `String()`/`%v` on a value that is concurrently mutated or holds a lock can race or deadlock. Format from an immutable copy.
- **[#69] `append` on a slice shared across goroutines is a data race,** even when each goroutine writes "its own" element — they share the backing array. Guard it, collect per-goroutine results and merge, or pass values over a channel.
- **[#70] A mutex must cover every read and write of the data it protects.** Don't hand a reference to protected data out and mutate it outside the lock. Either protect the whole function or operate on a copy.
- **[#71] `WaitGroup.Add` belongs in the parent goroutine, before starting the child;** `Done` belongs inside the child (`defer wg.Done()`). Getting this backwards is a classic race.
- **[#72] Reach for `sync.Cond` only for genuinely arbitrary conditions,** not one-shot signals. Always wait in a loop (`for !ok { c.Wait() }`), and pick `Signal()` for one waiter versus `Broadcast()` for many.
- **[#73] Use `golang.org/x/sync/errgroup` instead of hand-rolling a goroutine fan-out with error handling and cancellation.** The function passed to `g.Go` must be context-aware.
- **[#74] Never copy a type containing a `sync` value** (`Mutex`, `WaitGroup`, `Cond`, `sync.Map`). Pass pointers, and make sure every goroutine uses the same instance. `go vet` catches most of these.

## 10 · The standard library

- **[#75] Always use the `time` constants** (`time.Second`, `time.Millisecond`), never a bare integer, so the unit is unambiguous at the call site.
- **[#76] Avoid `time.After` in loops or long-lived `select`s.** Its timer isn't collected until it fires. Use `time.NewTimer` and `defer timer.Stop()` instead.
- **[#77] Watch the JSON traps.** Embedded types affect marshaling; the monotonic clock breaks equality and round-trips; unmarshaling numbers into `map[string]any` yields `float64`, not `int` (use `json.Number`/`Decoder.UseNumber` when it matters).
- **[#78] `sql.Open` does not connect** — it only validates arguments; use `Ping` to verify. Configure the pool (`SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`), use prepared statements for repeated queries, handle NULL with `sql.Null*`/pointers, `Close` the `Stmt`, and always check `rows.Err()` after iterating.
- **[#79] Close every transient resource** — HTTP response bodies (even on error paths), `sql.Rows`, `os.File`. `defer` the close immediately after a successful acquire, and `Sync`/`Flush` when durability matters.
- **[#80] Add `return` after replying to an HTTP request.** `http.Error` and `w.Write` don't stop the handler, so execution continues and can write a second response.
- **[#81] Never ship the default HTTP client or server.** They have no timeouts and leak connections. Configure an `http.Client` with an explicit `Timeout` and a tuned `http.Transport`, and an `http.Server` with read/write/idle timeouts.

## 11 · Testing

- **[#82] Categorize tests** (unit / integration / e2e) with build tags, environment variables, or `testing.Short()`, so the cheap suite can run on every commit.
- **[#83] Run concurrent code under `-race`** in local and CI runs. It's expensive, so schedule it deliberately rather than enabling it unconditionally — and see [#82]: narrowing the suite is usually what makes a `-race` run affordable.
- **[#84] Use the test execution flags.** `t.Parallel()`, `-shuffle` (surfaces order-dependent tests), `-count`, `-run`. Know what exists rather than assuming the defaults are all there is.
- **[#85] Use table-driven tests when cases share a structure.** Pre-1.22, shadow the loop variable (`tt := tt`) inside the closure — the same trap as [#63].
- **[#86] Never `time.Sleep` to fix a flaky test.** Synchronize instead (channels, `select` with a `time.After` timeout), or retry with a bounded budget. If synchronization is impossible, the design is the problem.
- **[#87] Don't call `time.Now()` deep inside untestable code.** Inject the clock as a dependency, or have the caller pass the current time.
- **[#88] Use the testing utility packages** — `net/http/httptest`, `testing/iotest` — instead of hand-rolled fakes.
- **[#89] Benchmark rather than guess, and benchmark correctly.** `b.ResetTimer()` around setup, recreate inputs each iteration to avoid the observer effect, make sure the compiler can't optimize the work away, and check `b.ReportAllocs()`. Micro-benchmarks of CPU-bound code are especially easy to fool.
- **[#90] Know the testing toolkit:** code coverage, `t.Cleanup` for teardown, `t.Helper()` in helpers, and an external `_test` package to test behavior rather than internals.

## 12 · Optimizations *(perf — measure first)*

- **[#91] Understand CPU caches and access patterns.** Data is loaded per cache line, so spatial locality dominates; power-of-two "critical strides" can thrash the cache sets, and slice-of-structs vs struct-of-slices is a real trade-off. Treat micro-benchmark numbers as machine-specific.
- **[#92] Watch for false sharing.** Two goroutines writing different variables that share a cache line invalidate each other. Pad the fields, or communicate results over channels instead of sharing memory.
- **[#93] Give the CPU independent work to overlap.** Restructuring independent operations so out-of-order execution can run them in parallel is often a bigger win than shaving instructions.
- **[#94] Order struct fields largest-first to minimize padding.** A struct's size is rounded up to a multiple of the word size, so field order can cost or save real memory (e.g. 24 → 16 bytes).
- **[#95] Prefer stack allocation, and don't optimize allocations blindly.** Returning a pointer "to avoid a copy" is not automatically faster. Check escape analysis (`-gcflags=-m`) and `b.ReportAllocs()`, and fix readability and semantics first.
- **[#96] Reduce allocations where it's measured.** Let APIs write into a caller-provided buffer, check whether the compiler already eliminated the allocation, and use `sync.Pool` for many short-lived identical objects — it is not a cache and has no size or capacity limit.
- **[#97] You can't force inlining, but you can allow it.** Keep functions within the compiler's inlining budget, and split fast/slow paths so the hot path can be inlined while the slow path lives in its own function.
- **[#98] Use `pprof` and the execution tracer** rather than guessing where time goes. Enable only one profiler at a time, and keep `pprof` available in production — its footprint is usually worth the insight.
- **[#99] Tune the GC with `GOGC`** when allocation behavior is understood, and know the heap peak so you can stabilize it. A burst of growth (e.g. scaling fast) triggers GC far more often than steady state.
- **[#100] Go is not CFS-aware.** `GOMAXPROCS` sees the host's CPUs, not the container's CPU quota, so a CPU-limited container gets throttled. Use `go.uber.org/automaxprocs` (blank import) to match the quota.

## Notes since the book was published

The rules above reflect Go ≤ 1.18 semantics. Three things have moved:

- **Go 1.22** made `for` loop variables per-iteration, so `[#63]`/`[#85]`'s shadowing workaround is no longer needed (and `go vet` flags it as redundant in modules targeting ≥ 1.22).
- **Go 1.19** added `GOMEMLIMIT`, a soft memory limit that complements `GOGC` from `[#99]` — often the better knob for containerized services.
- **Go 1.20** added `errors.Join`, useful for the multi-error case in `[#45]`.
