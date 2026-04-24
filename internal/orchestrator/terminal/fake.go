package terminal

import (
	"context"
	"fmt"
	"sync"
)

// Call records one method invocation on FakeTerminal.
type Call struct {
	Method string
	ID     SurfaceID
	Arg    string // text / key / empty
}

// FakeTerminal is an in-memory spy that satisfies the Terminal interface.
// It is intended for unit tests: assert on the Calls slice to verify
// orchestrator behaviour without a real terminal.
// FakeTerminal is safe for concurrent use.
type FakeTerminal struct {
	mu      sync.Mutex
	next    int
	screens map[SurfaceID][]byte
	alive   map[SurfaceID]bool
	Calls   []Call
}

// NewFake returns an initialised FakeTerminal.
func NewFake() *FakeTerminal {
	return &FakeTerminal{
		screens: make(map[SurfaceID][]byte),
		alive:   make(map[SurfaceID]bool),
	}
}

func (f *FakeTerminal) NewSplit(_ context.Context, opts SplitOpts) (SurfaceID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := SurfaceID(fmt.Sprintf("fake-%d", f.next))
	f.screens[id] = nil
	f.alive[id] = true
	f.Calls = append(f.Calls, Call{Method: "NewSplit", Arg: opts.Cmd})
	return id, nil
}

func (f *FakeTerminal) Send(_ context.Context, id SurfaceID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.alive[id] {
		return fmt.Errorf("%w: %s", ErrSurfaceNotFound, id)
	}
	f.screens[id] = append(f.screens[id], []byte(text)...)
	f.Calls = append(f.Calls, Call{Method: "Send", ID: id, Arg: text})
	return nil
}

func (f *FakeTerminal) SendKey(_ context.Context, id SurfaceID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.alive[id] {
		return fmt.Errorf("%w: %s", ErrSurfaceNotFound, id)
	}
	f.Calls = append(f.Calls, Call{Method: "SendKey", ID: id, Arg: key})
	return nil
}

func (f *FakeTerminal) ReadScreen(_ context.Context, id SurfaceID) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.alive[id] {
		return nil, fmt.Errorf("%w: %s", ErrSurfaceNotFound, id)
	}
	content := make([]byte, len(f.screens[id]))
	copy(content, f.screens[id])
	f.Calls = append(f.Calls, Call{Method: "ReadScreen", ID: id})
	return content, nil
}

func (f *FakeTerminal) Focus(_ context.Context, id SurfaceID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.alive[id] {
		return fmt.Errorf("%w: %s", ErrSurfaceNotFound, id)
	}
	f.Calls = append(f.Calls, Call{Method: "Focus", ID: id})
	return nil
}

func (f *FakeTerminal) Close(_ context.Context, id SurfaceID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.alive[id] {
		return fmt.Errorf("%w: %s", ErrSurfaceNotFound, id)
	}
	delete(f.alive, id)
	delete(f.screens, id)
	f.Calls = append(f.Calls, Call{Method: "Close", ID: id})
	return nil
}

func (f *FakeTerminal) Capabilities() Caps {
	return Caps{CanReadScreen: true}
}

// IsAlive reports whether the surface is still open.
func (f *FakeTerminal) IsAlive(id SurfaceID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[id]
}

// SetScreen overwrites the simulated screen content (useful in tests to
// produce specific ReadScreen output).
func (f *FakeTerminal) SetScreen(id SurfaceID, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.screens[id] = content
}
