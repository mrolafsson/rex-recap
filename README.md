# rex-recap

**Come back to a bunch of agents and know in seconds which ones need you.**

Starting a new agent is almost too easy. Coming back to a dozen or more and
remembering what each one was doing is the pain. rex-recap tells you, per
agent, where it got to and whose move it is, in a floating pane in
[Rex](https://www.superlogical.com/rex).

It's [herdr-recap](https://github.com/mrolafsson/herdr-recap), the same
program, with Rex as its host instead of herdr.

- **Recaps are written while you're away.** Claude Code's own `/recap` runs
  in a throwaway fork, so your agent's conversation isn't touched.
- **Only when it's worth it.** A recap is written once an agent has sat
  finished or blocked for a few minutes without you looking, while its
  prompt cache is still warm.
- **Grouped by whose move it is.** Needs you, Done, Idle and Working, as tabs
  with counts.
- **See what a blocked agent wants.** The pending question is right on the
  row.
- **Reply without leaving the list.** `tab`, type, `enter`. Or `enter` to jump
  to the agent.
- **Context at a glance.** Each row has the branch, uncommitted and unpushed
  work, task progress, and how long it has been waiting.
- **Type to filter** across titles, recaps, branches and sessions. When no
  row matches, the conversations are searched for what you typed.
- **Better titles.** Agents with no title get a suggested one, applied with
  one key.

Rex's own Activity view shows which agents are working or waiting. rex-recap
adds what they were doing, what they're asking, and a way to answer.

**Contents:** [Install](#install) · [Use](#use) · [Keys](#keys) ·
[When recaps are written](#when-recaps-are-written) ·
[What it costs](#what-it-costs) · [Configuration](#configuration) ·
[Privacy](#privacy) · [How it works](#how-it-works) ·
[Limits](#limits) · [Development](#development)

## Install

There are two parts: the `rex-recap` binary, on the machine your Rex server
runs on, and a small Lua module for your Rex config.

**1. Build the binary** (Go 1.26 or later):

```sh
git clone https://github.com/mrolafsson/rex-recap.git
cd rex-recap
go build -o ~/.local/bin/rex-recap .
```

`~/.local/bin` has to be on your login shell's `PATH` (`which rex-recap`).
If it isn't, set `bin` below to the binary's full path.

**2. Link the Lua module** next to your `init.lua`, from inside the clone:

```sh
mkdir -p ~/.config/rex
ln -s "$PWD/rex/rex-recap.lua" ~/.config/rex/rex-recap.lua
```

**3. Turn it on.** Add this to `~/.config/rex/init.lua`:

```lua
require("rex-recap").setup{ key = "cmd+shift+r" }
```

then apply it:

```sh
rex config check && rex config reload
```

Pick any key that's free (`rex keymap` shows what's taken), or leave `key`
out and use the command palette.

**Updating**, from inside the clone:

```sh
git pull && go build -o ~/.local/bin/rex-recap .
```

### Requirements

- **Rex**, the Mac app or a Linux server. Tried with the snapshot of
  9 November 2026.
- **Claude Code** with `/recap` (2.1.x), signed in. Claude Code reports its
  status to Rex itself, which is how rex-recap knows what each agent is doing.
- Optional: **Codex**, for Codex recaps.
- **Linux** or **macOS**.

## Use

Press your key, or run **Recap: Toggle** from the command palette. The list
opens in a floating pane over your tab. Press the key again, or `esc`, to
close it.

**Recap: Demo** shows the list on made-up agents: try it, or take
screenshots without showing real work.

From a shell: `rex do recap_toggle`, or `rex-recap action open` (opens it in
the session you're in).

## Keys

| Key | Does |
| --- | --- |
| typing | filter; with no row matching, search the conversations |
| `↑` `↓`, `ctrl+p` `ctrl+n` | move |
| `pgup` / `pgdn`, `home` / `end` | page, first / last |
| `enter`, click | go to that agent and close |
| `tab` | reply to that agent: type, then `enter` to send, `esc` to drop it; `alt+enter` or `ctrl+j` for a new line |
| `ctrl+r` | rename that agent to the title suggested under its own; with none suggested, ask for one |
| `ctrl+x` | ignore the suggested title: it isn't offered again |
| `←` / `→` | the stage before / after |
| `esc` | clear the filter, then back to *All*, then close |

An agent stopped on a question can't take a typed reply, so for those
`enter` takes you to answer it there.

## When recaps are written

Soon after a turn ends. When an agent finishes (done) or stops to ask you
something (blocked), rex-recap waits `recap_after_seconds` (3 minutes). If by
then you still haven't looked at it, its recap is written. If you have, or
you answered it, nothing is spent.

"Looked at" means you focused its pane. Rex keeps a finished agent's status
at done until it starts again, so rex-recap notes when each pane is focused,
and treats a done agent you've focused since it finished as idle.

Opening the list also rewrites any recap that no longer matches its
conversation (an agent that has carried on working, say), a few at a time.

All of this is done by a small background process, `rex-recap watch`, which
follows Rex's events. The list starts it when it isn't running, so recaps
are written from the first time you open it. To have it running from login,
start `rex-recap watch` with launchd or a systemd user service. Only one runs
at a time.

## What it costs

A Claude recap is one short Claude request on the agent's own conversation,
billed like any other (or counted against your plan). Written soon after the
turn, it reuses that session's prompt cache: about 2¢ at list price on a
~70k-token conversation in testing. At most one recap per agent each time you
walk away from it.

Everything else on a row is read from files and git on your machine, and
costs nothing. herdr-recap's README has
[the detail](https://github.com/mrolafsson/herdr-recap#what-it-costs),
including other agents.

## Configuration

Optional: `~/.config/rex-recap/config.json` (`$XDG_CONFIG_HOME/rex-recap`).
The settings are herdr-recap's; see [config.example.json](config.example.json).

| Key | Default | Meaning |
| --- | --- | --- |
| `recap_after_seconds` | `180` | How long an agent waits on you, unlooked at, before its recap is written |
| `language` | the agent's default | Language for recaps and suggested titles, such as `"Japanese"` or `"zh-CN"` |
| `claude` | the agent's own | Claude Code binary for recaps |
| `agents` | `["claude", "codex", "omp"]` | The agents to recap |
| `timeout_seconds` | `120` | The longest one recap may take |
| `theme` | ask the terminal | `"dark"` or `"light"` |

Options for the Lua module, in `setup{}`:

| Option | Default | Meaning |
| --- | --- | --- |
| `bin` | `"rex-recap"` | The binary, on your login shell's `PATH` or as a full path |
| `key` | none | A key for Recap: Toggle |
| `bounds` | `{ 0.1, 0.08, 0.8, 0.8 }` | The floating pane's `{ x, y, w, h }`, as fractions of the window |

## Privacy

Claude recaps are made by the same Claude Code, account and settings the
agent uses, on its own conversation, so nothing goes anywhere it hadn't
already. The recap is a fork that's never saved: the agent's session isn't
written to. Recaps are kept in `~/.local/state/rex-recap/recaps/`, readable
only by you. Searching the conversations reads transcripts on your disk and
sends nothing anywhere. More in herdr-recap's
[Privacy](https://github.com/mrolafsson/herdr-recap#privacy).

## How it works

| herdr-recap gets from herdr | rex-recap gets from Rex |
| --- | --- |
| The agent list | Every terminal block in every session, with the status its program reports ([Program Status Protocol](https://www.superlogical.com/rex/docs/build/program-status)), from one `rex do` script |
| An agent's process | The terminal's `process` method |
| Status-change hooks | `rex events`, followed by `rex-recap watch` |
| Done turning idle when you look | Focus changes, noted by the watcher |
| Workspaces | Sessions |
| A popup | A floating pane, opened by the Lua module |
| Go to an agent | `session.focus_block`, once the floating pane has closed |
| Typing a reply | A bracketed paste and enter, written to the agent's terminal |

The code is herdr-recap's with a Rex layer: [`host.go`](host.go) picks the
host, [`rex.go`](rex.go) and [`watch.go`](watch.go) are Rex's side, and
[`rex/rex-recap.lua`](rex/rex-recap.lua) is the module. herdr is still
supported (`RECAP_HOST=herdr`), which keeps herdr-recap's changes easy to
bring over.

## Limits

- **No links to other plugins.** herdr-recap's GitHub, Linear and Snooze
  links come from herdr plugins that Rex doesn't have, so the PR and issue
  on a row, and snoozing, aren't there.
- **Agents in other Rex hosts** aren't listed yet: only the server
  rex-recap runs against.
- **The list is a terminal program in a floating pane**, not part of Rex's
  own interface. Rex has no way for a plugin to add its own.
- **An agent sitting on a startup prompt** (Claude's folder-trust question)
  reports no status, so it shows up once it's past that.
- **No notifications.** Rex has none for a program to show, so what
  herdr-recap would say in one (a broken config, say) goes to the
  watcher's log, `~/.local/state/rex-recap/recap.log`, instead.

## Development

```sh
go test ./...
```

The Rex side is tested against a snapshot taken from a real Rex server
([`testdata/rex/snapshot.json`](testdata/rex/snapshot.json)), with the `rex`
CLI stood in for. The tests that came with herdr-recap run against herdr.

To try it against a Rex server without the Mac app (Linux, say), start an
agent in a session and open the list in a block:

```sh
rex new try
rex run -s try -- claude
rex run -s try -- env RECAP_HOST=rex rex-recap picker
```

To bring over herdr-recap's changes:

```sh
git fetch upstream && git merge upstream/main
```

## License

MIT. See [LICENSE](LICENSE).
