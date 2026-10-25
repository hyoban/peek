package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Event is what Codex does as it answers, as Stream reports it: the text
// it adds to its answer, or a step it starts, such as a command it runs.
type Event struct {
	// Delta is text Codex adds to its answer.
	Delta string
	// Step is the kind of step Codex starts, "" for none: "reasoning",
	// "commandExecution", "mcpToolCall", "webSearch" or "agentMessage";
	// Detail tells it, as a command's line.
	Step, Detail string
}

// Stream has Codex answer the prompt over the repository in a read-only
// sandbox, through its app server, which streams the answer as Codex
// writes it, each piece and step to on, on the goroutine reading them: a
// new conversation where session is "", else the one of that id, resumed
// with its earlier turns in mind. It returns the answer, Codex's messages
// of the turn one after another, and the conversation's id.
func Stream(ctx context.Context, repo, session, prompt string, timeout time.Duration, on func(Event)) (string, string, error) {
	if repo == "" {
		return "", session, fmt.Errorf("a repository is required for Codex")
	}
	bin, err := executable()
	if err != nil {
		return "", session, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "app-server")
	cmd.Dir = repo
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return "", session, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", session, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", session, err
	}
	defer func() {
		in.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()
	c := &rpc{in: in, out: bufio.NewReaderSize(out, 64*1024)}
	fail := func(err error) (string, string, error) {
		if ctx.Err() != nil {
			return "", session, ctx.Err()
		}
		if s := strings.TrimSpace(stderr.String()); s != "" {
			err = fmt.Errorf("%w\n%s", err, s)
		}
		return "", session, fmt.Errorf("Codex failed: %w", err)
	}
	// Reading stops as the context ends, which kills the server.
	go func() {
		<-ctx.Done()
		in.Close()
	}()
	if _, err := c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "peek", "title": "Peek", "version": "1"}}, nil); err != nil {
		return fail(err)
	}
	if err := c.notify("initialized"); err != nil {
		return fail(err)
	}
	thread := map[string]any{"cwd": repo, "sandbox": "read-only", "approvalPolicy": "never"}
	method := "thread/start"
	if session != "" {
		method, thread["threadId"] = "thread/resume", session
	}
	res, err := c.call(method, thread, nil)
	if err != nil {
		return fail(err)
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(res, &started) == nil && started.Thread.ID != "" {
		session = started.Thread.ID
	}
	var answer strings.Builder
	var turnErr error
	_, err = c.call("turn/start", map[string]any{"threadId": session, "input": []map[string]string{{"type": "text", "text": prompt}}},
		func(method string, params json.RawMessage) bool {
			switch method {
			case "item/agentMessage/delta":
				var d struct {
					Delta string `json:"delta"`
				}
				if json.Unmarshal(params, &d) == nil && d.Delta != "" {
					answer.WriteString(d.Delta)
					on(Event{Delta: d.Delta})
				}
			case "item/started":
				var s struct {
					Item struct {
						Type    string `json:"type"`
						Command string `json:"command"`
					} `json:"item"`
				}
				if json.Unmarshal(params, &s) != nil {
					break
				}
				if s.Item.Type == "agentMessage" && answer.Len() > 0 {
					// Messages one after another, as paragraphs.
					answer.WriteString("\n\n")
					on(Event{Delta: "\n\n"})
				}
				on(Event{Step: s.Item.Type, Detail: strings.Join(strings.Fields(s.Item.Command), " ")})
			case "turn/completed":
				var t struct {
					Turn struct {
						Status string `json:"status"`
						Error  *struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"turn"`
				}
				if json.Unmarshal(params, &t) == nil && t.Turn.Status != "completed" {
					turnErr = fmt.Errorf("Codex's turn %s", t.Turn.Status)
					if t.Turn.Error != nil && t.Turn.Error.Message != "" {
						turnErr = fmt.Errorf("%s", t.Turn.Error.Message)
					}
				}
				return true
			case "error":
				var e struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
					WillRetry bool `json:"willRetry"`
				}
				if json.Unmarshal(params, &e) == nil && !e.WillRetry && e.Error.Message != "" {
					turnErr = fmt.Errorf("%s", e.Error.Message)
				}
			}
			return false
		})
	if err != nil {
		return fail(err)
	}
	if turnErr != nil {
		return "", session, turnErr
	}
	return strings.TrimSpace(answer.String()), session, nil
}

// rpc is a JSON-RPC connection to Codex's app server over its input and
// output, one message a line.
type rpc struct {
	mu   sync.Mutex
	in   io.Writer
	out  *bufio.Reader
	next int
}

type message struct {
	ID     *int            `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *rpc) send(m map[string]any) error {
	m["jsonrpc"] = "2.0"
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.in.Write(append(data, '\n'))
	return err
}

func (c *rpc) notify(method string) error { return c.send(map[string]any{"method": method}) }

// call sends the request and returns its result, handing the
// notifications that come meanwhile to on, where not nil; it waits on
// after the result for on to be done, where it is not yet.
func (c *rpc) call(method string, params any, on func(string, json.RawMessage) bool) (json.RawMessage, error) {
	c.next++
	id := c.next
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		m, err := c.read()
		if err != nil {
			return nil, err
		}
		switch {
		case m.Method != "" && m.ID != nil:
			// A request of the server's, as an approval, which a read-only
			// sandbox that never asks has none of: declined.
			c.send(map[string]any{"id": *m.ID, "error": map[string]any{"code": -32601, "message": "not supported"}})
		case m.Method != "":
			if on != nil && on(m.Method, m.Params) {
				on = nil
			}
		case m.ID != nil && *m.ID == id:
			if m.Error != nil {
				return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
			}
			if on != nil {
				return m.Result, c.wait(on)
			}
			return m.Result, nil
		}
	}
}

// wait hands the notifications that come to on until it is done.
func (c *rpc) wait(on func(string, json.RawMessage) bool) error {
	for {
		m, err := c.read()
		if err != nil {
			return err
		}
		switch {
		case m.Method != "" && m.ID != nil:
			c.send(map[string]any{"id": *m.ID, "error": map[string]any{"code": -32601, "message": "not supported"}})
		case m.Method != "" && on(m.Method, m.Params):
			return nil
		}
	}
}

func (c *rpc) read() (message, error) {
	for {
		line, err := c.out.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if err == io.EOF {
				err = fmt.Errorf("the app server stopped")
			}
			return message{}, err
		}
		var m message
		if json.Unmarshal(line, &m) == nil {
			return m, nil
		}
	}
}
