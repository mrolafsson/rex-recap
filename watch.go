package main

// The watcher stands in for herdr's event hooks under Rex: herdr runs `tick`
// on every agent status change, and Rex has the changes as an event stream
// (rex events) that something has to follow. `watch` follows it: a status
// change schedules a recap as tick does, and a change of focus notes the
// block you're looking at as seen (rex.go). One runs per machine, started by
// the popup when none is.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func watchLockPath() string { return filepath.Join(stateDir(), "watch.lock") }

// lockWatch takes the watcher's lock, held until the process ends: false
// when another watcher has it.
func lockWatch() (*os.File, bool) {
	if os.MkdirAll(stateDir(), 0o700) != nil {
		return nil, false
	}
	f, err := os.OpenFile(watchLockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, false
	}
	return f, true
}

// watching says whether a watcher is running.
func watching() bool {
	f, ok := lockWatch()
	if ok {
		f.Close()
	}
	return !ok
}

// ensureWatcher starts a watcher in the background when none is running.
func ensureWatcher() {
	if watching() {
		return
	}
	if err := spawn("watch"); err != nil {
		fmt.Fprintln(os.Stderr, "rex-recap: starting the watcher:", err)
	}
}

// runWatch is `watch`: follow Rex's events until stopped, starting again
// when the stream ends (the server restarted, say).
func runWatch(ctx context.Context, cfg config) error {
	lock, ok := lockWatch()
	if !ok {
		return nil
	}
	defer lock.Close()
	// Agents that finished while nothing was watching.
	if err := tickPane(cfg, ""); err != nil {
		fmt.Fprintln(os.Stderr, "rex-recap: tick:", err)
	}
	wait := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := followEvents(ctx, cfg)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(start) > time.Minute {
			wait = time.Second
		}
		fmt.Fprintln(os.Stderr, "rex-recap: events ended, again in", wait, err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
	return nil
}

func followEvents(ctx context.Context, cfg config) error {
	cmd := exec.CommandContext(ctx, rexBin(), "events", "--all", "--json")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		handleEvent(cfg, sc.Bytes())
	}
	return cmd.Wait()
}

// rexEvent is one line of rex events --json: a server event, or a session's
// wrapped in session_event, or a block's in that again.
type rexEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type rexSessionEvent struct {
	SessionID string   `json:"session_id"`
	Event     rexEvent `json:"event"`
}

type rexBlockEvent struct {
	BlockID string `json:"block_id"`
	Name    string `json:"name"`
	Payload struct {
		ID     string    `json:"id"`
		Record rexRecord `json:"record"`
	} `json:"payload"`
}

// The watcher's view of Rex, variables so tests can stand them in.
var (
	tickFor     = tickPane
	markFocused = markFocusedSeen
)

// handleEvent acts on one event: a block's own status changing ticks it,
// and the layout changing (focus moves with it) notes what's focused.
func handleEvent(cfg config, line []byte) {
	var ev rexEvent
	if json.Unmarshal(line, &ev) != nil || ev.Type != "session_event" {
		return
	}
	var se rexSessionEvent
	if json.Unmarshal(ev.Payload, &se) != nil {
		return
	}
	switch se.Event.Type {
	case "block_event":
		var be rexBlockEvent
		if json.Unmarshal(se.Event.Payload, &be) != nil {
			return
		}
		if be.Name == "program_status_changed" && be.Payload.ID == "" && be.BlockID != "" {
			if err := tickFor(cfg, be.BlockID); err != nil {
				fmt.Fprintln(os.Stderr, "rex-recap: tick", be.BlockID+":", err)
			}
		}
	case "session_view_changed", "active_window_changed":
		markFocused(se.SessionID)
	}
}

// markFocusedSeen notes the block focused in a session's active window as
// seen now.
func markFocusedSeen(sessionID string) {
	out, err := rexRun("", "api", "call", "-s", sessionID, "session.view", "{}")
	if err != nil {
		return
	}
	if b := focusedBlock(out); b != "" {
		markSeen(b, time.Now())
	}
}

// focusedBlock is the block focused in the active window of a session view.
func focusedBlock(view []byte) string {
	var v struct {
		Active  string `json:"active_window_id"`
		Windows []struct {
			WindowID string `json:"window_id"`
			Focused  string `json:"focused_block_id"`
		} `json:"windows"`
	}
	if json.Unmarshal(view, &v) != nil {
		return ""
	}
	for _, w := range v.Windows {
		if w.WindowID == v.Active {
			return w.Focused
		}
	}
	return ""
}
