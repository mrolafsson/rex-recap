package main

import "os"

// Recap runs under one of two hosts: herdr (herdr.dev), which it was written
// for, or Rex (superlogical.com/rex). Each herdr call (herdr.go and the like)
// asks onRex first and goes to its Rex counterpart (rex.go) when it says so.

// defaultHost is the host when the environment doesn't say: this build is
// rex-recap, so Rex.
const defaultHost = "rex"

// hostEnv picks the host by hand: "rex" or "herdr".
const hostEnv = "RECAP_HOST"

// onRex says whether Rex is the host: as RECAP_HOST says, else herdr when
// herdr started this as its plugin, else Rex when this runs in a Rex pane,
// else herdr when in a herdr pane, else defaultHost. A Rex pane can carry
// herdr's pane variables (a Rex server started from a herdr pane passes them
// on), so they count for less than Rex's. A variable so tests can pick.
var onRex = func() bool {
	switch os.Getenv(hostEnv) {
	case "rex":
		return true
	case "herdr":
		return false
	}
	if os.Getenv("HERDR_PLUGIN_ID") != "" {
		return false
	}
	if os.Getenv("REX_SESSION") != "" || os.Getenv("REX_BLOCK") != "" {
		return true
	}
	if os.Getenv("HERDR_SOCKET_PATH") != "" {
		return false
	}
	return defaultHost == "rex"
}
