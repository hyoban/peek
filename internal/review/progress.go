package review

import (
	"bytes"
	"encoding/json"
	"strings"
)

const maxEvent = 64 * 1024

// Consume JSONL incrementally. Command output can be very large; discard
// oversized events without retaining their contents or interrupting Codex.
// Only activity metadata is displayed, never reasoning or command output.
type progressWriter struct {
	line     []byte
	dropping bool
	report   func(string, string)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		part := p
		if i >= 0 {
			part = p[:i]
		}
		if !w.dropping {
			if len(w.line)+len(part) > maxEvent {
				w.line, w.dropping = nil, true
			} else {
				w.line = append(w.line, part...)
			}
		}
		if i < 0 {
			break
		}
		if !w.dropping {
			w.event(w.line)
		}
		w.line, w.dropping = w.line[:0], false
		p = p[i+1:]
	}
	return n, nil
}

func (w *progressWriter) event(data []byte) {
	var event struct {
		Type string `json:"type"`
		Item struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"item"`
	}
	if json.Unmarshal(data, &event) != nil {
		return
	}
	switch event.Type {
	case "thread.started", "turn.started":
		w.report("Reviewing…", "Codex is reviewing changes")
	case "item.started":
		if event.Item.Type == "command_execution" {
			command := []rune(strings.Join(strings.Fields(event.Item.Command), " "))
			if len(command) > 240 {
				command = append(command[:240], '…')
			}
			w.report("Inspecting…", string(command))
		} else if event.Item.Type == "mcp_tool_call" || event.Item.Type == "web_search" {
			w.report("Inspecting…", "Codex is gathering review context")
		}
	case "item.completed":
		if event.Item.Type == "command_execution" || event.Item.Type == "mcp_tool_call" || event.Item.Type == "web_search" {
			w.report("Reviewing…", "Codex is reviewing the collected context")
		}
	case "turn.completed":
		w.report("Checking…", "Validating the review result and checking for changes")
	}
}
