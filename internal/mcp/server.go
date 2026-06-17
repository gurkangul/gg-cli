// Package mcp implements a hand-rolled, minimal Model Context Protocol (MCP)
// server for gg. It speaks JSON-RPC 2.0 over a newline-delimited stdio
// transport (no port, no daemon): an MCP client spawns `gg mcp serve` as a
// child process and exchanges messages over the child's stdin/stdout.
//
// The server is READ-ONLY: it exposes only the gg_* read tools (search,
// context, impact, canon, task_get, bug_get). No write tools are registered,
// which is the read-only guarantee — there is no record/task-create/done
// surface to call at all.
//
// stdout is the protocol channel. Nothing other than JSON-RPC responses may be
// written to stdout; all diagnostics go to stderr.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ProtocolVersion is the MCP protocol revision advertised in the initialize
// handshake. Clients negotiate against this; we echo a fixed supported version.
const ProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 reserved error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// maxLineBytes caps a single newline-delimited JSON-RPC message. bufio.Scanner
// defaults to 64 KiB; we raise the read buffer generously so a large client
// request is never silently truncated into a parse error.
const maxLineBytes = 8 << 20 // 8 MiB

// rpcRequest is the inbound JSON-RPC 2.0 envelope. id is decoded as
// json.RawMessage so a string, number, or null id round-trips unchanged in the
// response (the spec requires the response id to equal the request id).
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcResponse is the outbound JSON-RPC 2.0 envelope. Exactly one of Result or
// Error is populated.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolHost is the backend the protocol core dispatches tool calls to. Kept
// narrow so the JSON-RPC machinery has no dependency on the gg store.
type ToolHost interface {
	// ListTools returns the advertised tool descriptors for tools/list.
	ListTools() []Tool
	// CallTool dispatches a tools/call by name. The returned content blocks are
	// returned verbatim to the client; isError marks a tool-level (not
	// protocol-level) failure.
	CallTool(ctx context.Context, name string, args map[string]any) (content []ContentBlock, isError bool)
}

// Tool is a tool descriptor advertised over tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ContentBlock is a single MCP content block. Only text blocks are produced.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TextBlock wraps a string as a single text content block.
func TextBlock(text string) []ContentBlock {
	return []ContentBlock{{Type: "text", Text: text}}
}

// Server is the transport-agnostic JSON-RPC 2.0 protocol core. Feed it one raw
// line via HandleLine; it returns the response bytes (or nil for notifications
// and blank input). Serve wires it to stdin/stdout for the stdio transport.
type Server struct {
	host       ToolHost
	serverInfo serverInfo
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// New constructs a Server backed by host. version is advertised in the
// initialize handshake's serverInfo.
func New(host ToolHost, version string) *Server {
	return &Server{
		host:       host,
		serverInfo: serverInfo{Name: "gg", Version: version},
	}
}

// Serve runs the stdio transport: read newline-delimited JSON-RPC requests from
// in, write newline-delimited JSON-RPC responses to out, and route diagnostics
// to logw (stderr). It returns when in reaches EOF or the context is cancelled.
//
// CRITICAL: out is the JSON-RPC channel. The caller must ensure nothing else
// writes to it.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer, logw io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		resp := s.HandleLine(ctx, line)
		if resp == nil {
			continue // notification or blank line — no response
		}
		if _, err := out.Write(append(resp, '\n')); err != nil {
			fmt.Fprintf(logw, "gg mcp: stdout write failed: %v\n", err)
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(logw, "gg mcp: stdin read failed: %v\n", err)
		return err
	}
	return nil
}

// HandleLine processes one raw JSON-RPC line and returns the marshalled
// response, or nil when no response must be written (notifications, blank
// input). It never panics on malformed input.
func (s *Server) HandleLine(ctx context.Context, raw []byte) []byte {
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return nil
	}

	var req rpcRequest
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return s.encodeError(nil, codeParseError, "Parse error")
	}
	if req.Method == "" {
		return s.encodeError(idOrNull(req.ID), codeInvalidRequest, "Invalid Request")
	}

	// A request without an id is a notification: dispatch for side effects but
	// never emit a response (initialize ack, etc.).
	isNotification := isNullID(req.ID)

	result, rpcErr := s.dispatch(ctx, req.Method, req.Params)
	if isNotification {
		return nil
	}
	if rpcErr != nil {
		return s.encodeError(idOrNull(req.ID), rpcErr.Code, rpcErr.Message)
	}
	out, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: idOrNull(req.ID), Result: result})
	if err != nil {
		return s.encodeError(idOrNull(req.ID), codeInternalError, err.Error())
	}
	return out
}

// dispatch routes a method to its handler. methodNotFound is signalled via the
// returned rpcError.
func (s *Server) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": ProtocolVersion,
			"serverInfo":      s.serverInfo,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		}, nil
	case "notifications/initialized":
		// Notification — no response. Return empty; HandleLine drops it.
		return map[string]any{}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.host.ListTools()}, nil
	case "tools/call":
		return s.dispatchToolCall(ctx, params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "Method not found: " + method}
	}
}

func (s *Server) dispatchToolCall(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidRequest, Message: "invalid params: " + err.Error()}
		}
	}
	if p.Name == "" {
		return nil, &rpcError{Code: codeInvalidRequest, Message: `tools/call requires a string "name"`}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}
	content, isErr := s.host.CallTool(ctx, p.Name, p.Arguments)
	if content == nil {
		content = TextBlock("")
	}
	return map[string]any{"content": content, "isError": isErr}, nil
}

func (s *Server) encodeError(id json.RawMessage, code int, message string) []byte {
	out, err := json.Marshal(rpcResponse{
		JSONRPC: "2.0",
		ID:      idOrNull(id),
		Error:   &rpcError{Code: code, Message: message},
	})
	if err != nil {
		// Last-ditch hand-built error; id is best-effort "null".
		return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":null,"error":{"code":%d,"message":"encode error"}}`, codeInternalError))
	}
	return out
}

// idOrNull returns the raw id, or the JSON literal null when absent so the
// response always carries an explicit id per JSON-RPC 2.0.
func idOrNull(id json.RawMessage) json.RawMessage {
	if isNullID(id) {
		return json.RawMessage("null")
	}
	return id
}

// isNullID reports whether an id field is absent or the JSON null literal.
func isNullID(id json.RawMessage) bool {
	s := strings.TrimSpace(string(id))
	return s == "" || s == "null"
}
