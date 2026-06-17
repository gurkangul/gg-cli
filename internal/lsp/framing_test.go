package lsp

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestFramingRoundTrip verifies the Content-Length codec encode→decode without
// needing any language server: write a message, then read it back intact.
func TestFramingRoundTrip(t *testing.T) {
	cases := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{}`,
		`{"emoji":"🚀","note":"multibyte ünïcödé"}`, // exercises byte-length, not rune count
		"",
	}
	for _, payload := range cases {
		var buf bytes.Buffer
		if err := writeMessage(&buf, []byte(payload)); err != nil {
			t.Fatalf("writeMessage(%q): %v", payload, err)
		}
		got, err := readMessage(newReader(&buf))
		if err != nil {
			t.Fatalf("readMessage(%q): %v", payload, err)
		}
		if string(got) != payload {
			t.Fatalf("round-trip mismatch: got %q want %q", got, payload)
		}
	}
}

// TestFramingHeaderShape asserts the exact wire framing: a Content-Length header,
// the literal length in BYTES, a blank line, then the body.
func TestFramingHeaderShape(t *testing.T) {
	payload := `{"k":"🚀"}` // 11 bytes: the emoji is 4 bytes
	var buf bytes.Buffer
	if err := writeMessage(&buf, []byte(payload)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	wantHeader := "Content-Length: " + itoa(len(payload)) + "\r\n\r\n"
	if !strings.HasPrefix(out, wantHeader) {
		t.Fatalf("header mismatch:\n got %q\nwant prefix %q", out, wantHeader)
	}
	if got := strings.TrimPrefix(out, wantHeader); got != payload {
		t.Fatalf("body mismatch: got %q want %q", got, payload)
	}
}

// TestFramingTwoMessages confirms the reader resyncs on the second message
// boundary so interleaved server notifications can be drained one at a time.
func TestFramingTwoMessages(t *testing.T) {
	var buf bytes.Buffer
	first := `{"id":1}`
	second := `{"method":"window/logMessage"}`
	if err := writeMessage(&buf, []byte(first)); err != nil {
		t.Fatal(err)
	}
	if err := writeMessage(&buf, []byte(second)); err != nil {
		t.Fatal(err)
	}
	r := newReader(&buf)
	got1, err := readMessage(r)
	if err != nil || string(got1) != first {
		t.Fatalf("first message: got %q err %v", got1, err)
	}
	got2, err := readMessage(r)
	if err != nil || string(got2) != second {
		t.Fatalf("second message: got %q err %v", got2, err)
	}
	if _, err := readMessage(r); err != io.EOF {
		t.Fatalf("expected io.EOF at stream end, got %v", err)
	}
}

// TestReadMessageMissingContentLength rejects a header block with no
// Content-Length rather than blocking or panicking.
func TestReadMessageMissingContentLength(t *testing.T) {
	raw := "Content-Type: application/json\r\n\r\n{}"
	if _, err := readMessage(newReader(strings.NewReader(raw))); err == nil {
		t.Fatal("expected error for missing Content-Length, got nil")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
