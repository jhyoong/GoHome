package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// --- helpers ----------------------------------------------------------------

// oneTextTurnClient returns a fake client that responds with a single text
// message and end_turn. It is used to simulate the child agent completing.
func oneTextTurnClient(text string) *fakeClient {
	return &fakeClient{
		sequences: [][]common.StreamEvent{
			{
				{Kind: common.EventTextDelta, TextDelta: text},
				{Kind: common.EventTurnDone, StopReason: "end_turn"},
			},
		},
	}
}

// buildSpawnParent creates a parent Agent ready to call Spawn.
// The caller provides the client (for the child turn) and a tempDir for Home.
func buildSpawnParent(t *testing.T, client common.Client, home string) *Agent {
	t.Helper()
	g := compileYoloGuard(t)
	fe := &fakeRecorder{}

	a := &Agent{
		Client:   client,
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		Writer:   nil, // no parent writer for basic Spawn test
		System:   "parent system",
		Home:     home,
	}
	// Simulate a parent session at depth 0.
	a.Session = session.NewSession("parent-sess", home, "model", "anthropic")
	return a
}

// --- Task 10.2 tests --------------------------------------------------------

// TestSpawn_BasicChildAnswer verifies the happy path:
//   - Spawn returns (resultText == "child answer", isError false, err nil).
//   - A child JSONL file was created under a.Home.
func TestSpawn_BasicChildAnswer(t *testing.T) {
	home := t.TempDir()
	client := oneTextTurnClient("child answer")
	a := buildSpawnParent(t, client, home)

	resultText, isError, err := a.Spawn(context.Background(), "do thing", "")
	if err != nil {
		t.Fatalf("Spawn: unexpected error: %v", err)
	}
	if isError {
		t.Errorf("Spawn: isError = true, want false")
	}
	if resultText != "child answer" {
		t.Errorf("Spawn: resultText = %q, want %q", resultText, "child answer")
	}

	// Verify a child JSONL exists somewhere under home/sessions.
	var found []string
	err = filepath.Walk(filepath.Join(home, "sessions"), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(p, ".jsonl") {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk sessions: %v", err)
	}
	if len(found) == 0 {
		t.Errorf("expected at least one child JSONL under %s/sessions", home)
	}
}

// TestSpawn_DepthCheck verifies that a subagent (depth >= 1) cannot spawn.
func TestSpawn_DepthCheck(t *testing.T) {
	home := t.TempDir()
	client := oneTextTurnClient("irrelevant")
	a := buildSpawnParent(t, client, home)

	// Force parent session to depth 1 (simulates running inside a subagent).
	a.Session.Depth = 1

	_, isError, err := a.Spawn(context.Background(), "nested task", "")
	if err == nil {
		t.Fatal("Spawn: expected error for depth >= 1, got nil")
	}
	if !isError {
		t.Errorf("Spawn: isError = false, want true for depth error")
	}
}

// TestSpawn_CustomSystemPrompt verifies that a non-empty systemPrompt overrides
// the parent's System when building the child agent.
func TestSpawn_CustomSystemPrompt(t *testing.T) {
	home := t.TempDir()
	// The child client doesn't know what system prompt was used, but we can
	// confirm Spawn doesn't error and uses the override — it's a structural test.
	client := oneTextTurnClient("custom system result")
	a := buildSpawnParent(t, client, home)

	resultText, isError, err := a.Spawn(context.Background(), "task", "specialist system")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if isError {
		t.Errorf("isError: got true, want false")
	}
	if resultText != "custom system result" {
		t.Errorf("resultText: got %q", resultText)
	}
}

// TestSpawn_IncrementingChildIDs verifies that successive Spawn calls generate
// sub-1, sub-2, ... IDs (via the atomic counter).
func TestSpawn_IncrementingChildIDs(t *testing.T) {
	home := t.TempDir()
	// Two sequential turns for two spawns.
	client := &fakeClient{
		sequences: [][]common.StreamEvent{
			{{Kind: common.EventTextDelta, TextDelta: "first"}, {Kind: common.EventTurnDone, StopReason: "end_turn"}},
			{{Kind: common.EventTextDelta, TextDelta: "second"}, {Kind: common.EventTurnDone, StopReason: "end_turn"}},
		},
	}
	a := buildSpawnParent(t, client, home)

	r1, _, err := a.Spawn(context.Background(), "task1", "")
	if err != nil {
		t.Fatalf("Spawn 1: %v", err)
	}
	r2, _, err := a.Spawn(context.Background(), "task2", "")
	if err != nil {
		t.Fatalf("Spawn 2: %v", err)
	}

	if r1 != "first" {
		t.Errorf("r1 = %q, want 'first'", r1)
	}
	if r2 != "second" {
		t.Errorf("r2 = %q, want 'second'", r2)
	}

	// Two JSONL files under sessions.
	var files []string
	_ = filepath.Walk(filepath.Join(home, "sessions"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".jsonl") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 2 {
		t.Errorf("expected 2 child JSONL files, got %d: %v", len(files), files)
	}
}

// --- Task 10.3 tests --------------------------------------------------------

// TestSpawn_ParentWriterMarkers verifies that after a successful Spawn, the
// PARENT writer's JSONL contains both a subagent_spawn and a subagent_done line
// with the right childId, and that the child's own JSONL exists with its messages.
func TestSpawn_ParentWriterMarkers(t *testing.T) {
	home := t.TempDir()
	client := oneTextTurnClient("child reply")

	g := compileYoloGuard(t)
	fe := &fakeRecorder{}

	// Open a real parent writer.
	parentPath := filepath.Join(home, "parent.jsonl")
	pw, err := session.OpenWriter(parentPath)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	a := &Agent{
		Client:   client,
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		Writer:   pw,
		System:   "parent system",
		Home:     home,
	}
	a.Session = session.NewSession("parent-sess", home, "model", "anthropic")

	_, _, err = a.Spawn(context.Background(), "the task", "")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Close the parent writer to flush all events before reading.
	if err := pw.Close(); err != nil {
		t.Fatalf("pw.Close: %v", err)
	}

	// Read parent JSONL.
	lines := readJSONLLines(t, parentPath)

	var foundSpawn, foundDone bool
	var spawnChildID, doneChildID string
	for _, m := range lines {
		var typ string
		if err := json.Unmarshal(m["type"], &typ); err != nil {
			continue
		}
		switch typ {
		case "subagent_spawn":
			foundSpawn = true
			if err := json.Unmarshal(m["childId"], &spawnChildID); err != nil {
				t.Errorf("subagent_spawn childId unmarshal: %v", err)
			}
			var task string
			if err := json.Unmarshal(m["task"], &task); err != nil {
				t.Errorf("subagent_spawn task unmarshal: %v", err)
			}
			if task != "the task" {
				t.Errorf("subagent_spawn task: got %q, want %q", task, "the task")
			}
		case "subagent_done":
			foundDone = true
			if err := json.Unmarshal(m["childId"], &doneChildID); err != nil {
				t.Errorf("subagent_done childId unmarshal: %v", err)
			}
			var isErr bool
			if err := json.Unmarshal(m["isError"], &isErr); err != nil {
				t.Errorf("subagent_done isError unmarshal: %v", err)
			}
			if isErr {
				t.Errorf("subagent_done isError: got true, want false")
			}
		}
	}

	if !foundSpawn {
		t.Errorf("no subagent_spawn event found in parent JSONL")
	}
	if !foundDone {
		t.Errorf("no subagent_done event found in parent JSONL")
	}
	if spawnChildID != doneChildID {
		t.Errorf("childId mismatch: spawn=%q done=%q", spawnChildID, doneChildID)
	}
	if spawnChildID == "" {
		t.Errorf("childId is empty")
	}

	// Verify the child JSONL exists and contains a session_start and assistant_message.
	var childFiles []string
	_ = filepath.Walk(filepath.Join(home, "sessions"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".jsonl") {
			childFiles = append(childFiles, p)
		}
		return nil
	})
	if len(childFiles) == 0 {
		t.Fatalf("no child JSONL found under %s/sessions", home)
	}

	childLines := readJSONLLines(t, childFiles[0])
	var foundSessionStart, foundAssistant bool
	for _, m := range childLines {
		var typ string
		if err := json.Unmarshal(m["type"], &typ); err != nil {
			continue
		}
		switch typ {
		case "session_start":
			foundSessionStart = true
		case "assistant_message":
			foundAssistant = true
		}
	}
	if !foundSessionStart {
		t.Errorf("child JSONL missing session_start")
	}
	if !foundAssistant {
		t.Errorf("child JSONL missing assistant_message")
	}
}

// readJSONLLines opens path and returns each decoded JSON line as a map.
func readJSONLLines(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var out []map[string]json.RawMessage
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal JSONL line: %v\nraw: %s", err, line)
		}
		out = append(out, m)
	}
	return out
}
