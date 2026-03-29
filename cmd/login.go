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
	"golang.org/x/term"

	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var (
	loginNsec     string
	loginGenerate bool
	loginNew      bool
)

var loginCmd = &cobra.Command{
	Use:     "login",
	Short:   "Create a new account or import an existing one",
	Long: `Login with an existing nsec or generate a new keypair.

Creates an account directory in ~/.nostr/accounts/<npub>/ with keys, relays,
and optionally profile metadata.

Flags:
  --nsec <key>   Import an existing nsec (non-interactive)
  --new          Generate a new keypair (skip prompt)
  --generate     Alias for --new

Examples:
  nostr login                     # Interactive: import or generate
  nostr login --new               # Generate new keypair
  nostr login --nsec nsec1...     # Import existing key`,
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

	// Check if account already exists
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		fmt.Printf("Account %s already exists. Switching to it.\n", npub)
		return config.SetActiveProfile(npub)
	}

	// Create account directory
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

		// Set as active account
		if err := config.SetActiveProfile(npub); err != nil {
			return fmt.Errorf("failed to set active account: %w", err)
		}

		// Prompt for profile setup
		if isTTY {
			fmt.Println()
			fmt.Println("Set up your Nostr profile (enter to skip any field):")
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
				event, pErr := profile.CreateMetadataEvent(npub, meta)
				if pErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not create profile event: %v\n", pErr)
				} else {
					fmt.Println("Publishing profile to relays...")
					if _, pErr = ui.PublishEventToRelays(npub, event, relays, 0); pErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not publish profile: %v\n", pErr)
					}
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

		// Set as active account
		if err := config.SetActiveProfile(npub); err != nil {
			return fmt.Errorf("failed to set active account: %w", err)
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
	fmt.Printf("  Account dir: %s\n", dir)

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
		fmt.Printf("\nChoose an alias for this account [%s]: ", defaultAlias)
	} else {
		fmt.Print("\nChoose an alias for this account (enter to skip): ")
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

// relayChecklist shows an interactive checkbox list for relays using bubbletea.
// Returns the selected relays. All relays are checked by default.
func relayChecklist(relays []string) []string {
	loginRelayHost := func(r string) string {
		if u, err := url.Parse(r); err == nil && u.Host != "" {
			return u.Host
		}
		return r
	}

	for {
		items := make([]ui.CheckboxItem, len(relays))
		for i, r := range relays {
			items[i] = ui.CheckboxItem{
				Label:   loginRelayHost(r),
				Checked: true,
			}
		}
		// Add an "Add relay..." option at the bottom
		items = append(items, ui.CheckboxItem{
			Label:   "+ Add relay...",
			Checked: false,
		})

		result := ui.RunCheckboxPicker(ui.CheckboxPickerConfig{
			Title: "Select relays for your account:",
			Items: items,
		})

		if result.Cancelled {
			return relays // return all on cancel
		}

		// Check if "Add relay..." was selected
		addRelaySelected := false
		for _, idx := range result.Selected {
			if idx == len(relays) {
				addRelaySelected = true
				break
			}
		}

		if addRelaySelected {
			// Prompt for new relay URL
			inputResult := ui.RunInlineInput(ui.InlineInputConfig{
				Prompt: "  Relay URL: ",
				Hint:   "Enter a relay URL (wss://...)",
			})
			if !inputResult.Cancelled && inputResult.Text != "" {
				newRelay := inputResult.Text
				if strings.HasPrefix(newRelay, "wss://") || strings.HasPrefix(newRelay, "ws://") {
					relays = append(relays, newRelay)
				}
			}
			continue // re-show the picker with the new relay
		}

		// Collect selected relays (excluding the "Add relay..." option)
		var selected []string
		for _, idx := range result.Selected {
			if idx < len(relays) {
				selected = append(selected, relays[idx])
			}
		}
		if len(selected) == 0 {
			return relays // fallback: return all
		}
		return selected
	}
}

// readMaskedInput reads a secret input using bubbletea with masked display.
func readMaskedInput() (string, error) {
	result := ui.RunSecretInput(ui.SecretInputConfig{
		Prompt: "Enter your nsec (leave blank to generate a new keypair): ",
	})
	if result.Cancelled {
		return "", fmt.Errorf("interrupted")
	}
	return result.Value, nil
}
