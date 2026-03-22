package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"golang.org/x/term"
)

var switchCmd = &cobra.Command{
	Use:   "switch [npub|alias]",
	Short: "Switch active profile",
	Long:  "Switch to a different profile. Without arguments, select interactively.",
	RunE:  runSwitch,
}

func init() {
	rootCmd.AddCommand(switchCmd)
}

type profileEntry struct {
	npub string
	name string
}

func runSwitch(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	activeNpub, _ := config.ActiveProfile()

	if len(args) > 0 {
		return switchToTarget(args[0], activeNpub, green)
	}

	// Interactive selection
	entries, err := listSwitchableProfiles()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No profiles found. Run 'nostr login' to create one.")
		return nil
	}

	// Find the index of the active profile
	selected := 0
	for i, e := range entries {
		if e.npub == activeNpub {
			selected = i
			break
		}
	}

	chosen, err := interactiveSelect(entries, selected)
	if err != nil {
		return err
	}
	if chosen < 0 {
		return nil // user cancelled
	}

	target := entries[chosen]
	if target.npub == activeNpub {
		fmt.Println("Already on this profile.")
		return nil
	}

	if err := config.SetActiveProfile(target.npub); err != nil {
		return err
	}

	if target.name != "" {
		green.Printf("Switched to %s (%s)\n", target.name, target.npub)
	} else {
		green.Printf("Switched to %s\n", target.npub)
	}
	return nil
}

func switchToTarget(arg string, activeNpub string, green *color.Color) error {
	targetNpub := arg
	// Try alias/npub resolution (works even without active profile)
	resolved, err := resolve.ResolveToNpub(activeNpub, arg)
	if err == nil {
		targetNpub = resolved
	}

	dir, err := config.ProfileDir(targetNpub)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("profile %s not found. Run 'nostr login' first", targetNpub)
	}

	if err := config.SetActiveProfile(targetNpub); err != nil {
		return err
	}

	meta, _ := profile.LoadCached(targetNpub)
	name := profileName(meta)
	if name != "" {
		green.Printf("Switched to %s (%s)\n", name, targetNpub)
	} else {
		green.Printf("Switched to %s\n", targetNpub)
	}
	return nil
}

func listSwitchableProfiles() ([]profileEntry, error) {
	base, err := config.BaseDir()
	if err != nil {
		return nil, err
	}

	profilesDir := filepath.Join(base, "profiles")
	dirEntries, err := os.ReadDir(profilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []profileEntry
	for _, de := range dirEntries {
		// Skip symlinks (aliases) — only show real profile dirs
		if de.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !de.IsDir() {
			continue
		}
		npub := de.Name()
		if !strings.HasPrefix(npub, "npub1") {
			continue
		}
		// Only include profiles that have an nsec (owned identities)
		if !config.HasNsec(npub) {
			continue
		}
		meta, _ := profile.LoadCached(npub)
		entries = append(entries, profileEntry{npub: npub, name: profileName(meta)})
	}
	return entries, nil
}

func profileName(meta *profile.Metadata) string {
	if meta == nil {
		return ""
	}
	if meta.Name != "" {
		return meta.Name
	}
	return meta.DisplayName
}

func interactiveSelect(entries []profileEntry, selected int) (int, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Not a terminal — fall back to listing
		cyan := color.New(color.FgCyan).SprintFunc()
		bold := color.New(color.Bold).SprintFunc()
		activeNpub, _ := config.ActiveProfile()
		fmt.Println("Available profiles:")
		for i, e := range entries {
			active := ""
			if e.npub == activeNpub {
				active = " " + bold("(active)")
			}
			if e.name != "" {
				fmt.Printf("  %d. %s %s%s\n", i+1, cyan(e.name), e.npub, active)
			} else {
				fmt.Printf("  %d. %s%s\n", i+1, e.npub, active)
			}
		}
		fmt.Println()
		fmt.Println("Run 'nostr login' to add a new identity.")
		return -1, nil
	}

	// Put terminal in raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return -1, err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()

	render := func() {
		// Move cursor to start and clear
		fmt.Print("\r\033[J") // clear from cursor to end of screen
		fmt.Print("Select a profile (arrow keys to move, enter to select, q to cancel):\r\n\r\n")
		for i, e := range entries {
			label := e.npub
			if e.name != "" {
				label = fmt.Sprintf("%s (%s)", cyan(e.name), e.npub[:20]+"...")
			}
			if i == selected {
				fmt.Printf("  > %s\r\n", label)
			} else {
				fmt.Printf("    %s\r\n", dim(entryLabel(e)))
			}
		}
		fmt.Printf("\r\n  %s\r\n", dim("Run 'nostr login' to add a new identity."))
	}

	render()

	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			return -1, err
		}

		switch b[0] {
		case 13: // enter
			fmt.Print("\r\033[J")
			return selected, nil
		case 'q', 3: // q or Ctrl-C
			fmt.Print("\r\033[J")
			return -1, nil
		case 27: // ESC — could be arrow key or bare Esc
			seq := make([]byte, 2)
			n, _ := os.Stdin.Read(seq)
			if n == 2 && seq[0] == '[' {
				switch seq[1] {
				case 'A':
					if selected > 0 {
						selected--
					}
				case 'B':
					if selected < len(entries)-1 {
						selected++
					}
				}
			} else if n == 1 && seq[0] == '[' {
				if _, err := os.Stdin.Read(seq[:1]); err == nil {
					switch seq[0] {
					case 'A':
						if selected > 0 {
							selected--
						}
					case 'B':
						if selected < len(entries)-1 {
							selected++
						}
					}
				}
			} else {
				fmt.Print("\r\033[J")
				return -1, nil
			}
		case 'k': // vim up
			if selected > 0 {
				selected--
			}
		case 'j': // vim down
			if selected < len(entries)-1 {
				selected++
			}
		}

		// Re-render: move cursor up to overwrite
		lines := len(entries) + 4 // header + entries + footer
		fmt.Printf("\033[%dA", lines)
		render()
	}
}

func entryLabel(e profileEntry) string {
	if e.name != "" {
		return fmt.Sprintf("%s (%s...)", e.name, e.npub[:20])
	}
	return e.npub
}
