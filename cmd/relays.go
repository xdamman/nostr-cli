package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var (
	relaysJSONFlag  bool
	relaysRelayFlag string
)

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
	relaysCmd.Flags().StringVar(&relaysRelayFlag, "relay", "", "Show a specific relay only (full URL or domain)")
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

	// Filter to a specific relay if --relay is specified
	if relaysRelayFlag != "" {
		var matched []string
		for _, r := range relays {
			if r == relaysRelayFlag || relayHost(r) == relaysRelayFlag {
				matched = append(matched, r)
				break
			}
		}
		if len(matched) == 0 {
			return fmt.Errorf("relay %s is not in your configured relays", relaysRelayFlag)
		}
		relays = matched
	}

	if relaysJSONFlag {
		return runRelaysListJSON(relays)
	}

	bold := color.New(color.Bold).SprintFunc()
	greenFn := color.New(color.FgGreen).SprintFunc()
	redFn := color.New(color.FgRed).SprintFunc()
	dim := color.New(color.Faint)

	// Display host for each relay
	type relayLine struct {
		host string
		url  string
	}
	rl := make([]relayLine, len(relays))
	for i, r := range relays {
		host := r
		if u, uErr := url.Parse(r); uErr == nil && u.Host != "" {
			host = u.Host
		}
		rl[i] = relayLine{host: host, url: r}
		fmt.Printf("%s %s %s\n", bold(fmt.Sprintf("%d.", i+1)), dim.Sprint(ui.SpinnerFrames[0]), dim.Sprint(host))
	}

	// Show hints immediately
	fmt.Println()
	dim.Println("To edit this list of relays, use:")
	dim.Println("  nostr relays add <wss://...>    Add a relay")
	dim.Println("  nostr relays rm <number>        Remove a relay")

	// Total lines to jump back: relay lines + blank + 3 hint lines
	totalLines := len(rl) + 4

	// Check connectivity in parallel, streaming results
	type pingResult struct {
		idx    int
		ok     bool
		pingMs int
	}
	ch := make(chan pingResult, len(relays))
	for i, r := range relays {
		go func(idx int, relayURL string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			relay, err := nostr.RelayConnect(ctx, relayURL)
			elapsed := time.Since(start)
			if err == nil {
				relay.Close()
				ch <- pingResult{idx: idx, ok: true, pingMs: int(elapsed.Milliseconds())}
			} else {
				ch <- pingResult{idx: idx, ok: false}
			}
		}(i, r)
	}

	results := make(map[int]pingResult)

	renderRelays := func(frame int) {
		fmt.Printf("\033[%dA", totalLines)
		for i, l := range rl {
			fmt.Print("\r\033[K")
			if res, ok := results[i]; ok {
				if res.ok {
					fmt.Printf("%s %s %s  %s\n", bold(fmt.Sprintf("%d.", i+1)), greenFn("✓"), l.host, dim.Sprintf("%dms", res.pingMs))
				} else {
					fmt.Printf("%s %s %s\n", bold(fmt.Sprintf("%d.", i+1)), redFn("✗"), l.host)
				}
			} else {
				f := ui.SpinnerFrames[frame%len(ui.SpinnerFrames)]
				fmt.Printf("%s %s %s\n", bold(fmt.Sprintf("%d.", i+1)), dim.Sprint(f), dim.Sprint(l.host))
			}
		}
		// Move cursor back down past the hints
		fmt.Printf("\033[%dB", 4)
	}

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	done := false

	for !done {
		select {
		case res := <-ch:
			results[res.idx] = res
			renderRelays(frame)
			if len(results) == len(rl) {
				done = true
			}
		case <-ticker.C:
			if len(results) < len(rl) {
				frame++
				renderRelays(frame)
			}
		}
	}
	renderRelays(frame)

	return nil
}

func runRelaysListJSON(relays []string) error {
	infos := make([]relayInfo, len(relays))
	var wg sync.WaitGroup

	for i, r := range relays {
		infos[i] = relayInfo{URL: r}
		wg.Add(1)
		go func(idx int, relayURL string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			relay, err := nostr.RelayConnect(ctx, relayURL)
			elapsed := time.Since(start)
			if err == nil {
				infos[idx].Reachable = true
				infos[idx].PingMs = int(elapsed.Milliseconds())
				relay.Close()
			}
		}(i, r)
	}

	wg.Wait()

	data, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
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
