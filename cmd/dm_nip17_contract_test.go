package cmd

import "testing"

func TestDM_Nip04FlagExists(t *testing.T) {
	cmd := requireCmd(t, "dm")
	requireFlag(t, cmd, "nip04")
}

func TestDM_CommandExists(t *testing.T) {
	requireCmd(t, "dm")
}
