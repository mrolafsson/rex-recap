package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The tests that came with herdr-recap are herdr's: run them against herdr,
// whatever the machine running them has set. Rex's tests pick Rex (useRex).
func TestMain(m *testing.M) {
	os.Setenv(hostEnv, "herdr")
	os.Exit(m.Run())
}

// useRex makes Rex the host for a test, with its state in a folder of its own.
func useRex(t *testing.T) {
	t.Helper()
	old := onRex
	onRex = func() bool { return true }
	t.Cleanup(func() { onRex = old })
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// fakeRex stands in for the rex CLI: reply answers each call.
func fakeRex(t *testing.T, reply func(stdin string, args []string) (string, error)) *[][]string {
	t.Helper()
	var calls [][]string
	old := rexRun
	rexRun = func(stdin string, args ...string) ([]byte, error) {
		calls = append(calls, args)
		out, err := reply(stdin, args)
		return []byte(out), err
	}
	t.Cleanup(func() { rexRun = old })
	return &calls
}

// snapshotFixture is a real snapshot (paths made up): a session with a
// shell, Claude Code asking a question, and recap's own popup.
func snapshotFixture(t *testing.T) rexSnapshot {
	t.Helper()
	data, err := os.ReadFile("testdata/rex/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var snap rexSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestRexSnapshotParses(t *testing.T) {
	snap := snapshotFixture(t)
	if len(snap.Sessions) != 1 || snap.Sessions[0].Label != "fixture" {
		t.Fatalf("sessions = %+v", snap.Sessions)
	}
	shell := snap.Blocks[0]
	if shell.Label != "shell" || len(shell.Status.Records) != 0 {
		t.Errorf("shell = %+v: its records come as {} from Lua, and are none", shell)
	}
	agent := snap.Blocks[1]
	r := agent.Status.root()
	if r == nil || r.State != "blocked" || r.Kind != "question" || r.App != "claude-code" {
		t.Fatalf("agent's status = %+v", r)
	}
	if !strings.Contains(r.Msg, "tea or coffee") || r.UpdatedAt.IsZero() || r.Owner.PID == 0 {
		t.Errorf("agent's status = %+v", r)
	}
	if agent.Process.Foreground.command() != "claude" {
		t.Errorf("command = %q: Claude's process name is its version, so it's what was run", agent.Process.Foreground.command())
	}
}

func TestRexAgents(t *testing.T) {
	useRex(t)
	snap := snapshotFixture(t)
	snap.Blocks[1].Focused = false
	agents := rexAgents(snap, time.Now())
	if len(agents) != 1 {
		t.Fatalf("agents = %+v: only Claude, not the shell or recap's popup", agents)
	}
	a := agents[0]
	b := snap.Blocks[1]
	if a.PaneID != b.BlockID || a.WorkspaceID != b.SessionID || a.TabID != b.WindowID {
		t.Errorf("ids = %s %s %s", a.PaneID, a.WorkspaceID, a.TabID)
	}
	if a.Agent != "claude" || a.Status != "blocked" || a.Cwd != "/home/me/src/app" || a.Name != "agent" {
		t.Errorf("agent = %+v", a)
	}
	if a.StateChangeSeq != b.Status.root().UpdatedAt.UnixNano() {
		t.Errorf("seq = %d, not the status's time", a.StateChangeSeq)
	}
	if !recapCapable(withDefaults(config{}), a) {
		t.Error("a Claude agent under Rex isn't recapped")
	}
}

func TestRexAgentByProcess(t *testing.T) {
	// Codex before it has reported a status: known by what's running.
	b := rexBlock{Process: rexProcess{Foreground: &rexProc{Name: "codex", Argv0: "codex", InvokedPath: "/usr/local/bin/codex"}}}
	if got := agentOf(b); got != "codex" {
		t.Errorf("agentOf = %q", got)
	}
	b = rexBlock{Process: rexProcess{Foreground: &rexProc{Name: "vim", Argv0: "vim"}}}
	if got := agentOf(b); got != "" {
		t.Errorf("vim is an agent: %q", got)
	}
	b = rexBlock{Status: rexStatus{Records: lenientList[rexRecord]{{State: "working", App: "brew"}}}}
	if got := agentOf(b); got != "" {
		t.Errorf("brew reporting status is an agent: %q", got)
	}
}

func TestRexStatusSeen(t *testing.T) {
	at := time.Date(2026, 11, 9, 21, 0, 0, 0, time.UTC)
	done := rexBlock{Status: rexStatus{Records: lenientList[rexRecord]{{State: "done", UpdatedAt: at}}}}
	cases := []struct {
		name  string
		b     rexBlock
		seen  time.Time
		state string
		seq   int64
	}{
		{"done, not looked at", done, time.Time{}, "done", at.UnixNano()},
		{"done, looked at before it finished", done, at.Add(-time.Minute), "done", at.UnixNano()},
		// herdr makes a done agent idle once you look: so does this, and
		// the look is a change, so a recap waiting for it is called off.
		{"done, looked at since", done, at.Add(time.Minute), "idle", at.Add(time.Minute).UnixNano()},
		{"failed", rexBlock{Status: rexStatus{Records: lenientList[rexRecord]{{State: "error", UpdatedAt: at}}}}, time.Time{}, "done", at.UnixNano()},
		{"blocked, looked at since", rexBlock{Status: rexStatus{Records: lenientList[rexRecord]{{State: "blocked", UpdatedAt: at}}}}, at.Add(time.Minute), "blocked", at.UnixNano()},
		{"no status", rexBlock{}, time.Time{}, "idle", 0},
	}
	for _, c := range cases {
		state, seq := rexStatusOf(c.b, c.seen)
		if state != c.state || seq != c.seq {
			t.Errorf("%s: %s %d, want %s %d", c.name, state, seq, c.state, c.seq)
		}
	}
}

func TestRexFocusedDoneIsSeen(t *testing.T) {
	useRex(t)
	at := time.Date(2026, 11, 9, 21, 0, 0, 0, time.UTC)
	snap := rexSnapshot{Blocks: lenientList[rexBlock]{{
		BlockID: "block:a", Focused: true,
		Status:  rexStatus{Records: lenientList[rexRecord]{{State: "done", App: "claude-code", UpdatedAt: at}}},
	}}}
	now := at.Add(time.Second)
	if a := rexAgents(snap, now); len(a) != 1 || a[0].Status != "idle" {
		t.Fatalf("a done agent you're looking at = %+v", a)
	}
	if !seenAt("block:a").Equal(now) {
		t.Errorf("seen = %v: it's noted, so it stays seen when you look away", seenAt("block:a"))
	}
	snap.Blocks[0].Focused = false
	if a := rexAgents(snap, now.Add(time.Minute)); a[0].Status != "idle" {
		t.Errorf("after looking away: %s", a[0].Status)
	}
}

func TestRexTitle(t *testing.T) {
	for in, want := range map[string]string{
		"✳ Tea or coffee preference": "Tea or coffee preference",
		"⠂ Fix the build":            "Fix the build",
		"Plain":                      "Plain",
		"  ":                         "",
	} {
		if got := rexTitle(in); got != want {
			t.Errorf("rexTitle(%q) = %q", in, got)
		}
	}
}

func TestRexProcesses(t *testing.T) {
	useRex(t)
	snap := snapshotFixture(t)
	procs := rexProcessList(snap.Blocks[1].Process)
	if len(procs) != 1 {
		t.Fatalf("procs = %+v: child and foreground are the same process", procs)
	}
	pid, argv0 := claudePID(procs)
	if pid != snap.Blocks[1].Process.Foreground.PID || argv0 != "/home/me/.local/bin/claude" {
		t.Errorf("claudePID = %d %q", pid, argv0)
	}
	// Claude started from a shell: the shell, then Claude.
	procs = rexProcessList(rexProcess{
		Child:      &rexProc{PID: 1, Name: "zsh", Argv0: "-zsh", InvokedPath: "/bin/zsh"},
		Foreground: &rexProc{PID: 2, Name: "2.1.321", Argv0: "claude"},
	})
	if pid, _ := claudePID(procs); len(procs) != 2 || pid != 2 {
		t.Errorf("procs = %+v, claude = %d", procs, pid)
	}

	calls := fakeRex(t, func(_ string, args []string) (string, error) {
		b, _ := json.Marshal(snap.Blocks[1].Process)
		return string(b), nil
	})
	got, err := paneProcesses("block:x")
	if err != nil || len(got) != 1 {
		t.Fatalf("paneProcesses = %+v %v", got, err)
	}
	if want := "block call -b block:x process"; strings.Join((*calls)[0], " ") != want {
		t.Errorf("called rex %v", (*calls)[0])
	}
}

func TestRexListAgentsThroughCLI(t *testing.T) {
	useRex(t)
	data, _ := os.ReadFile("testdata/rex/snapshot.json")
	calls := fakeRex(t, func(stdin string, args []string) (string, error) {
		if args[0] != "do" || !strings.Contains(stdin, "session.list") {
			t.Errorf("rex %v", args)
		}
		return string(data), nil
	})
	agents, err := listAgents()
	if err != nil || len(agents) != 1 || agents[0].Agent != "claude" {
		t.Fatalf("listAgents = %+v %v", agents, err)
	}
	ws := listWorkspaces()
	if len(ws) != 1 || ws[0].Label != "fixture" {
		t.Errorf("workspaces = %+v", ws)
	}
	if len(*calls) != 2 {
		t.Errorf("%d calls: one snapshot each", len(*calls))
	}
}

func TestRexPrompt(t *testing.T) {
	useRex(t)
	var writes []string
	fakeRex(t, func(_ string, args []string) (string, error) {
		if strings.Join(args[:4], " ") != "block call -b block:a" || args[4] != "write" {
			t.Errorf("rex %v", args)
		}
		var w struct{ Data string }
		_ = json.Unmarshal([]byte(args[5]), &w)
		writes = append(writes, w.Data)
		return `{"bytes_written":1}`, nil
	})
	if err := promptAgent("block:a", "two\nlines"); err != nil {
		t.Fatal(err)
	}
	// Pasted, so the newline doesn't send it early; then entered.
	if len(writes) != 2 || writes[0] != "\x1b[200~two\nlines\x1b[201~" || writes[1] != "\r" {
		t.Errorf("writes = %q", writes)
	}
}

func TestRexFocusFromPopupWaits(t *testing.T) {
	useRex(t)
	var spawned [][]string
	old := spawn
	spawn = func(args ...string) error { spawned = append(spawned, args); return nil }
	t.Cleanup(func() { spawn = old })
	t.Setenv("REX_BLOCK", "block:popup")
	if err := focusAgent("block:a"); err != nil {
		t.Fatal(err)
	}
	if len(spawned) != 1 || strings.Join(spawned[0], " ") != "rex-focus --after block:popup block:a" {
		t.Errorf("spawned %v: closing the popup would take the focus back", spawned)
	}
}

func TestRexFocusNow(t *testing.T) {
	useRex(t)
	t.Setenv("REX_BLOCK", "")
	calls := fakeRex(t, func(stdin string, args []string) (string, error) {
		if !strings.Contains(stdin, "session.focus_block") {
			t.Errorf("script doesn't focus: %s", stdin)
		}
		return `{"session_id":"s"}`, nil
	})
	if err := focusAgent("block:a"); err != nil {
		t.Fatal(err)
	}
	if args := (*calls)[0]; args[0] != "do" || args[2] != `data={"block_id":"block:a"}` {
		t.Errorf("rex %v", args)
	}
}

func TestRexHostChoice(t *testing.T) {
	vars := []string{hostEnv, "HERDR_PLUGIN_ID", "HERDR_SOCKET_PATH", "REX_SESSION", "REX_BLOCK"}
	cases := []struct {
		set  map[string]string
		want bool
	}{
		{map[string]string{}, defaultHost == "rex"},
		{map[string]string{"REX_SESSION": "s"}, true},
		{map[string]string{"HERDR_SOCKET_PATH": "/x"}, false},
		// A Rex server started from a herdr pane passes herdr's on.
		{map[string]string{"HERDR_SOCKET_PATH": "/x", "REX_BLOCK": "b"}, true},
		// herdr running recap as its plugin, in a Rex pane.
		{map[string]string{"HERDR_PLUGIN_ID": "herdr-recap", "REX_BLOCK": "b"}, false},
		{map[string]string{hostEnv: "rex", "HERDR_PLUGIN_ID": "herdr-recap"}, true},
		{map[string]string{hostEnv: "herdr", "REX_BLOCK": "b"}, false},
	}
	for _, c := range cases {
		for _, v := range vars {
			t.Setenv(v, c.set[v])
		}
		if got := onRex(); got != c.want {
			t.Errorf("with %v: onRex = %v", c.set, got)
		}
	}
}

func TestRexDirs(t *testing.T) {
	useRex(t)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("XDG_STATE_HOME", "/state")
	if configDir() != "/cfg/rex-recap" || stateDir() != "/state/rex-recap" {
		t.Errorf("dirs = %s %s", configDir(), stateDir())
	}
}

func TestRexNoSiblings(t *testing.T) {
	useRex(t)
	fakeRex(t, func(string, []string) (string, error) {
		t.Error("asked Rex for herdr's plugins")
		return "", nil
	})
	if s := siblings(); s != nil {
		t.Errorf("siblings = %v", s)
	}
}

// Events as rex events --json writes them.
const (
	statusEvent = `{"type":"session_event","payload":{"session_id":"session:s","event":{"type":"block_event","payload":{"block_id":"block:a","creator_name":"com.superlogical.terminal","name":"program_status_changed","payload":{"id":"","record":{"state":"done","app":"claude-code","effective_app":"claude-code","updated_at":"2026-11-09T21:53:18.881757706+01:00","owner":{"pid":32797,"name":"2.1.321"}},"reason":"report"}}}}}`
	childEvent  = `{"type":"session_event","payload":{"session_id":"session:s","event":{"type":"block_event","payload":{"block_id":"block:a","creator_name":"com.superlogical.terminal","name":"program_status_changed","payload":{"id":"build/test","record":{"state":"working"},"reason":"report"}}}}}`
	viewEvent   = `{"type":"session_event","payload":{"session_id":"session:s","event":{"type":"session_view_changed","payload":{"revision":3}}}}`
	clientEvent = `{"type":"client_joined","payload":{"client":{"client_id":"client:c"}}}`
)

func TestHandleEvent(t *testing.T) {
	var ticked, marked []string
	oldTick, oldMark := tickFor, markFocused
	tickFor = func(_ config, only string) error { ticked = append(ticked, only); return nil }
	markFocused = func(s string) { marked = append(marked, s) }
	t.Cleanup(func() { tickFor, markFocused = oldTick, oldMark })

	cfg := withDefaults(config{})
	for _, line := range []string{statusEvent, childEvent, viewEvent, clientEvent, "not json"} {
		handleEvent(cfg, []byte(line))
	}
	if len(ticked) != 1 || ticked[0] != "block:a" {
		t.Errorf("ticked %v: once, for the block's own status", ticked)
	}
	if len(marked) != 1 || marked[0] != "session:s" {
		t.Errorf("marked %v", marked)
	}
}

func TestFocusedBlock(t *testing.T) {
	view := `{"active_window_id":"window:2","windows":[{"window_id":"window:1","focused_block_id":"block:1"},{"window_id":"window:2","focused_block_id":"block:2"}]}`
	if got := focusedBlock([]byte(view)); got != "block:2" {
		t.Errorf("focusedBlock = %q", got)
	}
}

func TestWatchLock(t *testing.T) {
	useRex(t)
	if watching() {
		t.Fatal("watching before any watcher")
	}
	lock, ok := lockWatch()
	if !ok {
		t.Fatal("no lock")
	}
	if !watching() {
		t.Error("not watching with the lock held")
	}
	if _, ok := lockWatch(); ok {
		t.Error("a second watcher got the lock")
	}
	lock.Close()
	if watching() {
		t.Error("watching after the lock went")
	}
}

func TestRexOpenPopup(t *testing.T) {
	useRex(t)
	t.Setenv("REX_SESSION", "session:s")
	calls := fakeRex(t, func(stdin string, args []string) (string, error) {
		return `{}`, nil
	})
	if err := runAction("open"); err != nil {
		t.Fatal(err)
	}
	args := (*calls)[0]
	var data struct {
		SessionID string   `json:"session_id"`
		Label     string   `json:"label"`
		Command   []string `json:"command"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(args[2], "data=")), &data); err != nil {
		t.Fatal(err)
	}
	if data.SessionID != "session:s" || data.Label != rexPopupLabel {
		t.Errorf("data = %+v", data)
	}
	// The popup knows it's under Rex, whatever the server's environment says.
	if len(data.Command) < 4 || strings.Join(data.Command[:2], " ") != "env RECAP_HOST=rex" || data.Command[3] != "picker" {
		t.Errorf("command = %v", data.Command)
	}
}
