package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

var switchCmd = &cobra.Command{
	Use:   "switch [npub|alias]",
	Short: "Switch active profile",
	Long:  "Switch to a different profile. Without arguments, lists all available profiles.",
	RunE:  runSwitch,
}

func init() {
	rootCmd.AddCommand(switchCmd)
}

func runSwitch(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	activeNpub, _ := config.ActiveProfile()

	if len(args) == 0 {
		return listProfiles(activeNpub, cyan, bold)
	}

	// Resolve target
	targetNpub := args[0]
	if activeNpub != "" {
		resolved, err := resolve.ResolveToNpub(activeNpub, args[0])
		if err == nil {
			targetNpub = resolved
		}
	}

	// Check profile exists
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
	name := ""
	if meta != nil && meta.Name != "" {
		name = meta.Name
	} else if meta != nil && meta.DisplayName != "" {
		name = meta.DisplayName
	}

	if name != "" {
		green.Printf("✓ Switched to %s (%s)\n", name, targetNpub)
	} else {
		green.Printf("✓ Switched to %s\n", targetNpub)
	}
	return nil
}

func listProfiles(activeNpub string, cyan, bold func(a ...interface{}) string) error {
	base, err := config.BaseDir()
	if err != nil {
		return err
	}

	profilesDir := filepath.Join(base, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No profiles found. Run 'nostr login' to create one.")
			return nil
		}
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No profiles found. Run 'nostr login' to create one.")
		return nil
	}

	fmt.Println("Available profiles:")
	fmt.Println()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		npub := entry.Name()
		active := ""
		if npub == activeNpub {
			active = " " + bold("(active)")
		}

		meta, _ := profile.LoadCached(npub)
		name := ""
		if meta != nil && meta.Name != "" {
			name = meta.Name
		} else if meta != nil && meta.DisplayName != "" {
			name = meta.DisplayName
		}

		if name != "" {
			fmt.Printf("  %s %s%s\n", cyan(name), npub, active)
		} else {
			fmt.Printf("  %s%s\n", npub, active)
		}
	}
	return nil
}
