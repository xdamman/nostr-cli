package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
)

var relaysCmd = &cobra.Command{
	Use:   "relays",
	Short: "List configured relays",
	RunE:  runRelaysList,
}

var relaysAddCmd = &cobra.Command{
	Use:   "add [url]",
	Short: "Add a relay",
	Args:  cobra.ExactArgs(1),
	RunE:  runRelaysAdd,
}

var relaysRmCmd = &cobra.Command{
	Use:   "rm [url or number]",
	Short: "Remove a relay",
	Args:  cobra.ExactArgs(1),
	RunE:  runRelaysRm,
}

func init() {
	relaysCmd.AddCommand(relaysAddCmd)
	relaysCmd.AddCommand(relaysRmCmd)
	rootCmd.AddCommand(relaysCmd)
}

func runRelaysList(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	if len(relays) == 0 {
		fmt.Println("No relays configured. Run 'nostr-cli relays add wss://...' to add one.")
		return nil
	}

	for i, r := range relays {
		fmt.Printf("%d. %s\n", i+1, r)
	}
	return nil
}

func runRelaysAdd(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	url := args[0]
	if !strings.HasPrefix(url, "wss://") && !strings.HasPrefix(url, "ws://") {
		return fmt.Errorf("relay URL must start with wss:// or ws://")
	}

	relays, _ := config.LoadRelays(npub)

	// Check duplicate
	for _, r := range relays {
		if r == url {
			fmt.Printf("Relay %s is already in the list.\n", url)
			return nil
		}
	}

	relays = append(relays, url)
	if err := config.SaveRelays(npub, relays); err != nil {
		return fmt.Errorf("failed to save relays: %w", err)
	}

	fmt.Printf("✓ Added %s\n", url)
	return nil
}

func runRelaysRm(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	input := args[0]
	var removeIdx int = -1

	// Try as number first
	if num, err := strconv.Atoi(input); err == nil {
		if num < 1 || num > len(relays) {
			return fmt.Errorf("invalid relay number: %d (valid: 1-%d)", num, len(relays))
		}
		removeIdx = num - 1
	} else {
		// Try as URL
		for i, r := range relays {
			if r == input {
				removeIdx = i
				break
			}
		}
	}

	if removeIdx == -1 {
		return fmt.Errorf("relay not found: %s", input)
	}

	removed := relays[removeIdx]
	relays = append(relays[:removeIdx], relays[removeIdx+1:]...)

	if len(relays) == 0 {
		fmt.Println("Warning: removing the last relay. You won't be able to publish or fetch.")
	}

	if err := config.SaveRelays(npub, relays); err != nil {
		return fmt.Errorf("failed to save relays: %w", err)
	}

	fmt.Printf("✓ Removed %s\n", removed)
	return nil
}
