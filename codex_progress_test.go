package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodexProgressStream(t *testing.T) {
	var phases []string
	w := &codexProgressWriter{report: func(phase, detail string) {
		phases = append(phases, phase)
		if strings.Contains(detail, "hidden reasoning") || strings.Contains(detail, "private output") {
			t.Fatal("non-activity content displayed")
		}
	}}
	events := `{"type":"thread.started"}
{"type":"item.started","item":{"type":"command_execution","command":"git diff"}}
{"type":"item.completed","item":{"type":"reasoning","text":"hidden reasoning"}}
not JSON
{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"private output"}}
`
	// Pipes can split a JSON object anywhere, including inside a string.
	for _, b := range []byte(events) {
		if n, err := w.Write([]byte{b}); n != 1 || err != nil {
			t.Fatalf("write: %d, %v", n, err)
		}
	}
	for range 4 {
		w.Write([]byte(strings.Repeat("x", maxCodexEvent)))
		if len(w.line) > maxCodexEvent {
			t.Fatal("oversized event buffered")
		}
	}
	w.Write([]byte("\n{\"type\":\"turn.completed\"}\n"))
	want := []string{"Reviewing…", "Inspecting…", "Reviewing…", "Checking…"}
	if !reflect.DeepEqual(phases, want) {
		t.Fatalf("progress %v, want %v", phases, want)
	}
}

func TestCodexReviewStreamsBeforeCompletion(t *testing.T) {
	fakeReviewCodex(t, passingReview)
	dir := t.TempDir()
	events, ack := filepath.Join(dir, "events"), filepath.Join(dir, "ack")
	if err := os.WriteFile(events, []byte("{\"type\":\"item.started\",\"item\":{\"type\":\"command_execution\",\"command\":\"git diff\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PEEK_TEST_CODEX_EVENTS", events)
	t.Setenv("PEEK_TEST_CODEX_ACK", ack)
	repo := testRepo(t)
	var callbackErr error
	result, err := codexReviewProgress(context.Background(), repo, reviewTestSnapshot(t, repo), func(phase, detail string) {
		if phase == "Inspecting…" && detail == "git diff" {
			callbackErr = os.WriteFile(ack, nil, 0o600)
		}
	})
	if err != nil || callbackErr != nil || result.Verdict != "passed" {
		t.Fatalf("streamed review: %+v, %v, %v", result, err, callbackErr)
	}
}
