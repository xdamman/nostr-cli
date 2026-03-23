package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"golang.org/x/term"
)

var (
	loginNsec     string
	loginGenerate bool
	loginNew      bool
)

var loginCmd = &cobra.Command{
	Use:     "login",
	Short:   "Create a new profile or import an existing one",
	Long:    "Login with an existing nsec or generate a new keypair. Creates a profile in ~/.nostr/profiles/<npub>/.",
	GroupID: "profile",
	RunE:    runLogin,
}

func init() {
	loginCmd.Flags().StringVar(&loginNsec, "nsec", "", "Import an existing nsec (non-interactive)")
	loginCmd.Flags().BoolVar(&loginGenerate, "generate", false, "Generate a new keypair (skip prompt)")
	loginCmd.Flags().BoolVar(&loginNew, "new", false, "Generate a new keypair (alias for --generate)")
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint)

	var nsec, npub string
	var err error
	interactive := false
	isNewKeypair := false

	switch {
	case loginNsec != "":
		nsec = loginNsec
		npub, _, _, err = crypto.NsecToKeys(nsec)
		if err != nil {
			return fmt.Errorf("invalid nsec: %w", err)
		}
	case loginGenerate || loginNew:
		nsec, npub, _, err = crypto.GenerateKeyPair()
		if err != nil {
			return err
		}
		isNewKeypair = true
		green.Println("Generated new keypair")
	default:
		interactive = true
		fmt.Print("Enter your nsec (leave blank to generate a new keypair): ")
		input, _ := readMaskedInput()
		fmt.Println()
		if input == "" {
			nsec, npub, _, err = crypto.GenerateKeyPair()
			if err != nil {
				return err
			}
			isNewKeypair = true
			green.Println("Generated new keypair")
		} else {
			nsec = input
			npub, _, _, err = crypto.NsecToKeys(nsec)
			if err != nil {
				return fmt.Errorf("invalid nsec: %w", err)
			}
		}
	}

	// Check if profile already exists
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		fmt.Printf("Profile %s already exists. Switching to it.\n", npub)
		return config.SetActiveProfile(npub)
	}

	// Create profile directory
	if _, err := config.EnsureProfileDir(npub); err != nil {
		return err
	}

	// Save nsec
	if err := config.SaveNsec(npub, nsec); err != nil {
		return fmt.Errorf("failed to save nsec: %w", err)
	}

	isTTY := term.IsTerminal(int(os.Stdin.Fd()))

	if isNewKeypair {
		// --- New keypair flow ---
		// Show relay checklist
		relays := config.DefaultRelays()
		if isTTY {
			fmt.Println()
			fmt.Println("Default relays:")
			relays = relayChecklist(relays)
		}

		if err := config.SaveRelays(npub, relays); err != nil {
			return fmt.Errorf("failed to save relays: %w", err)
		}

		// Set as active profile
		if err := config.SetActiveProfile(npub); err != nil {
			return fmt.Errorf("failed to set active profile: %w", err)
		}

		// Prompt for profile setup
		if isTTY {
			fmt.Println()
			fmt.Println("Set up your profile (enter to skip any field):")
			reader := bufio.NewReader(os.Stdin)
			meta := &profile.Metadata{}
			meta.Name = promptField(reader, "Username", "")
			meta.DisplayName = promptField(reader, "Display name", "")
			meta.About = promptField(reader, "About", "")
			meta.Picture = promptField(reader, "Picture URL", "")
			meta.Website = promptField(reader, "Website", "")

			if err := profile.SaveCached(npub, meta); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not save profile: %v\n", err)
			}

			// Publish profile if any field was set
			if meta.Name != "" || meta.DisplayName != "" || meta.About != "" {
				fmt.Println("Publishing profile to relays...")
				ctx := context.Background()
				if err := profile.PublishMetadata(ctx, npub, meta, relays); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not publish profile: %v\n", err)
				} else {
					green.Println("✓ Profile published")
				}
			}

			// Alias prompt — default to Name
			promptAlias(npub, meta.Name, green)
		}
	} else {
		// --- Import existing nsec flow ---
		// Save default relays first so we can fetch
		if err := config.SaveDefaultRelays(npub); err != nil {
			return fmt.Errorf("failed to save default relays: %w", err)
		}

		// Set as active profile
		if err := config.SetActiveProfile(npub); err != nil {
			return fmt.Errorf("failed to set active profile: %w", err)
		}

		// Fetch profile from relays
		defaultRelays := config.DefaultRelays()
		fmt.Println("Fetching profile from relays...")
		ctx := context.Background()
		meta, fetchErr := profile.FetchFromRelays(ctx, npub, defaultRelays)
		if fetchErr == nil && meta != nil {
			if err := profile.SaveCached(npub, meta); err == nil {
				if meta.Name != "" {
					fmt.Printf("%s %s\n", cyan("Found profile:"), meta.Name)
				}
				if meta.DisplayName != "" {
					fmt.Printf("%s %s\n", cyan("Display name:"), meta.DisplayName)
				}
			}
		}

		// Fetch relay list (NIP-65 kind 10002) from default relays
		pubHex, _ := crypto.NpubToHex(npub)
		fetchedRelays := fetchRelayList(ctx, pubHex, defaultRelays)

		// Merge fetched relays with defaults (fetched first, then defaults not already in list)
		var allRelays []string
		seen := make(map[string]bool)
		for _, r := range fetchedRelays {
			if !seen[r] {
				seen[r] = true
				allRelays = append(allRelays, r)
			}
		}
		for _, r := range defaultRelays {
			if !seen[r] {
				seen[r] = true
				allRelays = append(allRelays, r)
			}
		}

		if isTTY && len(allRelays) > 0 {
			fmt.Println()
			if len(fetchedRelays) > 0 {
				fmt.Printf("Found %d relays from your profile. Confirm your relay list:\n", len(fetchedRelays))
			} else {
				fmt.Println("Default relays:")
			}
			allRelays = relayChecklist(allRelays)
		}

		if err := config.SaveRelays(npub, allRelays); err != nil {
			return fmt.Errorf("failed to save relays: %w", err)
		}

		// Alias prompt — default to Name
		if interactive && isTTY {
			name := ""
			if meta != nil {
				name = meta.Name
			}
			promptAlias(npub, name, green)
		}
	}

	fmt.Println()
	green.Printf("✓ Logged in as %s\n", npub)
	fmt.Printf("  Profile dir: %s\n", dir)

	// Next steps hints
	fmt.Println()
	dim.Println("Next steps:")
	dim.Println("  Post a public note:     nostr post \"Hello Nostr!\"")
	dim.Println("  Send a direct message:  nostr dm <profile> \"message\"")
	dim.Println("  Enter interactive mode:  nostr")
	dim.Println("  Manage relays:          nostr relays add <wss://...>")

	return nil
}

// promptAlias prompts for an alias with the given default name.
func promptAlias(npub, defaultName string, green *color.Color) {
	defaultAlias := ""
	if defaultName != "" {
		defaultAlias = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(defaultName), " ", "-"))
	}
	if defaultAlias != "" {
		fmt.Printf("\nChoose an alias for this profile [%s]: ", defaultAlias)
	} else {
		fmt.Print("\nChoose an alias for this profile (enter to skip): ")
	}
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		alias := strings.TrimSpace(scanner.Text())
		if alias == "" && defaultAlias != "" {
			alias = defaultAlias
		}
		if alias != "" {
			if err := config.SetGlobalAlias(alias, npub); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not set alias: %v\n", err)
			} else {
				green.Printf("✓ Alias %s → %s\n", alias, npub)
			}
		}
	}
}

// fetchRelayList fetches NIP-65 (kind 10002) relay list for a pubkey.
func fetchRelayList(ctx context.Context, pubHex string, relayURLs []string) []string {
	filter := nostr.Filter{
		Authors: []string{pubHex},
		Kinds:   []int{10002},
		Limit:   1,
	}

	event, err := internalRelay.FetchEvent(ctx, filter, relayURLs)
	if err != nil || event == nil {
		return nil
	}

	var relays []string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "r" {
			relays = append(relays, tag[1])
		}
	}
	return relays
}

// relayChecklist shows an interactive checkbox list for relays.
// Returns the selected relays. Allows toggling with space, adding with 'a', continuing with enter.
func relayChecklist(relays []string) []string {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Non-interactive: return all
		return relays
	}

	checked := make([]bool, len(relays))
	for i := range checked {
		checked[i] = true // all on by default
	}
	cursor := 0

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return relays
	}
	defer term.Restore(fd, oldState)

	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	relayHost := func(r string) string {
		if u, err := url.Parse(r); err == nil && u.Host != "" {
			return u.Host
		}
		return r
	}

	render := func() {
		fmt.Print("\r\033[J")
		for i, r := range relays {
			check := green("[✓]")
			if !checked[i] {
				check = dim("[ ]")
			}
			label := relayHost(r)
			if i == cursor {
				fmt.Printf("  %s %s\r\n", check, cyan(label))
			} else {
				fmt.Printf("  %s %s\r\n", check, dim(label))
			}
		}
		fmt.Printf("\r\n  %s\r\n", dim("space: toggle, a: add relay, enter: continue"))
	}

	render()

	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			break
		}

		switch b[0] {
		case 13: // enter — continue
			fmt.Print("\r\033[J")
			var selected []string
			for i, r := range relays {
				if checked[i] {
					selected = append(selected, r)
				}
			}
			return selected
		case ' ': // space — toggle
			checked[cursor] = !checked[cursor]
		case 'a': // add relay
			fmt.Print("\r\033[J")
			term.Restore(fd, oldState)
			fmt.Print("  Relay URL (wss://...): ")
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				newRelay := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(newRelay, "wss://") || strings.HasPrefix(newRelay, "ws://") {
					relays = append(relays, newRelay)
					checked = append(checked, true)
				}
			}
			oldState, _ = term.MakeRaw(fd)
		case 3: // ctrl-c
			fmt.Print("\r\033[J")
			var selected []string
			for i, r := range relays {
				if checked[i] {
					selected = append(selected, r)
				}
			}
			return selected
		case 27: // ESC / arrow keys
			seq := make([]byte, 2)
			n, _ := os.Stdin.Read(seq)
			if n >= 2 && seq[0] == '[' {
				switch seq[1] {
				case 'A': // up
					if cursor > 0 {
						cursor--
					}
				case 'B': // down
					if cursor < len(relays)-1 {
						cursor++
					}
				}
			} else if n == 1 && seq[0] == '[' {
				if _, err := os.Stdin.Read(seq[:1]); err == nil {
					switch seq[0] {
					case 'A':
						if cursor > 0 {
							cursor--
						}
					case 'B':
						if cursor < len(relays)-1 {
							cursor++
						}
					}
				}
			}
		case 'k': // vim up
			if cursor > 0 {
				cursor--
			}
		case 'j': // vim down
			if cursor < len(relays)-1 {
				cursor++
			}
		}

		// Re-render
		lines := len(relays) + 2
		fmt.Printf("\033[%dA", lines)
		render()
	}

	return relays
}

// readMaskedInput reads input character by character in raw mode,
// displaying • for all characters except the last 4.
func readMaskedInput() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Fallback: read without masking
		var line string
		fmt.Scanln(&line)
		return strings.TrimSpace(line), nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		var line string
		fmt.Scanln(&line)
		return strings.TrimSpace(line), nil
	}
	defer term.Restore(fd, oldState)

	var buf []byte
	b := make([]byte, 1)

	for {
		_, err := os.Stdin.Read(b)
		if err != nil {
			return string(buf), err
		}

		switch b[0] {
		case 13, 10: // Enter
			return string(buf), nil
		case 3: // Ctrl-C
			return "", fmt.Errorf("interrupted")
		case 127, 8: // Backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				redrawMasked(buf)
			}
		case 22: // Ctrl-V (paste) — just continue reading chars
			continue
		default:
			if b[0] >= 32 {
				buf = append(buf, b[0])
				redrawMasked(buf)
			}
		}
	}
}

// redrawMasked redraws the masked input: •••••last4
func redrawMasked(buf []byte) {
	display := maskSecret(string(buf))
	// Clear the line from cursor start, reprint prompt + masked value
	fmt.Printf("\r\033[K")
	fmt.Printf("Enter your nsec (leave blank to generate a new keypair): %s", display)
}

// maskSecret returns a masked version showing only the last 4 characters.
func maskSecret(s string) string {
	if len(s) <= 4 {
		return s
	}
	masked := strings.Repeat("•", len(s)-4)
	return masked + s[len(s)-4:]
}
