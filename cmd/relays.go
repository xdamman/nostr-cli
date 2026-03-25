package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/ui"
	"golang.org/x/term"
)

var (
	relaysJSONFlag  bool
	relaysRelayFlag string
	relaysYesFlag   bool
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
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRelaysRm,
}

func init() {
	relaysCmd.Flags().BoolVar(&relaysJSONFlag, "json", false, "Output as JSON with connection status and ping")
	relaysCmd.Flags().StringVar(&relaysRelayFlag, "relay", "", "Show a specific relay only (full URL or domain)")
	relaysRmCmd.Flags().BoolVarP(&relaysYesFlag, "yes", "y", false, "Skip confirmation")
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
	publishRelayList(npub, relays)
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

	if len(relays) == 0 {
		color.Yellow("No relays configured.")
		return nil
	}

	var removeIdx int = -1

	if len(args) == 1 {
		// Direct mode: resolve by number, URL, or domain
		input := args[0]
		if num, err := strconv.Atoi(input); err == nil {
			if num < 1 || num > len(relays) {
				return fmt.Errorf("invalid relay number: %d (valid: 1-%d)", num, len(relays))
			}
			removeIdx = num - 1
		} else {
			for i, r := range relays {
				if r == input || relayHost(r) == input {
					removeIdx = i
					break
				}
			}
		}
		if removeIdx == -1 {
			return fmt.Errorf("relay not found: %s", args[0])
		}
	} else {
		// Interactive mode: checkbox list with ping
		fmt.Println("Select the relay(s) you want to remove:")
		fmt.Println()
		toRemove := interactiveRelayPick(relays)
		if len(toRemove) == 0 {
			fmt.Println("Cancelled.")
			return nil
		}

		// Warn if removing all relays
		if len(toRemove) == len(relays) {
			color.Yellow("Warning: removing all relays means you won't be able to publish or fetch.")
		}

		// Remove selected relays (iterate in reverse to keep indices valid)
		var removedNames []string
		remaining := make([]string, len(relays))
		copy(remaining, relays)
		for i := len(toRemove) - 1; i >= 0; i-- {
			idx := toRemove[i]
			removedNames = append(removedNames, relayHost(remaining[idx]))
			remaining = append(remaining[:idx], remaining[idx+1:]...)
		}

		if err := config.SaveRelays(npub, remaining); err != nil {
			return fmt.Errorf("failed to save relays: %w", err)
		}

		for _, name := range removedNames {
			color.Green("✓ Removed %s", name)
		}
		publishRelayList(npub, remaining)
		return nil
	}

	removed := relays[removeIdx]

	// Ask for confirmation unless --yes
	if !relaysYesFlag {
		if len(relays) == 1 {
			color.Yellow("Warning: this is your last relay. Removing it means you won't be able to publish or fetch.")
		}
		fmt.Printf("Remove %s? [y/N] ", removed)
		var answer string
		fmt.Scanln(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	relays = append(relays[:removeIdx], relays[removeIdx+1:]...)

	if err := config.SaveRelays(npub, relays); err != nil {
		return fmt.Errorf("failed to save relays: %w", err)
	}

	color.Green("✓ Removed %s", removed)
	publishRelayList(npub, relays)
	return nil
}

// interactiveRelayPick shows a checkbox list of relays with live ping results.
// Users toggle with space, navigate with arrows/j/k, confirm with enter.
// All checkboxes start unchecked. Returns sorted indices of selected relays, or nil if cancelled.
func interactiveRelayPick(relays []string) []int {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}
	defer term.Restore(fd, oldState)

	cyanFn := color.New(color.FgCyan).SprintFunc()
	dimSprint := color.New(color.Faint).SprintFunc()
	greenSprint := color.New(color.FgGreen).SprintFunc()
	redSprint := color.New(color.FgRed).SprintFunc()

	checked := make([]bool, len(relays))
	cursor := 0
	frame := 0

	// Ping results
	type pingResult struct {
		idx    int
		ok     bool
		pingMs int
	}
	pingResults := make(map[int]pingResult)
	pingCh := make(chan pingResult, len(relays))
	for i, r := range relays {
		go func(idx int, relayURL string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			relay, connErr := nostr.RelayConnect(ctx, relayURL)
			elapsed := time.Since(start)
			if connErr == nil {
				relay.Close()
				pingCh <- pingResult{idx: idx, ok: true, pingMs: int(elapsed.Milliseconds())}
			} else {
				pingCh <- pingResult{idx: idx, ok: false}
			}
		}(i, r)
	}

	pingDone := false

	render := func() {
		fmt.Print("\r\033[J")
		for i, r := range relays {
			host := relayHost(r)
			var check, status string

			if pr, ok := pingResults[i]; ok {
				if checked[i] {
					check = greenSprint("[✓]")
				} else {
					check = dimSprint("[ ]")
				}
				if pr.ok {
					status = dimSprint(fmt.Sprintf("  %dms", pr.pingMs))
				} else {
					status = redSprint("  unreachable")
				}
			} else {
				if checked[i] {
					check = greenSprint("[✓]")
				} else {
					f := ui.SpinnerFrames[frame%len(ui.SpinnerFrames)]
					check = dimSprint(fmt.Sprintf("[%s]", f))
				}
			}

			var line string
			if i == cursor {
				line = fmt.Sprintf("  %s %s%s", check, cyanFn(host), status)
			} else {
				line = fmt.Sprintf("  %s %s%s", check, dimSprint(host), status)
			}
			fmt.Printf("%s\r\n", line)
		}

		selectedCount := 0
		for _, c := range checked {
			if c {
				selectedCount++
			}
		}
		var hint string
		if selectedCount == 0 {
			hint = "  ↑/↓ navigate, space toggle, ctrl+c to cancel"
		} else if selectedCount == 1 {
			hint = fmt.Sprintf("  ↑/↓ navigate, space toggle, enter to remove %d relay, ctrl+c to cancel", selectedCount)
		} else {
			hint = fmt.Sprintf("  ↑/↓ navigate, space toggle, enter to remove %d relays, ctrl+c to cancel", selectedCount)
		}
		fmt.Printf("\r\n%s\r\n", dimSprint(hint))
	}

	lines := len(relays) + 2

	rerender := func() {
		fmt.Printf("\033[%dA", lines)
		render()
	}

	render()

	// Input reader goroutine
	inputCh := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := os.Stdin.Read(buf); err != nil {
				close(inputCh)
				return
			}
			inputCh <- buf[0]
		}
	}()

	readNext := func() (byte, bool) {
		select {
		case b, ok := <-inputCh:
			return b, ok
		case <-time.After(20 * time.Millisecond):
			return 0, false
		}
	}

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case res, ok := <-pingCh:
			if !ok {
				pingDone = true
				pingCh = nil
			} else {
				pingResults[res.idx] = res
				if len(pingResults) == len(relays) {
					pingDone = true
				}
			}
			rerender()

		case b, ok := <-inputCh:
			if !ok {
				fmt.Print("\r\033[J")
				return nil
			}
			switch b {
			case 13: // enter
				fmt.Print("\r\033[J")
				var selected []int
				for i := range relays {
					if checked[i] {
						selected = append(selected, i)
					}
				}
				if len(selected) == 0 {
					return nil
				}
				return selected
			case ' ':
				checked[cursor] = !checked[cursor]
				rerender()
			case 3: // ctrl-c
				fmt.Print("\r\033[J")
				return nil
			case 27: // ESC — arrow key sequence
				b2, ok := readNext()
				if !ok || b2 != '[' {
					break
				}
				b3, ok := readNext()
				if !ok {
					break
				}
				switch b3 {
				case 'A': // up
					if cursor > 0 {
						cursor--
					}
				case 'B': // down
					if cursor < len(relays)-1 {
						cursor++
					}
				}
				rerender()
			case 'k':
				if cursor > 0 {
					cursor--
				}
				rerender()
			case 'j':
				if cursor < len(relays)-1 {
					cursor++
				}
				rerender()
			}

		case <-ticker.C:
			if !pingDone {
				frame++
				rerender()
			}
		}
	}
}

// publishRelayList publishes a NIP-65 (kind 10002) relay list metadata event.
func publishRelayList(npub string, relays []string) {
	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return
	}
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return
	}

	var tags nostr.Tags
	for _, r := range relays {
		tags = append(tags, nostr.Tag{"r", r})
	}

	event := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Now(),
		Kind:      10002,
		Tags:      tags,
	}
	if err := event.Sign(skHex); err != nil {
		return
	}

	fmt.Println()
	fmt.Println("Publishing updated relay list (NIP-65)...")
	timeout := time.Duration(timeoutFlag) * time.Millisecond
	ui.PublishEventToRelays(npub, event, relays, timeout)
}
