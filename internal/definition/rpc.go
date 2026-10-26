package definition

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// rpc drains notifications even between requests, so server output never
// holds up a later definition. Only the reader delivers replies.
type rpc struct {
	in      io.WriteCloser
	writes  sync.Mutex
	mu      sync.Mutex
	next    int
	pending map[int]chan message
	done    chan struct{}
	err     error
	stop    context.CancelFunc
	root    string
}

func startRPC(ctx context.Context, root, program string, args ...string) (*rpc, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = root
	// TypeScript starts tsserver children. Close the whole group on exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		in.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		cancel()
		in.Close()
		out.Close()
		return nil, err
	}
	r := &rpc{in: in, pending: map[int]chan message{}, done: make(chan struct{}), stop: cancel, root: root}
	go func() {
		err := r.read(bufio.NewReader(out))
		cancel()
		in.Close()
		cmd.Wait()
		r.mu.Lock()
		r.err = fmt.Errorf("language server stopped: %w", err)
		close(r.done)
		r.mu.Unlock()
	}()
	return r, nil
}

func (r *rpc) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.writes.Lock()
	defer r.writes.Unlock()
	_, err = fmt.Fprintf(r.in, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return err
}

func (r *rpc) notify(method string, params any) error {
	return r.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (r *rpc) call(ctx context.Context, method string, params, result any) error {
	r.mu.Lock()
	r.next++
	id := r.next
	ch := make(chan message, 1)
	r.pending[id] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	// A stuck server must not block a write to its stdin indefinitely.
	stop := context.AfterFunc(ctx, r.stop)
	defer stop()
	if err := r.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		r.mu.Lock()
		err := r.err
		r.mu.Unlock()
		return err
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(m.Result, result)
	}
}

func readMessage(in *bufio.Reader) (message, error) {
	length, headers := -1, 0
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return message{}, err
		}
		headers += len(line)
		if headers > 8192 {
			return message{}, errors.New("language server header too large")
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return message{}, err
			}
		}
	}
	if length < 0 || length > 16<<20 {
		return message{}, errors.New("invalid language server message length")
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(in, b); err != nil {
		return message{}, err
	}
	var m message
	err := json.Unmarshal(b, &m)
	return m, err
}

func (r *rpc) read(in *bufio.Reader) error {
	for {
		m, err := readMessage(in)
		if err != nil {
			return err
		}
		if m.Method != "" {
			if len(m.ID) > 0 {
				if err := r.answer(m); err != nil {
					return err
				}
			}
			continue
		}
		var id int
		if json.Unmarshal(m.ID, &id) != nil {
			continue
		}
		r.mu.Lock()
		if ch := r.pending[id]; ch != nil {
			select {
			case ch <- m:
			default:
			}
		}
		r.mu.Unlock()
	}
}

func (r *rpc) answer(m message) error {
	var result any
	var rpcErr *rpcError
	switch m.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return err
		}
		result = make([]any, len(params.Items))
	case "workspace/workspaceFolders":
		result = []any{map[string]any{"uri": fileURI(r.root), "name": r.root}}
	case "window/workDoneProgress/create", "client/registerCapability", "client/unregisterCapability":
		// The app invalidates servers when its repository watcher fires.
	case "window/showMessageRequest":
	default:
		rpcErr = &rpcError{Code: -32601, Message: "Peek does not support " + m.Method}
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": m.ID}
	if rpcErr != nil {
		reply["error"] = rpcErr
	} else {
		reply["result"] = result
	}
	return r.send(reply)
}
