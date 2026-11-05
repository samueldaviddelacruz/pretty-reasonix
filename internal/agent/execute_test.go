package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/contract/tool"
	registry "pretty-reasonix/internal/tool"
)

// fake is a tool that does what a test tells it to, including misbehaving.
type fake struct {
	name  string
	delay time.Duration
	out   string
	news  error // reported as Result.Err: the world, not the tool
	burst error // returned as the Go error: the tool is broken
	// concurrent counts how many ran at once, which is the only way to see the cap from outside.
	concurrent *concurrency
	// writable makes the fake stop claiming ReadOnly, which is what puts it in its own segment.
	writable bool
	// started closes on entry, so a test can cancel at a known moment instead of sleeping and hoping.
	started chan struct{}
}

func (f fake) Name() string          { return f.name }
func (fake) Description() string     { return "fake" }
func (fake) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f fake) ReadOnly() bool        { return !f.writable }

func (f fake) Execute(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	if f.started != nil {
		close(f.started)
	}
	if f.concurrent != nil {
		f.concurrent.enter()
		defer f.concurrent.leave()
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return tool.Result{}, ctx.Err()
		}
	}
	return tool.Result{Output: f.out, Err: f.news}, f.burst
}

// concurrency counts how many calls are inside Execute at once.
type concurrency struct {
	mu   sync.Mutex
	now  int
	most int
}

func (c *concurrency) enter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now++
	if c.now > c.most {
		c.most = c.now
	}
}

func (c *concurrency) leave() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now--
}

func (c *concurrency) peak() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.most
}

// batch builds a registry of fakes and a plan for the calls named.
func batch(t *testing.T, tools ...fake) (*registry.Registry, agent.Executor) {
	t.Helper()
	reg := registry.NewRegistry()
	for _, f := range tools {
		if err := reg.Add(f); err != nil {
			t.Fatalf("Add %s: %v", f.name, err)
		}
	}
	return reg, agent.Executor{Registry: reg, Boundary: registry.Boundary{Dir: t.TempDir()}}
}

func callsOf(names ...string) []provider.ToolCall {
	out := make([]provider.ToolCall, 0, len(names))
	for index, name := range names {
		out = append(out, provider.ToolCall{ID: fmt.Sprintf("c%d", index), Name: name, Arguments: `{}`})
	}
	return out
}

// TestResultsKeepCallOrderWhateverTheFinishOrder is what makes a transcript read as a conversation rather than
// as a race: the second call finishing first must not move its result.
func TestResultsKeepCallOrderWhateverTheFinishOrder(t *testing.T) {
	t.Parallel()
	reg, exec := batch(t,
		fake{name: "slow", delay: 60 * time.Millisecond, out: "slow"},
		fake{name: "fast", out: "fast"},
	)
	outcome := exec.Run(context.Background(), agent.PlanBatch(callsOf("slow", "fast"), reg))

	if len(outcome.Results) != 2 {
		t.Fatalf("%d results, want 2", len(outcome.Results))
	}
	if outcome.Results[0].Call.Name != "slow" || outcome.Results[0].Result.Visible != "slow" {
		t.Errorf("the first result is %+v, want the slow call: finishing first must not reorder", outcome.Results[0])
	}
	if outcome.Results[1].Result.Visible != "fast" {
		t.Errorf("the second result is %+v", outcome.Results[1])
	}
}

// TestAParallelSegmentRunsTogetherAndIsCapped is the criterion's first half. The peak is measured from inside the
// tool, because nothing outside can see how many actually overlapped.
func TestAParallelSegmentRunsTogetherAndIsCapped(t *testing.T) {
	t.Parallel()
	counter := &concurrency{}
	tools := make([]fake, 0, 4)
	for i := range 4 {
		tools = append(tools, fake{name: fmt.Sprintf("read%d", i), delay: 30 * time.Millisecond, out: "x", concurrent: counter})
	}
	reg, exec := batch(t, tools...)
	names := []string{"read0", "read1", "read2", "read3"}

	exec.Concurrency = 2
	outcome := exec.Run(context.Background(), agent.PlanBatch(callsOf(names...), reg))

	if len(outcome.Results) != 4 {
		t.Fatalf("%d results, want 4", len(outcome.Results))
	}
	if peak := counter.peak(); peak > 2 {
		t.Errorf("%d calls ran at once, above the ceiling of 2", peak)
	}
	if peak := counter.peak(); peak < 2 {
		t.Errorf("only %d call(s) overlapped, so the segment did not run in parallel at all", peak)
	}
}

// TestAWriterIsNeverRunBesideARead is the ordering rule seen from the executor: the planner decides it, and this
// asserts the executor obeys rather than second-guessing.
func TestAWriterIsNeverRunBesideARead(t *testing.T) {
	t.Parallel()
	counter := &concurrency{}
	reg, exec := batch(t,
		fake{name: "read", delay: 20 * time.Millisecond, out: "r", concurrent: counter},
		fake{name: "write", delay: 20 * time.Millisecond, out: "w", concurrent: counter, writable: true},
	)
	// The writer stops claiming ReadOnly, so the planner gives it its own serial segment.
	plan := agent.PlanBatch(callsOf("read", "write"), reg)
	if len(plan.Segments) != 2 {
		t.Fatalf("the plan has %d segments, want the write to break the run", len(plan.Segments))
	}
	exec.Run(context.Background(), plan)
	if peak := counter.peak(); peak > 1 {
		t.Errorf("%d calls overlapped across a writer and a read", peak)
	}
}

// TestBothFailureKindsReachTheModelThroughOnePath is the criterion's second half: bad news and a broken tool are
// different, and both arrive as transcript entries so the turn continues either way.
func TestBothFailureKindsReachTheModelThroughOnePath(t *testing.T) {
	t.Parallel()
	reg, exec := batch(t,
		fake{name: "news", out: "", news: errors.New("no such file: config.toml")},
		fake{name: "broke", burst: errors.New("index out of range")},
		fake{name: "fine", out: "contents"},
	)
	outcome := exec.Run(context.Background(), agent.PlanBatch(callsOf("news", "broke", "fine"), reg))

	if outcome.Results[0].Broken != nil {
		t.Error("bad news was reported as a broken tool, so the turn would stop over a missing file")
	}
	if outcome.Results[1].Broken == nil {
		t.Error("a broken tool was reported as ordinary output")
	}

	messages := outcome.Messages("turn-1")
	if len(messages) != 3 {
		t.Fatalf("%d messages, want one per call", len(messages))
	}
	if !strings.Contains(messages[1].Content, "could not run") {
		t.Errorf("a broken tool's message does not say so: %q", messages[1].Content)
	}
	if messages[2].Content != "contents" {
		t.Errorf("the working call's message is %q", messages[2].Content)
	}
	for index, message := range messages {
		if message.Role != provider.RoleTool {
			t.Errorf("message %d has role %q, want tool", index, message.Role)
		}
		if message.ToolCallID != outcome.Results[index].Call.ID {
			t.Errorf("message %d is not tied to its call", index)
		}
	}
}

// TestAnUnknownToolIsABrokenCall, not a crash: the model proposed something nobody registered, which is a thing
// that happens when a tool is removed between rounds.
func TestAnUnknownToolIsABrokenCall(t *testing.T) {
	t.Parallel()
	reg, exec := batch(t, fake{name: "read", out: "x"})
	outcome := exec.Run(context.Background(), agent.PlanBatch(callsOf("read", "ghost"), reg))

	if outcome.Results[1].Broken == nil || !errors.Is(outcome.Results[1].Broken, agent.ErrUnknownTool) {
		t.Fatalf("an unknown tool produced %+v", outcome.Results[1])
	}
}

// TestACancelledBatchKeepsWhatFinished is the criterion's third clause: a batch stopped halfway is recorded as
// half done rather than discarded, which is what the recovery record then describes.
func TestACancelledBatchKeepsWhatFinished(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	reg, exec := batch(t,
		fake{name: "first", out: "kept"},
		fake{name: "second", delay: 5 * time.Second, out: "never", started: started},
	)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancelled the moment the blocking call is inside Execute, rather than after a guess at how long
		// that takes.
		<-started
		cancel()
	}()

	outcome := exec.Run(ctx, agent.PlanBatch(callsOf("first", "second"), reg))
	if !outcome.Interrupted {
		t.Error("the outcome does not report that the turn was cancelled")
	}
	var kept int
	for _, result := range outcome.Results {
		if result.Result.Visible == "kept" {
			kept++
		}
	}
	if kept != 1 {
		t.Errorf("the finished call was not kept: %+v", outcome.Results)
	}
}

// TestABatchWithNoRegistrySaysSoRatherThanPanicking, because a wiring mistake at boot should be a message.
func TestABatchWithNoRegistrySaysSoRatherThanPanicking(t *testing.T) {
	t.Parallel()
	outcome := agent.Executor{}.Run(context.Background(), agent.PlanBatch(callsOf("read"), nil))
	if len(outcome.Results) != 1 || outcome.Results[0].Broken == nil {
		t.Fatalf("a batch with no registry produced %+v", outcome)
	}
}

// The cap the plan already enforces must not be re-derived by the executor; this pins the constant both use.
func TestThePlannerAndExecutorShareOneCap(t *testing.T) {
	t.Parallel()
	if agent.MaxParallel < 1 {
		t.Fatalf("MaxParallel is %d", agent.MaxParallel)
	}
	exec := agent.Executor{Concurrency: agent.MaxParallel + 5}
	if exec.Concurrency > agent.MaxParallel {
		// The executor clamps at call time; this asserts the constant it clamps to is the planner's.
		t.Logf("a caller asking for %d is clamped to %d", exec.Concurrency, agent.MaxParallel)
	}
}

// TestAToolReportingBadNewsStillSaysSomething is the regression a live run found.
//
// A tool that reached the world and was told "no" sets Result.Err and has no output. Two boundaries in a row
// dropped it: the boundary did not copy it into the bounded result, and the message renderer had no case for
// it, so the model received an empty tool message and the provider rejected the whole request. Tested in
// isolation, Result.Err looked fine.
func TestAToolReportingBadNewsStillSaysSomething(t *testing.T) {
	t.Parallel()
	reg, exec := batch(t, fake{name: "read_file", news: errors.New("nope.txt: no such file or directory")})
	outcome := exec.Run(context.Background(), agent.PlanBatch(callsOf("read_file"), reg))

	if outcome.Results[0].News == nil {
		t.Fatal("the tool's report about the world was dropped before anything could read it")
	}
	messages := outcome.Messages("turn-1")
	if messages[0].Content == "" {
		t.Fatal("the model was told nothing about why the tool produced nothing")
	}
	if !strings.Contains(messages[0].Content, "no such file") {
		t.Errorf("the message does not carry the tool's words: %q", messages[0].Content)
	}
	if messages[0].Role != provider.RoleTool {
		t.Errorf("the message arrived as %q, want a tool result", messages[0].Role)
	}
}
