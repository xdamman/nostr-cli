package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var (
	syncRelayFlag string
	syncJSONFlag  bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local events with relays",
	Long: `Sync your locally stored events with your configured relays.

Fetches all your events still present on relays and publishes any local
events that may be missing from them.

Replaceable events (profile, follow list, relay list, etc.) are handled
correctly: only the latest version is synced, as relays discard older ones.

Use --relay to sync with a specific relay only.
Use --json for machine-readable output.`,
	GroupID: "infra",
	RunE:    runSync,
}

func init() {
	syncCmd.Flags().StringVar(&syncRelayFlag, "relay", "", "Sync with a specific relay only (wss://...)")
	syncCmd.Flags().BoolVar(&syncJSONFlag, "json", false, "Output sync results as JSON")
	rootCmd.AddCommand(syncCmd)
}

// plural returns "s" if n != 1, empty string otherwise.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// relayHost extracts the host from a relay URL for display.
func relayHost(r string) string {
	if u, err := url.Parse(r); err == nil && u.Host != "" {
		return u.Host
	}
	return r
}

// --- Event kind classification per NIP-01 ---
// https://github.com/nostr-protocol/nips/blob/master/01.md

func isReplaceable(kind int) bool {
	return kind == 0 || kind == 3 || (kind >= 10000 && kind < 20000)
}

func isEphemeral(kind int) bool {
	return kind >= 20000 && kind < 30000
}

func isAddressable(kind int) bool {
	return kind >= 30000 && kind < 40000
}

func dTagValue(ev nostr.Event) string {
	for _, tag := range ev.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			return tag[1]
		}
	}
	return ""
}

// syncableEvents filters events to only those that should exist on relays.
func syncableEvents(events []nostr.Event) (syncable []nostr.Event, skipped int) {
	latestReplaceable := make(map[int]nostr.Event)
	latestAddressable := make(map[string]nostr.Event)
	var regular []nostr.Event

	for _, ev := range events {
		switch {
		case isEphemeral(ev.Kind):
			skipped++
		case isReplaceable(ev.Kind):
			if existing, ok := latestReplaceable[ev.Kind]; !ok || ev.CreatedAt > existing.CreatedAt {
				latestReplaceable[ev.Kind] = ev
			}
		case isAddressable(ev.Kind):
			key := fmt.Sprintf("%d:%s", ev.Kind, dTagValue(ev))
			if existing, ok := latestAddressable[key]; !ok || ev.CreatedAt > existing.CreatedAt {
				latestAddressable[key] = ev
			}
		default:
			regular = append(regular, ev)
		}
	}

	replaceableCount := 0
	for _, ev := range events {
		if isReplaceable(ev.Kind) {
			replaceableCount++
		}
	}
	addressableCount := 0
	for _, ev := range events {
		if isAddressable(ev.Kind) {
			addressableCount++
		}
	}
	skipped += (replaceableCount - len(latestReplaceable)) + (addressableCount - len(latestAddressable))

	syncable = regular
	for _, ev := range latestReplaceable {
		syncable = append(syncable, ev)
	}
	for _, ev := range latestAddressable {
		syncable = append(syncable, ev)
	}

	sort.Slice(syncable, func(i, j int) bool {
		return syncable[i].CreatedAt < syncable[j].CreatedAt
	})

	return syncable, skipped
}

// relayFetchState tracks what we've learned from fetching a relay.
type relayFetchState struct {
	done       bool
	ok         bool
	count      int
	eventIDs   map[string]bool
	missing    int // local events missing from relay (need to push)
	remoteOnly int // relay events missing from local (need to pull)
	duration   time.Duration
	err        error
}

// --- JSON output types ---

type syncJSONRelay struct {
	URL          string `json:"url"`
	Events       int    `json:"events"`
	Missing      int    `json:"missing"`
	Published    int    `json:"published"`
	Failed       int    `json:"failed"`
	Reachable    bool   `json:"reachable"`
	ErrorMessage string `json:"error,omitempty"`
}

type syncJSONOutput struct {
	Account        string          `json:"account"`
	LocalEvents    int             `json:"local_events"`
	SyncableEvents int             `json:"syncable_events"`
	SkippedEvents  int             `json:"skipped_events"`
	SavedLocally   int             `json:"saved_locally"`
	Relays         []syncJSONRelay `json:"relays"`
}

func runSync(cmd *cobra.Command, args []string) error {
	if syncJSONFlag {
		return runSyncJSON(cmd, args)
	}
	return runSyncInteractive(cmd, args)
}

func runSyncJSON(cmd *cobra.Command, args []string) error {
	npub, err := loadAccount()
	if err != nil {
		return err
	}
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	allRelays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	// If --relay specified, filter
	var relays []string
	if syncRelayFlag != "" {
		var matched string
		for _, r := range allRelays {
			if r == syncRelayFlag || relayHost(r) == syncRelayFlag {
				matched = r
				break
			}
		}
		if matched == "" {
			return fmt.Errorf("relay %s is not in your configured relays", syncRelayFlag)
		}
		relays = []string{matched}
	} else {
		relays = allRelays
	}

	localEvents, err := cache.LoadSentEvents(npub)
	if err != nil {
		return fmt.Errorf("failed to load local events: %w", err)
	}
	localSyncable, localSkipped := syncableEvents(localEvents)

	localIDs := make(map[string]bool, len(localEvents))
	for _, ev := range localEvents {
		localIDs[ev.ID] = true
	}

	myHex := pubHex

	// Fetch authored events from all selected relays
	timeout := time.Duration(timeoutFlag) * time.Millisecond
	authorFilter := nostr.Filter{Authors: []string{pubHex}}
	fetchCh := internalRelay.FetchEventsPerRelay(context.Background(), authorFilter, relays, timeout)

	fetchStates := make(map[string]*relayFetchState)
	for res := range fetchCh {
		st := &relayFetchState{done: true, eventIDs: make(map[string]bool), duration: res.Duration, err: res.Err}
		if res.Err == nil {
			st.ok = true
			st.count = len(res.Events)
			for _, ev := range res.Events {
				st.eventIDs[ev.ID] = true
				if !localIDs[ev.ID] {
					st.remoteOnly++
				}
			}
			for _, ev := range localSyncable {
				if !st.eventIDs[ev.ID] {
					st.missing++
				}
			}
		}
		fetchStates[res.URL] = st
	}

	// Build remote IDs, save new events locally
	remoteIDs := make(map[string]bool)
	for _, r := range relays {
		if st, ok := fetchStates[r]; ok && st.ok {
			for id := range st.eventIDs {
				remoteIDs[id] = true
			}
		}
	}

	var newRemoteIDs []string
	for id := range remoteIDs {
		if !localIDs[id] {
			newRemoteIDs = append(newRemoteIDs, id)
		}
	}

	savedLocally := 0
	if len(newRemoteIDs) > 0 {
		idFilter := nostr.Filter{IDs: newRemoteIDs, Limit: len(newRemoteIDs)}
		fetched, fErr := internalRelay.FetchEvents(context.Background(), idFilter, relays)
		if fErr == nil {
			for _, ev := range fetched {
				saveSyncedEvent(npub, myHex, *ev)
				savedLocally++
			}
		}
	}

	// Fetch incoming DMs (sent to this account)
	dmFilter := nostr.Filter{
		Kinds: []int{nostr.KindEncryptedDirectMessage},
		Tags:  nostr.TagMap{"p": []string{pubHex}},
	}
	dmFetchCh := internalRelay.FetchEventsPerRelay(context.Background(), dmFilter, relays, timeout)
	dmSaved := 0
	for res := range dmFetchCh {
		if res.Err != nil {
			continue
		}
		for _, ev := range res.Events {
			saveSyncedEvent(npub, myHex, *ev)
			dmSaved++
		}
	}

	// Find missing events and publish
	var missing []nostr.Event
	for _, ev := range localSyncable {
		if !remoteIDs[ev.ID] {
			missing = append(missing, ev)
		}
	}

	// Build per-relay publish results
	relayResults := make(map[string]*syncJSONRelay)
	for _, r := range relays {
		jr := &syncJSONRelay{URL: r}
		if st, ok := fetchStates[r]; ok && st.done {
			if st.ok {
				jr.Events = st.count
				jr.Missing = st.missing
				jr.Reachable = true
			} else {
				if st.err != nil {
					jr.ErrorMessage = st.err.Error()
				}
			}
		}
		relayResults[r] = jr
	}

	// Publish missing events
	if len(missing) > 0 {
		for _, event := range missing {
			pubCh := internalRelay.PublishEventWithProgress(context.Background(), event, relays, timeout)
			for res := range pubCh {
				if jr, ok := relayResults[res.URL]; ok {
					if res.OK {
						jr.Published++
					} else {
						jr.Failed++
					}
				}
			}
		}
	}

	// Build output
	out := syncJSONOutput{
		Account:        npub,
		LocalEvents:    len(localEvents),
		SyncableEvents: len(localSyncable),
		SkippedEvents:  localSkipped,
		SavedLocally:   savedLocally,
	}
	for _, r := range relays {
		out.Relays = append(out.Relays, *relayResults[r])
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runSyncInteractive(cmd *cobra.Command, args []string) error {
	dimFn := color.New(color.Faint)
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen)
	greenFn := color.New(color.FgGreen).SprintFunc()
	redFn := color.New(color.FgRed).SprintFunc()

	npub, err := loadAccount()
	if err != nil {
		return err
	}

	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	profileName := resolveProfileName(npub)

	// Show account summary
	alias := ""
	if aliases, aErr := config.LoadGlobalAliases(); aErr == nil {
		for a, n := range aliases {
			if n == npub {
				alias = a
				break
			}
		}
	}
	profileDir, _ := config.ProfileDir(npub)
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(profileDir, home) {
		profileDir = "~" + profileDir[len(home):]
	}

	sentEvents, _ := cache.LoadSentEvents(npub)

	// Count DM conversations
	dmDir := ""
	if dir, err := config.ProfileDir(npub); err == nil {
		dmDir = filepath.Join(dir, "directmessages")
	}
	dmConversations := 0
	dmEvents := 0
	if dmDir != "" {
		if entries, err := os.ReadDir(dmDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
					dmConversations++
					// Count lines in each file
					if f, err := os.Open(filepath.Join(dmDir, e.Name())); err == nil {
						scanner := bufio.NewScanner(f)
						for scanner.Scan() {
							dmEvents++
						}
						f.Close()
					}
				}
			}
		}
	}

	displayName := profileName
	if displayName == "" {
		displayName = npub[:20] + "..."
	}

	fmt.Printf("Account: %s\n\n", cyan(displayName))
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-16s", "Npub:")), npub)
	if alias != "" {
		fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-16s", "Alias:")), alias)
	}
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-16s", "Directory:")), profileDir)
	fmt.Printf("  %s %d\n", cyan(fmt.Sprintf("%-16s", "Sent events:")), len(sentEvents))
	if dmConversations > 0 {
		fmt.Printf("  %s %d event%s across %d conversation%s\n",
			cyan(fmt.Sprintf("%-16s", "Direct messages:")),
			dmEvents, plural(dmEvents), dmConversations, plural(dmConversations))
	} else {
		fmt.Printf("  %s none\n", cyan(fmt.Sprintf("%-16s", "Direct messages:")))
	}
	fmt.Println()

	// Load configured relays
	allRelays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}
	if len(allRelays) == 0 {
		return fmt.Errorf("no relays configured — run `nostr relays add <url>` first")
	}

	// Reuse sent events from the summary above
	localEvents := sentEvents
	localSyncable, localSkipped := syncableEvents(localEvents)

	localIDs := make(map[string]bool, len(localEvents))
	for _, ev := range localEvents {
		localIDs[ev.ID] = true
	}

	myHex := pubHex

	// Start fetching from ALL relays immediately (before user selects)
	timeout := time.Duration(timeoutFlag) * time.Millisecond
	filter := nostr.Filter{
		Authors: []string{pubHex},
	}
	fetchCh := internalRelay.FetchEventsPerRelay(context.Background(), filter, allRelays, timeout)

	// Collect fetch results in background into a shared map
	fetchStates := make(map[string]*relayFetchState)
	fetchResultCh := make(chan internalRelay.FetchResult, len(allRelays))
	go func() {
		for res := range fetchCh {
			fetchResultCh <- res
		}
		close(fetchResultCh)
	}()

	processFetchResult := func(res internalRelay.FetchResult) {
		st := &relayFetchState{done: true, eventIDs: make(map[string]bool), duration: res.Duration, err: res.Err}
		if res.Err == nil {
			st.ok = true
			st.count = len(res.Events)
			for _, ev := range res.Events {
				st.eventIDs[ev.ID] = true
				if !localIDs[ev.ID] {
					st.remoteOnly++
				}
			}
			for _, ev := range localSyncable {
				if !st.eventIDs[ev.ID] {
					st.missing++
				}
			}
		}
		fetchStates[res.URL] = st
	}

	// --- Select relays (with live fetch status) ---
	var relays []string
	if syncRelayFlag != "" {
		// Match by full URL or just domain name
		var matched string
		for _, r := range allRelays {
			if r == syncRelayFlag || relayHost(r) == syncRelayFlag {
				matched = r
				break
			}
		}
		if matched == "" {
			return fmt.Errorf("relay %s is not in your configured relays", syncRelayFlag)
		}
		relays = []string{matched}

		// Show single relay with live fetch progress
		host := relayHost(matched)
		fmt.Printf("  %s %s\n", dimFn.Sprint(ui.SpinnerFrames[0]), dimFn.Sprint(host))

		ticker := time.NewTicker(80 * time.Millisecond)
		frame := 0
		waitDone := false
		for !waitDone {
			select {
			case res, ok := <-fetchResultCh:
				if !ok {
					waitDone = true
					break
				}
				processFetchResult(res)
				if res.URL == matched {
					fmt.Print("\033[1A\r\033[K")
					if st := fetchStates[matched]; st.ok {
						syncInfo := fmt.Sprintf("%d event%s", st.count, plural(st.count))
						if st.missing > 0 {
							syncInfo += fmt.Sprintf(", %d to push", st.missing)
						}
						if st.remoteOnly > 0 {
							syncInfo += fmt.Sprintf(", %d to pull", st.remoteOnly)
						}
						if st.missing == 0 && st.remoteOnly == 0 {
							syncInfo += ", in sync"
						}
						syncInfo += fmt.Sprintf(", %dms", st.duration.Milliseconds())
						fmt.Printf("  %s %s  %s\n", greenFn("✓"), host,
							dimFn.Sprint(syncInfo))
					} else {
						fmt.Printf("  %s %s  %s\n", redFn("✗"), host,
							dimFn.Sprintf("%dms", st.duration.Milliseconds()))
						if st.err != nil {
							dimFn.Printf("  %s\n", st.err.Error())
						}
					}
					waitDone = true
				}
			case <-ticker.C:
				if fetchStates[matched] == nil {
					frame++
					f := ui.SpinnerFrames[frame%len(ui.SpinnerFrames)]
					fmt.Print("\033[1A\r\033[K")
					fmt.Printf("  %s %s\n", dimFn.Sprint(f), dimFn.Sprint(host))
				}
			}
		}
		ticker.Stop()
		go func() {
			for range fetchResultCh {
			}
		}()
		fmt.Println()
	} else {
		// Interactive checklist with live fetch (spinner shown inside checkbox)
		fmt.Println("Select relays to sync:")
		relays = syncRelayChecklist(allRelays, fetchStates, fetchResultCh, processFetchResult)
		if len(relays) == 0 {
			go func() {
				for range fetchResultCh {
				}
			}()
			fmt.Println("No relays selected.")
			return nil
		}
	}

	if syncRelayFlag != "" {
		fmt.Printf("Sync with %s? [Y/n] ", cyan(relayHost(relays[0])))
		var answer string
		fmt.Scanln(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "n" || answer == "no" {
			fmt.Println("Cancelled.")
			return nil
		}
	} else {
		dimFn.Printf("  Tip: use nostr sync --relay <url> to sync with a single relay\n")
	}
	fmt.Println()

	// Drain any remaining fetch results
	drainDone := false
	for !drainDone {
		select {
		case res, ok := <-fetchResultCh:
			if !ok {
				drainDone = true
			} else {
				processFetchResult(res)
			}
		default:
			drainDone = true
		}
	}

	// Build remote ID set from selected relays
	remoteIDs := make(map[string]bool)
	for _, r := range relays {
		if st, ok := fetchStates[r]; ok && st.ok {
			for id := range st.eventIDs {
				remoteIDs[id] = true
			}
		}
	}

	n := len(remoteIDs)
	fmt.Printf("Found %s unique event%s across selected relays\n", cyan(fmt.Sprintf("%d", n)), plural(n))

	// Save new events from relays locally
	var newRemoteIDs []string
	for id := range remoteIDs {
		if !localIDs[id] {
			newRemoteIDs = append(newRemoteIDs, id)
		}
	}

	newFromRelays := 0
	if len(newRemoteIDs) > 0 {
		n := len(newRemoteIDs)
		sp := ui.NewSpinner(fmt.Sprintf("Saving %d new event%s from relays...", n, plural(n)))
		idFilter := nostr.Filter{
			IDs:   newRemoteIDs,
			Limit: len(newRemoteIDs),
		}
		fetched, fErr := internalRelay.FetchEvents(context.Background(), idFilter, relays)
		sp.Stop()
		if fErr == nil {
			for _, ev := range fetched {
				saveSyncedEvent(npub, myHex, *ev)
				newFromRelays++
			}
		}
		if newFromRelays > 0 {
			fmt.Printf("Saved %s new event%s from relays locally\n", cyan(fmt.Sprintf("%d", newFromRelays)), plural(newFromRelays))
		}
	}

	// Fetch incoming DMs (sent to this account)
	{
		sp := ui.NewSpinner("Fetching incoming direct messages...")
		dmFilter := nostr.Filter{
			Kinds: []int{nostr.KindEncryptedDirectMessage},
			Tags:  nostr.TagMap{"p": []string{pubHex}},
		}
		dmFetchCh := internalRelay.FetchEventsPerRelay(context.Background(), dmFilter, relays, timeout)
		dmSaved := 0
		for res := range dmFetchCh {
			if res.Err != nil {
				continue
			}
			for _, ev := range res.Events {
				saveSyncedEvent(npub, myHex, *ev)
				dmSaved++
			}
		}
		sp.Stop()
		if dmSaved > 0 {
			fmt.Printf("Saved %s incoming direct message%s\n", cyan(fmt.Sprintf("%d", dmSaved)), plural(dmSaved))
		}
	}

	// Find syncable events missing from relays
	var missing []nostr.Event
	for _, ev := range localSyncable {
		if !remoteIDs[ev.ID] {
			missing = append(missing, ev)
		}
	}

	if len(missing) == 0 {
		fmt.Println()
		green.Println("✓ Everything is in sync")
		if localSkipped > 0 {
			dimFn.Printf("  %d older event%s not synced (replaceable — relays only keep the latest version)\n", localSkipped, plural(localSkipped))
		}
		printSyncSummary(dimFn, npub, newFromRelays, 0, 0)
		return nil
	}

	sort.Slice(missing, func(i, j int) bool {
		return missing[i].CreatedAt < missing[j].CreatedAt
	})

	fmt.Println()
	n = len(missing)
	fmt.Printf("Publishing %s event%s missing from relays:\n", cyan(fmt.Sprintf("%d", n)), plural(n))
	fmt.Println()

	// Publish missing events
	publishedCount := 0
	failedCount := 0

	for i, event := range missing {
		ts := event.CreatedAt.Time().Format("2006-01-02 15:04")
		kindLabel := eventKindLabel(event.Kind)
		snippet := eventSnippet(event)

		fmt.Printf("%s %s %s %s\n",
			cyan(fmt.Sprintf("%d/%d", i+1, len(missing))),
			dimFn.Sprint(ts),
			kindLabel,
			snippet,
		)

		pubCh := internalRelay.PublishEventWithProgress(context.Background(), event, relays, timeout)

		anySuccess := false
		for res := range pubCh {
			host := relayHost(res.URL)
			ms := res.Duration.Milliseconds()
			if res.OK {
				fmt.Printf("  %s published to %s  %s\n", greenFn("✓"), host, dimFn.Sprintf("%dms", ms))
				anySuccess = true
			} else {
				fmt.Printf("  %s failed on %s  %s\n", redFn("✗"), host, dimFn.Sprintf("%dms", ms))
			}
		}

		if anySuccess {
			publishedCount++
		} else {
			failedCount++
		}
	}

	fmt.Println()
	if localSkipped > 0 {
		dimFn.Printf("  %d older event%s not synced (replaceable — relays only keep the latest version)\n", localSkipped, plural(localSkipped))
	}
	printSyncSummary(dimFn, npub, newFromRelays, publishedCount, failedCount)

	return nil
}

func printSyncSummary(dimFn *color.Color, npub string, saved, published, failed int) {
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan).SprintFunc()

	if saved > 0 || published > 0 {
		green.Printf("✓ Sync complete: %s event%s saved locally, %s event%s published to relays\n",
			cyan(fmt.Sprintf("%d", saved)), plural(saved),
			cyan(fmt.Sprintf("%d", published)), plural(published))
	} else {
		green.Println("✓ Sync complete")
	}
	if failed > 0 {
		color.New(color.FgRed).Printf("  %d event%s failed to publish\n", failed, plural(failed))
	}

	if saved > 0 {
		eventsPath := cache.SentEventsPath(npub)
		if eventsPath != "" {
			home, _ := os.UserHomeDir()
			if home != "" {
				eventsPath = strings.Replace(eventsPath, home, "~", 1)
			}
			dimFn.Printf("  Events saved in %s\n", eventsPath)
		}
	}
}

// syncRelayChecklist shows an interactive checklist where fetch results
// appear live via bubbletea. Auto-unchecks relays that are in sync.
func syncRelayChecklist(
	relays []string,
	fetchStates map[string]*relayFetchState,
	fetchCh <-chan internalRelay.FetchResult,
	processFn func(internalRelay.FetchResult),
) []string {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		for res := range fetchCh {
			processFn(res)
		}
		var selected []string
		for _, r := range relays {
			if st, ok := fetchStates[r]; ok && st.done && st.ok && (st.missing > 0 || st.remoteOnly > 0) {
				selected = append(selected, r)
			}
		}
		if len(selected) == 0 {
			return relays
		}
		return selected
	}

	items := make([]ui.CheckboxItem, len(relays))
	for i, r := range relays {
		items[i] = ui.CheckboxItem{
			Label:   relayHost(r),
			Checked: true,
		}
	}

	// Build a map from relay URL to item index for routing fetch results
	relayIndexMap := make(map[string]int, len(relays))
	for i, r := range relays {
		relayIndexMap[r] = i
	}

	// Create status channels and feed them from fetchCh
	statusChs := make([]chan ui.CheckboxStatusUpdate, len(relays))
	for i := range relays {
		statusChs[i] = make(chan ui.CheckboxStatusUpdate, 4)
	}

	go func() {
		for res := range fetchCh {
			processFn(res)
			idx, ok := relayIndexMap[res.URL]
			if !ok {
				continue
			}
			st := fetchStates[res.URL]
			if st == nil || !st.done {
				continue
			}
			falseVal := false
			trueVal := true
			var status string
			var setChecked *bool
			if st.ok {
				if st.missing == 0 && st.remoteOnly == 0 {
					status = fmt.Sprintf("%d event%s, in sync", st.count, plural(st.count))
					setChecked = &falseVal
				} else {
					parts := fmt.Sprintf("%d event%s", st.count, plural(st.count))
					if st.missing > 0 {
						parts += fmt.Sprintf(", %d to push", st.missing)
					}
					if st.remoteOnly > 0 {
						parts += fmt.Sprintf(", %d to pull", st.remoteOnly)
					}
					status = parts
					setChecked = &trueVal
				}
			} else {
				status = "failed"
				setChecked = &falseVal
			}
			statusChs[idx] <- ui.CheckboxStatusUpdate{Status: status, SetChecked: setChecked}
		}
		for _, ch := range statusChs {
			close(ch)
		}
	}()

	result := ui.RunCheckboxPicker(ui.CheckboxPickerConfig{
		Title:     "Select relays to sync:",
		Items:     items,
		StatusChs: statusChs,
	})

	if result.Cancelled {
		// Drain remaining fetch results
		go func() {
			for range fetchCh {
			}
		}()
		return nil
	}

	var selected []string
	for _, idx := range result.Selected {
		if idx < len(relays) {
			selected = append(selected, relays[idx])
		}
	}
	return selected
}

// saveSyncedEvent saves an event fetched during sync to the right storage.
// Authored events go to events.jsonl. DM events also go to directmessages/.
func saveSyncedEvent(npub, myHex string, ev nostr.Event) {
	// All authored events go to the sent events backup
	if ev.PubKey == myHex {
		_ = cache.LogSentEvent(npub, ev)
	}

	// Route DM events to per-counterparty conversation files
	if ev.Kind == nostr.KindEncryptedDirectMessage {
		var counterparty string
		if ev.PubKey == myHex {
			// I sent this DM — counterparty is in the "p" tag
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					counterparty = tag[1]
					break
				}
			}
		} else {
			// Someone sent this DM to me — counterparty is the author
			counterparty = ev.PubKey
		}
		if counterparty != "" {
			_ = cache.LogDMEvent(npub, counterparty, ev)
		}
	}
}

// eventKindLabel returns a human-readable label for event kinds.
func eventKindLabel(kind int) string {
	switch kind {
	case nostr.KindProfileMetadata:
		return "profile update"
	case nostr.KindTextNote:
		return "public note"
	case nostr.KindFollowList:
		return "follow list"
	case nostr.KindEncryptedDirectMessage:
		return "direct message"
	case nostr.KindDeletion:
		return "deletion"
	case nostr.KindReaction:
		return "reaction"
	case nostr.KindRelayListMetadata:
		return "relay list"
	default:
		return fmt.Sprintf("kind %d", kind)
	}
}

// eventSnippet returns a short preview of an event's content.
func eventSnippet(event nostr.Event) string {
	content := event.Content
	if content == "" {
		return dimStr(event.ID[:16] + "...")
	}
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > 60 {
		content = content[:57] + "..."
	}
	return content
}

func dimStr(s string) string {
	return color.New(color.Faint).Sprint(s)
}

// syncChecklistRenderTo renders the checklist to the given writer (or stdout
// if nil) and returns the number of lines written. Extracted for testability.
func syncChecklistRenderTo(w *strings.Builder, relays []string, checked []bool, cursor int, fetchStates map[string]*relayFetchState) int {
	out := func(format string, a ...interface{}) {
		if w != nil {
			fmt.Fprintf(w, format, a...)
		} else {
			fmt.Printf(format, a...)
		}
	}

	lineCount := 0
	for i, r := range relays {
		host := relayHost(r)

		var check string
		if checked[i] {
			check = "[✓]"
		} else {
			check = "[ ]"
		}

		status := ""
		if st, ok := fetchStates[r]; ok && st.done {
			if st.ok {
				if st.missing == 0 && st.remoteOnly == 0 {
					status = fmt.Sprintf("  %d event%s, in sync", st.count, plural(st.count))
				} else {
					parts := fmt.Sprintf("  %d event%s", st.count, plural(st.count))
					if st.missing > 0 {
						parts += fmt.Sprintf(", %d to push", st.missing)
					}
					if st.remoteOnly > 0 {
						parts += fmt.Sprintf(", %d to pull", st.remoteOnly)
					}
					status = parts
				}
			} else {
				status = "  failed"
			}
		}

		_ = cursor
		out("  %s %s%s\r\n", check, host, status)
		lineCount++
	}

	selectedCount := 0
	for i := range relays {
		if checked[i] {
			selectedCount++
		}
	}
	out("\r\n")
	lineCount++
	out("  hint line (%d selected)\r\n", selectedCount)
	lineCount++

	return lineCount
}
