package lsp

import "encoding/json"

// rpcRequest is an outbound JSON-RPC 2.0 request (carries an id) or notification
// (omits id). Notifications set ID to nil and rely on omitempty.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is an inbound JSON-RPC 2.0 message. A response carries an id and
// one of result/error; a server-initiated notification (window/logMessage,
// publishDiagnostics, …) carries a method and no id. ID is decoded as
// json.RawMessage so a missing/null id is distinguishable from a numeric one.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// Position is a 0-based LSP position (line and UTF-16 character offset). gg's
// user-facing CLI takes 1-based line/col and converts before building this.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open [start, end) span in LSP 0-based coordinates.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is an LSP location: a document URI plus a range within it.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// textDocumentIdentifier names a document by its file:// URI.
type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

// textDocumentItem is the payload of textDocument/didOpen.
type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// referenceContext controls whether the declaration itself is included in
// textDocument/references results.
type referenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// positionParams is the shared shape for definition/hover (textDocument +
// position). references adds a context field via referenceParams.
type positionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type referenceParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      referenceContext       `json:"context"`
}

// HoverResult is the decoded payload of textDocument/hover. contents may be a
// MarkupContent object ({kind,value}) or, from older servers, a string or array
// of marked strings; PlainText folds all of those into one string.
type HoverResult struct {
	PlainText string
	Range     *Range
}
