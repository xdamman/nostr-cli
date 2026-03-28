package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
)

var profilesJSONFlag bool

var accountsCmd = &cobra.Command{
	Use:     "accounts",
	Aliases: []string{"profiles"},
	Short:   "List all local accounts",
	GroupID: "profile",
	RunE:    runAccounts,
}

func init() {
	accountsCmd.Flags().BoolVar(&profilesJSONFlag, "json", false, "Output as JSON")
	accountsCmd.AddCommand(profileRmCmd)
	rootCmd.AddCommand(accountsCmd)
}

type profileInfo struct {
	Npub        string `json:"npub"`
	Name        string `json:"name,omitempty"`
	Active      bool   `json:"active"`
	Events      int    `json:"events"`
	DisplayName string `json:"display_name,omitempty"`
	About       string `json:"about,omitempty"`
	Picture     string `json:"picture,omitempty"`
	NIP05       string `json:"nip05,omitempty"`
	Banner      string `json:"banner,omitempty"`
	Website     string `json:"website,omitempty"`
	LUD16       string `json:"lud16,omitempty"`
}

func runAccounts(cmd *cobra.Command, args []string) error {
	entries, err := listSwitchableProfiles()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No accounts found. Run 'nostr login' to create one.")
		return nil
	}

	activeNpub, _ := config.ActiveProfile()

	// Reverse alias lookup
	aliases, _ := config.LoadGlobalAliases()
	aliasFor := make(map[string]string)
	for name, npub := range aliases {
		aliasFor[npub] = name
	}

	var infos []profileInfo
	for _, e := range entries {
		name := e.name
		if name == "" {
			name = aliasFor[e.npub]
		}
		meta, _ := profile.LoadCached(e.npub)
		if name == "" && meta != nil {
			name = profileName(meta)
		}
		info := profileInfo{
			Npub:   e.npub,
			Name:   name,
			Active: e.npub == activeNpub,
			Events: cache.CountSentEvents(e.npub),
		}
		if meta != nil {
			info.DisplayName = meta.DisplayName
			info.About = meta.About
			info.Picture = meta.Picture
			info.NIP05 = meta.NIP05
			info.Banner = meta.Banner
			info.Website = meta.Website
			info.LUD16 = meta.LUD16
		}
		infos = append(infos, info)
	}

	if profilesJSONFlag {
		data, err := json.MarshalIndent(infos, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	dim := color.New(color.Faint)

	for _, info := range infos {
		marker := "  "
		if info.Active {
			marker = green("▸ ")
		}

		// Short npub: first 8 + last 4 chars
		shortNpub := info.Npub
		if len(shortNpub) > 16 {
			shortNpub = shortNpub[:12] + "…" + shortNpub[len(shortNpub)-4:]
		}

		// Event count suffix
		var eventStr string
		if info.Events == 1 {
			eventStr = "1 event"
		} else {
			eventStr = fmt.Sprintf("%d events", info.Events)
		}

		if info.Name != "" {
			if info.Active {
				fmt.Printf("%s%s %s %s %s\n", marker, cyan(info.Name), dim.Sprint(shortNpub), dim.Sprint(eventStr), bold("(active)"))
			} else {
				fmt.Printf("%s%s %s %s\n", marker, cyan(info.Name), dim.Sprint(shortNpub), dim.Sprint(eventStr))
			}
		} else {
			if info.Active {
				fmt.Printf("%s%s %s %s\n", marker, dim.Sprint(shortNpub), dim.Sprint(eventStr), bold("(active)"))
			} else {
				fmt.Printf("%s%s %s\n", marker, dim.Sprint(shortNpub), dim.Sprint(eventStr))
			}
		}
	}

	// Also show aliases that point to profiles not in the list
	var aliasHints []string
	for name, npub := range aliases {
		found := false
		for _, info := range infos {
			if info.Npub == npub {
				found = true
				break
			}
		}
		if !found {
			aliasHints = append(aliasHints, fmt.Sprintf("  %s → %s", cyan(name), npub))
		}
	}

	if len(aliasHints) > 0 {
		fmt.Println()
		dim.Println("External aliases:")
		for _, h := range aliasHints {
			fmt.Println(h)
		}
	}

	fmt.Println()
	dim.Println("An <account> can be an alias, npub, or NIP-05 address.")
	dim.Println("")
	dim.Println("  nostr switch <account>        Switch active account")
	dim.Println("  nostr profile update           Update your Nostr profile metadata")
	dim.Println("  nostr accounts rm              Remove an account")
	dim.Println("  nostr login                    Add a new account")

	return nil
}
