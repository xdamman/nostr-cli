package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
)

var profilesJSONFlag bool

var profilesCmd = &cobra.Command{
	Use:     "profiles",
	Short:   "List all local profiles",
	GroupID: "profile",
	RunE:    runProfiles,
}

func init() {
	profilesCmd.Flags().BoolVar(&profilesJSONFlag, "json", false, "Output as JSON")
	rootCmd.AddCommand(profilesCmd)
}

type profileInfo struct {
	Npub        string `json:"npub"`
	Name        string `json:"name,omitempty"`
	Active      bool   `json:"active"`
	DisplayName string `json:"display_name,omitempty"`
	About       string `json:"about,omitempty"`
	Picture     string `json:"picture,omitempty"`
	NIP05       string `json:"nip05,omitempty"`
	Banner      string `json:"banner,omitempty"`
	Website     string `json:"website,omitempty"`
	LUD16       string `json:"lud16,omitempty"`
}

func runProfiles(cmd *cobra.Command, args []string) error {
	entries, err := listSwitchableProfiles()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No profiles found. Run 'nostr login' to create one.")
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

		if info.Name != "" {
			if info.Active {
				fmt.Printf("%s%s %s %s\n", marker, cyan(info.Name), info.Npub, bold("(active)"))
			} else {
				fmt.Printf("%s%s %s\n", marker, cyan(info.Name), info.Npub)
			}
		} else {
			if info.Active {
				fmt.Printf("%s%s %s\n", marker, info.Npub, bold("(active)"))
			} else {
				fmt.Printf("%s%s\n", marker, info.Npub)
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
	dim.Println("A <profile> can be an alias, npub, or NIP-05 address.")
	dim.Println("")
	dim.Println("  nostr switch <profile>        Switch active profile")
	dim.Println("  nostr profile update           Update your profile metadata")
	dim.Println("  nostr profile rm <profile>    Remove a profile")
	dim.Println("  nostr login                    Add a new profile")

	return nil
}
