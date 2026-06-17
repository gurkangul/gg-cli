// Package lsp implements a minimal, per-invocation Language Server Protocol
// (LSP) client. It speaks JSON-RPC 2.0 over a child process's stdin/stdout
// using LSP's Content-Length message framing (NOT newline-delimited):
//
//	Content-Length: <N>\r\n\r\n<json-of-N-bytes>
//
// The client is strictly per-invocation: spawn the language server, initialize,
// open the target file, run one query, shut down, and exit. There is NO daemon,
// no persistent server, and no goroutine outliving the command.
package lsp

import (
	"bufio"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
)

// writeMessage frames payload with an LSP Content-Length header and writes it to
// w. payload is the already-marshalled JSON-RPC message body.
func writeMessage(w io.Writer, payload []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readMessage reads one Content-Length-framed message from r and returns the
// raw JSON body. It parses the textproto header block (tolerating extra headers
// like Content-Type), then reads exactly Content-Length bytes.
//
// io.EOF is returned verbatim when the stream ends at a message boundary so the
// caller can distinguish a clean close from a mid-message truncation.
func readMessage(r *textproto.Reader) ([]byte, error) {
	header, err := r.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	cl := header.Get("Content-Length")
	if cl == "" {
		return nil, fmt.Errorf("lsp: message missing Content-Length header")
	}
	n, err := strconv.Atoi(cl)
	if err != nil {
		return nil, fmt.Errorf("lsp: invalid Content-Length %q: %w", cl, err)
	}
	if n < 0 {
		return nil, fmt.Errorf("lsp: negative Content-Length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r.R, body); err != nil {
		return nil, fmt.Errorf("lsp: short read of message body (%d bytes): %w", n, err)
	}
	return body, nil
}

// newReader wraps an io.Reader as a textproto.Reader over a buffered reader so
// the header block can be parsed line-by-line and the body read in bulk from the
// same buffered stream.
func newReader(r io.Reader) *textproto.Reader {
	return textproto.NewReader(bufio.NewReader(r))
}
