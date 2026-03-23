package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
)

var relaysJSONFlag bool

var relaysCmd = &cobra.Command{
	Use:     "relays",
	Short:   "Manage relays",
	GroupID: "infra",
	RunE:    runRelaysList,
}

var relaysAddCmd = &cobra.Command{
	Use:   "add [url]",
	Short: "Add a relay",
	Args:  exactArgs(1),
	RunE:  runRelaysAdd,
}

var relaysRmCmd = &cobra.Command{
	Use:   "rm [url or number]",
	Short: "Remove a relay",
	Args:  exactArgs(1),
	RunE:  runRelaysRm,
}

func init() {
	relaysCmd.Flags().BoolVar(&relaysJSONFlag, "json", false, "Output as JSON with connection status and ping")
	relaysCmd.AddCommand(relaysAddCmd)
	relaysCmd.AddCommand(relaysRmCmd)
	rootCmd.AddCommand(relaysCmd)
}

type relayInfo struct {
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	PingMs    int    `json:"ping_ms,omitempty"`
}

func runRelaysList(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	if !relaysJSONFlag {
		printActiveProfile(npub)
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	if len(relays) == 0 {
		if relaysJSONFlag {
			fmt.Println("[]")
			return nil
		}
		color.Yellow("No relays configured. Run 'nostr relays add wss://...' to add one.")
		return nil
	}

	// Check connectivity and measure ping for all relays in parallel
	infos := make([]relayInfo, len(relays))
	var wg sync.WaitGroup

	for i, r := range relays {
		infos[i] = relayInfo{URL: r}
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			relay, err := nostr.RelayConnect(ctx, url)
			elapsed := time.Since(start)
			if err == nil {
				infos[idx].Reachable = true
				infos[idx].PingMs = int(elapsed.Milliseconds())
				relay.Close()
			}
		}(i, r)
	}

	wg.Wait()

	if relaysJSONFlag {
		data, err := json.MarshalIndent(infos, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	bold := color.New(color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	dim := color.New(color.Faint)

	for i, info := range infos {
		status := red("✗")
		ping := ""
		if info.Reachable {
			status = green("✓")
			ping = dim.Sprintf("  %dms", info.PingMs)
		}
		fmt.Printf("%s %s %s%s\n", bold(fmt.Sprintf("%d.", i+1)), status, info.URL, ping)
	}
	fmt.Println()
	dim.Println("To edit this list of relays, use:")
	dim.Println("  nostr relays add <wss://...>    Add a relay")
	dim.Println("  nostr relays rm <number>        Remove a relay")
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
			color.Yellow("Relay %s is already in the list.", url)
			return nil
		}
	}

	relays = append(relays, url)
	if err := config.SaveRelays(npub, relays); err != nil {
		return fmt.Errorf("failed to save relays: %w", err)
	}

	color.Green("✓ Added %s", url)
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
		color.Yellow("Warning: removing the last relay. You won't be able to publish or fetch.")
	}

	if err := config.SaveRelays(npub, relays); err != nil {
		return fmt.Errorf("failed to save relays: %w", err)
	}

	color.Green("✓ Removed %s", removed)
	return nil
}
