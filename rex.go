package main

// Rex as recap's host: the herdr calls recap makes (herdr.go), made of Rex's
// CLI and Lua instead. What herdr hands over ready-made, Rex has in pieces:
//
//   - herdr's agent list is Rex's blocks, each with the status its program
//     reports (the Program Status Protocol, OSC 7501). Claude Code reports
//     idle, working, done and blocked, as herdr's are named, with app
//     "claude-code".
//   - herdr turns done into idle once you look at the pane; Rex keeps done.
//     So the watcher (watch.go) notes when a block is focused, and a done
//     block focused since it finished is idle here too. That keeps "only
//     recap what you haven't seen" working as it does under herdr.
//   - herdr's workspaces are Rex's sessions.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const rexTerminal = "com.superlogical.terminal"

// rexPopupLabel labels the block recap's popup runs in, so the toggle can
// find it.
const rexPopupLabel = "rex-recap"

// rexBin is the rex CLI: the one in REX_BIN_DIR when Rex says where, else
// rex on PATH.
func rexBin() string {
	if d := os.Getenv("REX_BIN_DIR"); d != "" {
		if p := filepath.Join(d, "rex"); isExecutable(p) {
			return p
		}
	}
	return "rex"
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// rexRun runs the rex CLI with stdin and returns what it prints. A variable
// so tests can stand Rex in.
var rexRun = func(stdin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rexBin(), args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("rex %s: %s", args[0], msg)
		}
		return out, fmt.Errorf("rex %s: %w", args[0], err)
	}
	return out, nil
}

// rexScript runs a Lua script with rex do, with data as rex.args.data, and
// decodes what it returns into out.
func rexScript(script string, data any, out any) error {
	args := []string{"do", "-"}
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		args = append(args, "data="+string(b))
	}
	res, err := rexRun(script, args...)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(res, out)
}

// rexSnapshotLua lists every block in every session, with what recap needs
// of each, in one call: a script can reach every session (it attaches to
// each), where the CLI would need a call per block.
const rexSnapshotLua = `
local T = "com.superlogical.terminal"
local out = { sessions = {}, blocks = {} }
local list, err = rex.call("session.list", {})
if list == nil then error(err) end
for _, s in ipairs(list.sessions or {}) do
  table.insert(out.sessions, { session_id = s.session_id, label = s.label })
  rex.session.attach{ session_id = s.session_id }
  local view = rex.call("session.view", { session_id = s.session_id })
  for _, w in ipairs((view or {}).windows or {}) do
    for _, layer in ipairs(w.layers or {}) do
      for _, b in ipairs(layer.blocks or {}) do
        if b.creator_name == T then
          local args = { session_id = s.session_id, block_id = b.block_id }
          table.insert(out.blocks, {
            session_id = s.session_id,
            window_id = w.window_id,
            window_label = w.label,
            layer = layer.kind,
            block_id = b.block_id,
            label = b.label,
            focused = view.active_window_id == w.window_id and w.focused_block_id == b.block_id,
            status = rex.block.call(T, "program_status", args),
            process = rex.block.call(T, "process", args),
            title = rex.block.call(T, "title", args),
          })
        end
      end
    end
  end
end
return out
`

type rexSnapshot struct {
	Sessions lenientList[rexSessionInfo] `json:"sessions"`
	Blocks   lenientList[rexBlock]       `json:"blocks"`
}

type rexSessionInfo struct {
	SessionID string `json:"session_id"`
	Label     string `json:"label"`
}

type rexBlock struct {
	SessionID   string     `json:"session_id"`
	WindowID    string     `json:"window_id"`
	WindowLabel string     `json:"window_label"`
	Layer       string     `json:"layer"`
	BlockID     string     `json:"block_id"`
	Label       string     `json:"label"`
	Focused     bool       `json:"focused"`
	Status      rexStatus  `json:"status"`
	Process     rexProcess `json:"process"`
	Title       struct {
		Title string `json:"title"`
	} `json:"title"`
}

type rexStatus struct {
	Records lenientList[rexRecord] `json:"records"`
}

// rexRecord is one program status record (OSC 7501).
type rexRecord struct {
	ID           string    `json:"id"`
	State        string    `json:"state"`
	Kind         string    `json:"kind"`
	App          string    `json:"app"`
	EffectiveApp string    `json:"effective_app"`
	Msg          string    `json:"msg"`
	UpdatedAt    time.Time `json:"updated_at"`
	Owner        struct {
		PID int `json:"pid"`
	} `json:"owner"`
}

// rexProcess is the terminal's process method: the program it started
// (child) and the one in the foreground now.
type rexProcess struct {
	Child      *rexProc `json:"child"`
	Foreground *rexProc `json:"foreground"`
}

type rexProc struct {
	PID int `json:"pid"`
	// Name is the executable's name, which for Claude Code is its version
	// ("2.1.321"): Argv0 and InvokedPath say what was run.
	Name        string `json:"name"`
	Argv0       string `json:"argv0"`
	InvokedPath string `json:"invoked_path"`
	Path        string `json:"path"`
	Cwd         string `json:"cwd"`
}

// lenientList is a JSON array that Lua may hand over as {} when it's empty.
type lenientList[T any] []T

func (l *lenientList[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("{}")) || bytes.Equal(b, []byte("null")) {
		*l = nil
		return nil
	}
	var items []T
	if err := json.Unmarshal(b, &items); err != nil {
		return err
	}
	*l = items
	return nil
}

func rexSnapshotNow() (rexSnapshot, error) {
	var snap rexSnapshot
	err := rexScript(rexSnapshotLua, nil, &snap)
	return snap, err
}

// rexAgentApps are the apps that report program status, by the agent names
// recap uses.
var rexAgentApps = map[string]string{
	"claude-code": "claude",
	"claude":      "claude",
	"codex":       "codex",
	"opencode":    "opencode",
	"hermes":      "hermes",
	"omp":         "omp",
}

// root is a block's own status: the record without an id, else the newest.
func (s rexStatus) root() *rexRecord {
	var newest *rexRecord
	for i := range s.Records {
		r := &s.Records[i]
		if r.ID == "" {
			return r
		}
		if newest == nil || r.UpdatedAt.After(newest.UpdatedAt) {
			newest = r
		}
	}
	return newest
}

// command is what a process was started as: the base of what was run.
func (p *rexProc) command() string {
	if p == nil {
		return ""
	}
	for _, c := range []string{p.InvokedPath, p.Argv0, p.Name} {
		if c != "" {
			return filepath.Base(c)
		}
	}
	return ""
}

// agentOf says which agent a block runs, if any: by the app its status names,
// else by the program in its foreground.
func agentOf(b rexBlock) string {
	if r := b.Status.root(); r != nil {
		app := r.EffectiveApp
		if app == "" {
			app = r.App
		}
		if name, ok := rexAgentApps[app]; ok {
			return name
		}
	}
	if supportedAgent(b.Process.Foreground.command()) {
		return b.Process.Foreground.command()
	}
	return ""
}

// rexStatusOf is a block's status in herdr's terms, and its change number:
// done once you've looked is idle, as herdr has it, and looking moves the
// number on as a status change would.
func rexStatusOf(b rexBlock, seen time.Time) (string, int64) {
	r := b.Status.root()
	if r == nil {
		return "idle", 0
	}
	seq := r.UpdatedAt.UnixNano()
	state := r.State
	switch state {
	case "error":
		// Failed and stopped: it's yours to look at, as a finished turn is.
		state = "done"
	case "idle", "working", "done", "blocked":
	default:
		state = "idle"
	}
	if state == "done" && seen.After(r.UpdatedAt) {
		return "idle", seen.UnixNano()
	}
	return state, seq
}

// rexTitle is a terminal title without the spinner or mark an agent puts in
// front of it.
func rexTitle(t string) string {
	t = strings.TrimLeftFunc(t, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.TrimSpace(t)
}

// rexAgents is herdr's agent.list from a snapshot: the blocks running an
// agent (which recap's own popup isn't). A done block that's focused right
// now counts as seen.
func rexAgents(snap rexSnapshot, now time.Time) []agentInfo {
	var agents []agentInfo
	for _, b := range snap.Blocks {
		agent := agentOf(b)
		if agent == "" {
			continue
		}
		seen := seenAt(b.BlockID)
		if b.Focused {
			if r := b.Status.root(); r != nil && r.State == "done" && !seen.After(r.UpdatedAt) {
				seen = now
				markSeen(b.BlockID, now)
			}
		}
		status, seq := rexStatusOf(b, seen)
		cwd := ""
		if p := b.Process.Foreground; p != nil {
			cwd = p.Cwd
		}
		agents = append(agents, agentInfo{
			PaneID:         b.BlockID,
			WorkspaceID:    b.SessionID,
			TabID:          b.WindowID,
			Agent:          agent,
			Name:           b.Label,
			Status:         status,
			Focused:        b.Focused,
			Cwd:            cwd,
			Title:          rexTitle(b.Title.Title),
			StateChangeSeq: seq,
		})
	}
	sanitize(&agents)
	return agents
}

func rexListAgents() ([]agentInfo, error) {
	snap, err := rexSnapshotNow()
	if err != nil {
		return nil, err
	}
	return rexAgents(snap, time.Now()), nil
}

func rexListWorkspaces() []workspaceInfo {
	snap, err := rexSnapshotNow()
	if err != nil {
		return nil
	}
	var out []workspaceInfo
	for _, s := range snap.Sessions {
		out = append(out, workspaceInfo{WorkspaceID: s.SessionID, Label: s.Label})
	}
	sanitize(&out)
	return out
}

// rexProcesses is herdr's pane.process_info: the program the terminal
// started, then the one in its foreground, as herdr lists them outermost
// first.
func rexProcesses(blockID string) ([]process, error) {
	out, err := rexRun("", "block", "call", "-b", blockID, "process")
	if err != nil {
		return nil, err
	}
	var p rexProcess
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, err
	}
	return rexProcessList(p), nil
}

func rexProcessList(p rexProcess) []process {
	var procs []process
	for _, rp := range []*rexProc{p.Child, p.Foreground} {
		if rp == nil || rp.PID == 0 || (len(procs) > 0 && procs[len(procs)-1].PID == rp.PID) {
			continue
		}
		run := rp.InvokedPath
		if run == "" {
			run = rp.Argv0
		}
		procs = append(procs, process{PID: rp.PID, Name: rp.command(), Argv: []string{run}, Cwd: rp.Cwd})
	}
	return procs
}

// rexFocusLua focuses a block in whichever session has it.
const rexFocusLua = `
local want = rex.args.data.block_id
for _, s in ipairs((rex.call("session.list", {}) or {}).sessions or {}) do
  rex.session.attach{ session_id = s.session_id }
  local view = rex.call("session.view", { session_id = s.session_id })
  for _, w in ipairs((view or {}).windows or {}) do
    for _, layer in ipairs(w.layers or {}) do
      for _, b in ipairs(layer.blocks or {}) do
        if b.block_id == want then
          local ok, err = rex.call("session.focus_block", { session_id = s.session_id, block_id = want })
          if ok == nil then error(err) end
          return { session_id = s.session_id }
        end
      end
    end
  end
end
error("no block " .. want)
`

// rexFocus focuses an agent's block. From recap's own popup it waits for
// the popup to close first: Rex gives the focus back to the pane that had it
// when a floating pane closes, which would undo it.
func rexFocus(blockID string) error {
	if own := os.Getenv("REX_BLOCK"); own != "" && own != blockID {
		return spawn("rex-focus", "--after", own, blockID)
	}
	return rexFocusNow(blockID)
}

func rexFocusNow(blockID string) error {
	return rexScript(rexFocusLua, map[string]string{"block_id": blockID}, nil)
}

// runRexFocus is `rex-focus --after BLOCK TARGET`: wait for BLOCK to close,
// then focus TARGET.
func runRexFocus(args []string) error {
	if len(args) == 3 && args[0] == "--after" {
		// rex wait returns once the block has gone; a block that's gone
		// already returns at once (with an error, which is fine).
		_, _ = rexRunWait("wait", args[1])
		time.Sleep(50 * time.Millisecond)
		return rexFocusNow(args[2])
	}
	if len(args) == 1 {
		return rexFocusNow(args[0])
	}
	return errors.New("rex-focus needs [--after BLOCK] TARGET")
}

// rexRunWait is rexRun without its time limit: rex wait lasts as long as the
// popup does.
var rexRunWait = func(args ...string) ([]byte, error) {
	return exec.Command(rexBin(), args...).Output()
}

// rexPrompt is herdr's agent.prompt: the text pasted into the agent's
// terminal (bracketed, so a newline in it doesn't send it early), then
// enter, written apart so the agent takes the paste first.
func rexPrompt(blockID, text string) error {
	paste := "\x1b[200~" + text + "\x1b[201~"
	for _, data := range []string{paste, "\r"} {
		b, err := json.Marshal(map[string]string{"data": data})
		if err != nil {
			return err
		}
		if _, err := rexRun("", "block", "call", "-b", blockID, "write", string(b)); err != nil {
			return err
		}
		time.Sleep(80 * time.Millisecond)
	}
	return nil
}

func rexRenameSession(sessionID, label string) error {
	b, err := json.Marshal(map[string]string{"session_id": sessionID, "label": label})
	if err != nil {
		return err
	}
	_, err = rexRun("", "api", "call", "-s", sessionID, "session.set_label", string(b))
	return err
}

// seenDir holds, per block, when it was last seen focused.
func seenDir() string { return filepath.Join(stateDir(), "seen") }

func seenPath(blockID string) string {
	return filepath.Join(seenDir(), notAlnum.ReplaceAllString(blockID, "_"))
}

// seenAt is when the block was last seen focused: zero if never.
func seenAt(blockID string) time.Time {
	data, err := os.ReadFile(seenPath(blockID))
	if err != nil {
		return time.Time{}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func markSeen(blockID string, at time.Time) {
	if blockID == "" || os.MkdirAll(seenDir(), 0o700) != nil {
		return
	}
	tmp := seenPath(blockID) + ".tmp"
	if os.WriteFile(tmp, []byte(strconv.FormatInt(at.UnixNano(), 10)), 0o600) == nil {
		_ = os.Rename(tmp, seenPath(blockID))
	}
}

// rexOpenLua opens recap's popup as a floating pane in a session, or closes
// it when it's open there: what the Lua module's toggle does, for running by
// hand (`rex-recap action open`).
const rexOpenLua = `
local d = rex.args.data
rex.session.attach{ session_id = d.session_id }
local list = rex.call("session.list_blocks", { session_id = d.session_id }) or {}
for _, b in ipairs(list.blocks or {}) do
  if b.label == d.label then
    rex.call("block.close", { session_id = d.session_id, block_id = b.block_id })
    return { closed = b.block_id }
  end
end
local r, err = rex.call("session.new_layer", {
  session_id = d.session_id,
  bounds = { x = 0.1, y = 0.08, w = 0.8, h = 0.8 },
  focus = true,
  layout = { block = {
    creator_name = "com.superlogical.terminal",
    flavor = "com.superlogical.terminal.shell",
    label = d.label,
    options = { command = d.command, cwd = d.cwd },
  } },
})
if r == nil then error(err) end
return r
`

// rexOpenPopup opens (or closes) the popup in the session this runs in, or
// else the first one.
func rexOpenPopup(demo bool) error {
	session := os.Getenv("REX_SESSION")
	if session == "" {
		snap, err := rexSnapshotNow()
		if err != nil {
			return err
		}
		if len(snap.Sessions) == 0 {
			return errors.New("no Rex session to open it in")
		}
		session = snap.Sessions[0].SessionID
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	command := []string{"env", hostEnv + "=rex", self, "picker"}
	if demo {
		command = append(command, "--demo")
	}
	home, _ := os.UserHomeDir()
	return rexScript(rexOpenLua, map[string]any{
		"session_id": session, "label": rexPopupLabel, "command": command, "cwd": home,
	}, nil)
}
