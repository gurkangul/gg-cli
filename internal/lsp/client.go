package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Client is a per-invocation LSP client bound to one running language server
// process. Construct it with Dial, run one or more queries, then Close. It is
// NOT safe for concurrent use and is NOT a daemon: the process dies with Close.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *textproto.Reader
	stderr *bytes.Buffer

	mu     sync.Mutex
	nextID int

	rootURI string
}

// Dial spawns the language server described by spec, performs the initialize /
// initialized handshake rooted at rootDir, and returns a ready Client. The
// caller MUST call Close to shut the server down. ctx bounds the handshake.
func Dial(ctx context.Context, spec ServerSpec, rootDir string) (*Client, error) {
	if err := spec.ensureOnPath(); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}

	cmd := exec.CommandContext(ctx, spec.Cmd, spec.Args...)
	cmd.Dir = absRoot
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Cmd, err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		reader:  newReader(stdout),
		stderr:  &stderr,
		nextID:  1,
		rootURI: PathToURI(absRoot),
	}

	if err := c.handshake(ctx); err != nil {
		_ = c.kill()
		return nil, c.wrapErr("handshake", err)
	}
	return c, nil
}

// handshake sends initialize, waits for its result, then fires the initialized
// notification — the sequence gopls requires before answering position queries.
func (c *Client) handshake(ctx context.Context) error {
	pid := os.Getpid()
	initParams := map[string]any{
		"processId":    pid,
		"rootUri":      c.rootURI,
		"capabilities": map[string]any{},
		"workspaceFolders": []map[string]any{
			{"uri": c.rootURI, "name": filepath.Base(c.rootURI)},
		},
	}
	if _, err := c.call(ctx, "initialize", initParams); err != nil {
		return err
	}
	return c.notify("initialized", map[string]any{})
}

// OpenFile reads path and sends textDocument/didOpen so the server has the
// buffer contents — REQUIRED before references/definition/hover return results.
// It returns the file's text (the caller uses it to map 1-based col → UTF-16).
func (c *Client) OpenFile(path, languageID string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	text := string(data)
	params := map[string]any{
		"textDocument": textDocumentItem{
			URI:        PathToURI(abs),
			LanguageID: languageID,
			Version:    1,
			Text:       text,
		},
	}
	if err := c.notify("textDocument/didOpen", params); err != nil {
		return "", err
	}
	return text, nil
}

// References returns all references (including the declaration) to the symbol at
// pos in the file identified by uri.
func (c *Client) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	raw, err := c.call(ctx, "textDocument/references", referenceParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     pos,
		Context:      referenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw)
}

// Definition returns the definition location(s) of the symbol at pos. Servers
// may answer with a single Location, an array, or LocationLink objects;
// decodeLocations normalizes all three.
func (c *Client) Definition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	raw, err := c.call(ctx, "textDocument/definition", positionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     pos,
	})
	if err != nil {
		return nil, err
	}
	return decodeLocations(raw)
}

// Hover returns the hover signature/documentation for the symbol at pos.
func (c *Client) Hover(ctx context.Context, uri string, pos Position) (HoverResult, error) {
	raw, err := c.call(ctx, "textDocument/hover", positionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Position:     pos,
	})
	if err != nil {
		return HoverResult{}, err
	}
	return decodeHover(raw)
}

// Close shuts the server down cleanly: shutdown request, exit notification, then
// wait (killing if it overstays). It is safe to call once. Errors from the
// graceful path are swallowed in favour of forcing the process down — the whole
// point is that no server outlives the command.
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.cmd == nil {
		return nil
	}
	// Best-effort graceful shutdown; ignore errors and force-kill regardless.
	_, _ = c.call(ctx, "shutdown", nil)
	_ = c.notify("exit", nil)
	_ = c.stdin.Close()
	return c.kill()
}

// kill waits briefly for the process via Wait; the context passed to
// exec.CommandContext already force-kills on timeout, and the parent's ctx
// cancel covers Ctrl+C. We always reap to avoid a zombie.
func (c *Client) kill() error {
	if c.cmd.Process == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
		return nil
	default:
		// Not yet exited after exit-notification + stdin close: terminate.
		_ = c.cmd.Process.Kill()
		<-done
		return nil
	}
}

// call sends a JSON-RPC request and blocks until the matching response (by id)
// arrives, ignoring interleaved server notifications and mismatched ids. ctx
// bounds the wait so a hung server cannot wedge the command.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", method, err)
	}
	if err := writeMessage(c.stdin, payload); err != nil {
		return nil, c.wrapErr("write "+method, err)
	}

	type readResult struct {
		raw json.RawMessage
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		raw, rerr := c.awaitResponse(id)
		ch <- readResult{raw, rerr}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s timed out: %w", method, ctx.Err())
	case r := <-ch:
		return r.raw, r.err
	}
}

// awaitResponse reads framed messages until it finds the response whose id
// matches want, skipping server-initiated notifications (no/ null id, or a
// method field) and any non-matching response ids.
func (c *Client) awaitResponse(want int) (json.RawMessage, error) {
	for {
		body, err := readMessage(c.reader)
		if err != nil {
			if err == io.EOF {
				return nil, c.wrapErr("await response", fmt.Errorf("server closed stream before responding"))
			}
			return nil, err
		}
		var msg rpcResponse
		if uerr := json.Unmarshal(body, &msg); uerr != nil {
			return nil, fmt.Errorf("decode message: %w", uerr)
		}
		// Server-initiated notification (window/logMessage, publishDiagnostics,
		// $/progress, …): no id (or null). Ignore and keep reading.
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			continue
		}
		var gotID int
		if uerr := json.Unmarshal(msg.ID, &gotID); uerr != nil {
			// Non-numeric id we never send: not ours, skip.
			continue
		}
		if gotID != want {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("server error: %s (code %d)", msg.Error.Message, msg.Error.Code)
		}
		return msg.Result, nil
	}
}

// notify sends a JSON-RPC notification (no id, no response expected).
func (c *Client) notify(method string, params any) error {
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("marshal %s: %w", method, err)
	}
	if err := writeMessage(c.stdin, payload); err != nil {
		return c.wrapErr("write "+method, err)
	}
	return nil
}

// wrapErr augments err with any captured stderr so a server that died with a
// diagnostic (e.g. version mismatch, build error) reports something useful.
func (c *Client) wrapErr(stage string, err error) error {
	if c.stderr != nil {
		if tail := bytes.TrimSpace(c.stderr.Bytes()); len(tail) > 0 {
			return fmt.Errorf("%s: %w (server stderr: %s)", stage, err, tail)
		}
	}
	return fmt.Errorf("%s: %w", stage, err)
}
