package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/profile"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"golang.org/x/term"
)

var switchCmd = &cobra.Command{
	Use:     "switch [profile]",
	Short:   "Switch active profile",
	Long:    "Switch to a different profile. Without arguments, select interactively.\nA <profile> can be an npub, alias, or NIP-05 address.",
	GroupID: "profile",
	RunE:    runSwitch,
}

func init() {
	rootCmd.AddCommand(switchCmd)
}

type profileEntry struct {
	npub      string
	name      string
	alias     string
	relayInfo string
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
	showSwitchedProfile(target.npub)
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
	showSwitchedProfile(targetNpub)
	return nil
}

// showSwitchedProfile displays profile details and relays after switching.
func showSwitchedProfile(npub string) {
	label := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint)

	// Try to fetch fresh profile from relays, fall back to cache
	relays, _ := config.LoadRelays(npub)
	var meta *profile.Metadata

	if len(relays) > 0 {
		ctx := context.Background()
		fresh, err := profile.FetchFromRelays(ctx, npub, relays)
		if err == nil && fresh != nil {
			meta = fresh
			_ = profile.SaveCached(npub, meta)
		}
	}
	if meta == nil {
		meta, _ = profile.LoadCached(npub)
	}

	fmt.Println()
	pubHex, _ := crypto.NpubToHex(npub)
	fmt.Printf("%s %s\n", label("npub:"), npub)
	if meta != nil {
		printColorField(label, "Name", meta.Name)
		printColorField(label, "Display Name", meta.DisplayName)
		printColorField(label, "About", meta.About)
		printColorField(label, "Picture", meta.Picture)
		printNIP05Field(label, meta.NIP05, pubHex)
		printColorField(label, "Banner", meta.Banner)
		printColorField(label, "Website", meta.Website)
		printColorField(label, "Lightning", meta.LUD16)
	}

	if len(relays) > 0 {
		fmt.Printf("%-14s\n", label("Relays:"))
		for _, r := range relays {
			if u, err := url.Parse(r); err == nil && u.Host != "" {
				dim.Printf("  %s\n", u.Host)
			} else {
				dim.Printf("  %s\n", r)
			}
		}
	}
}

func listSwitchableProfiles() ([]profileEntry, error) {
	base, err := config.BaseDir()
	if err != nil {
		return nil, err
	}

	// Build reverse alias map: npub -> alias
	aliasMap := make(map[string]string)
	if aliases, err := config.LoadGlobalAliases(); err == nil {
		for name, npub := range aliases {
			aliasMap[npub] = name
		}
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
		entries = append(entries, profileEntry{
			npub:      npub,
			name:      profileName(meta),
			alias:     aliasMap[npub],
			relayInfo: relayInfoStr(npub),
		})
	}
	return entries, nil
}

func relayInfoStr(npub string) string {
	relays, _ := config.LoadRelays(npub)
	if len(relays) == 0 {
		return "no relays"
	}
	if len(relays) == 1 {
		if u, err := url.Parse(relays[0]); err == nil {
			return u.Host
		}
		return relays[0]
	}
	return fmt.Sprintf("%d relays", len(relays))
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

// columnWidths computes the max width for name, alias, and npub columns.
func columnWidths(entries []profileEntry) (int, int, int) {
	var nameW, aliasW, npubW int
	for _, e := range entries {
		if len(e.alias) > aliasW {
			aliasW = len(e.alias)
		}
		if len(e.name) > nameW {
			nameW = len(e.name)
		}
		short := e.npub
		if len(short) > 20 {
			short = short[:20] + "..."
		}
		if len(short) > npubW {
			npubW = len(short)
		}
	}
	return nameW, aliasW, npubW
}

func shortNpub(npub string) string {
	return npub
}

func formatEntry(e profileEntry, nameW, aliasW, npubW int, cyanFn, dimFn func(a ...interface{}) string, highlight bool) string {
	name := e.name
	alias := e.alias
	npub := shortNpub(e.npub)
	relay := e.relayInfo

	// Build columns with padding: name, alias, npub, relays
	nameCol := fmt.Sprintf("%-*s", nameW, name)
	aliasCol := fmt.Sprintf("%-*s", aliasW, alias)
	npubCol := fmt.Sprintf("%-*s", npubW, npub)

	if highlight {
		parts := []string{}
		if nameW > 0 {
			parts = append(parts, nameCol)
		}
		if aliasW > 0 {
			if alias != "" {
				parts = append(parts, cyanFn(aliasCol))
			} else {
				parts = append(parts, aliasCol)
			}
		}
		parts = append(parts, dimFn(npubCol))
		if relay != "" {
			parts = append(parts, dimFn(relay))
		}
		return strings.Join(parts, "  ")
	}

	// Non-selected: dim name and npub, bright alias
	parts := []string{}
	if nameW > 0 {
		parts = append(parts, dimFn(nameCol))
	}
	if aliasW > 0 {
		if alias != "" {
			parts = append(parts, cyanFn(aliasCol))
		} else {
			parts = append(parts, dimFn(aliasCol))
		}
	}
	parts = append(parts, dimFn(npubCol))
	if relay != "" {
		parts = append(parts, dimFn(relay))
	}
	return strings.Join(parts, "  ")
}

func interactiveSelect(entries []profileEntry, selected int, footer ...string) (int, error) {
	footerText := "Run 'nostr login' to add a new profile."
	if len(footer) > 0 {
		footerText = footer[0]
	}
	nameW, aliasW, npubW := columnWidths(entries)

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Not a terminal — fall back to listing
		cyan := color.New(color.FgCyan).SprintFunc()
		bold := color.New(color.Bold).SprintFunc()
		dim := color.New(color.Faint).SprintFunc()
		activeNpub, _ := config.ActiveProfile()
		fmt.Println("Available profiles:")
		for i, e := range entries {
			active := ""
			if e.npub == activeNpub {
				active = " " + bold("(active)")
			}
			line := formatEntry(e, nameW, aliasW, npubW, cyan, dim, true)
			fmt.Printf("  %d. %s%s\n", i+1, line, active)
		}
		if footerText != "" {
			fmt.Println()
			fmt.Println(footerText)
		}
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
			line := formatEntry(e, nameW, aliasW, npubW, cyan, dim, i == selected)
			if i == selected {
				fmt.Printf("  > %s\r\n", line)
			} else {
				fmt.Printf("    %s\r\n", line)
			}
		}
		if footerText != "" {
			fmt.Printf("\r\n  %s\r\n", dim(footerText))
		}
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
		lines := len(entries) + 2 // header + entries
		if footerText != "" {
			lines += 2 // blank line + footer
		}
		fmt.Printf("\033[%dA", lines)
		render()
	}
}
