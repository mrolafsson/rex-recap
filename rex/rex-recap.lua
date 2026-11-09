-- rex-recap for Rex: your coding agents in a floating pane, with a recap of
-- where each one got to, written while you were away.
--
-- The rex-recap binary does the work: it lists the agents, writes the
-- recaps, and draws the list. This module adds the actions and opens its
-- pane.
--
-- In ~/.config/rex/init.lua:
--
--   require("rex-recap").setup{ key = "cmd+shift+r" }
--
-- setup options:
--   bin     the rex-recap binary (default "rex-recap", found on your login
--           shell's PATH)
--   key     a key for Recap: Toggle (default none: use the command palette)
--   bounds  { x, y, w, h } of the floating pane, as fractions of the window
--           (default { 0.1, 0.08, 0.8, 0.8 })

local M = {}

M.options = {
  bin = "rex-recap",
  key = nil,
  bounds = { 0.1, 0.08, 0.8, 0.8 },
}

local SHELL = "com.superlogical.terminal.shell"
-- The toggle finds the popup's pane by this label.
local LABEL = "rex-recap"

local function fail(message)
  error("rex-recap: " .. message, 0)
end

local function check(result, err)
  if result == nil then fail(tostring(err)) end
  return result
end

-- The popup's block in the session, if it's open.
local function locate(ctx)
  local list = check(rex.session.list_blocks{ session_id = ctx.session_id })
  for _, b in ipairs(list.blocks or {}) do
    if b.label == LABEL then return b end
  end
end

-- The popup is a terminal running the binary. RECAP_HOST says it's under
-- Rex, whatever else its environment carries.
local function command(args)
  local cmd = { "env", "RECAP_HOST=rex", M.options.bin, "picker" }
  if args and args.demo then cmd[#cmd + 1] = "--demo" end
  return cmd
end

function M.open(ctx, args)
  local result = check(rex.session.new_layer{
    session_id = ctx.session_id,
    bounds = M.options.bounds,
    focus = true,
    layout = rex.layout.block{
      flavor = SHELL,
      label = LABEL,
      options = { command = command(args), cwd = os.getenv("HOME") },
    },
  })
  return { block_id = result.block_ids and result.block_ids[1] }
end

function M.close(ctx)
  local popup = locate(ctx)
  if not popup then return { closed = false } end
  check(rex.call("block.close", { session_id = ctx.session_id, block_id = popup.block_id }))
  return { closed = true, block_id = popup.block_id }
end

-- Open the popup, or close it when it's open.
function M.toggle(ctx, args)
  if locate(ctx) then return M.close(ctx) end
  return M.open(ctx, args)
end

function M.setup(opts)
  for k, v in pairs(opts or {}) do
    if M.options[k] == nil and k ~= "key" then
      rex.log("warn", "rex-recap: ignoring unknown option " .. tostring(k))
    else
      M.options[k] = v
    end
  end

  local function action(name, title, description, run)
    rex.action{
      name = name, title = title, description = description, category = "Recap",
      keywords = "recap agents claude codex status summary", run = run,
    }
  end

  action("recap_toggle", "Recap: Toggle",
    "Your coding agents and where each one got to: open the list, or close it.",
    function(ctx) return M.toggle(ctx) end)
  action("recap_demo", "Recap: Demo",
    "The list on made-up agents: try it, or take screenshots without showing real work.",
    function(ctx) return M.toggle(ctx, { demo = true }) end)

  if M.options.key then
    rex.bind(M.options.key, "recap_toggle")
  end
  return M
end

return M
