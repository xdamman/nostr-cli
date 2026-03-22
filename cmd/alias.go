package cmd

import (
	"fmt"
	"sort"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

var aliasCmd = &cobra.Command{
	Use:   "alias [name] [npub|nip05]",
	Short: "Manage aliases for users",
	Long:  "Create, list, or remove aliases. Aliases are global shortcuts for npubs.",
	RunE:  runAlias,
}

var aliasesCmd = &cobra.Command{
	Use:   "aliases",
	Short: "List all aliases",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAlias(cmd, nil)
	},
}

var aliasRmCmd = &cobra.Command{
	Use:   "rm [name]",
	Short: "Remove an alias",
	Args:  cobra.ExactArgs(1),
	RunE:  runAliasRm,
}

func init() {
	aliasCmd.AddCommand(aliasRmCmd)
	rootCmd.AddCommand(aliasCmd)
	rootCmd.AddCommand(aliasesCmd)
}

func runAlias(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan).SprintFunc()

	if len(args) == 0 {
		// List aliases
		aliases, err := config.LoadGlobalAliases()
		if err != nil {
			return err
		}
		if len(aliases) == 0 {
			dim := color.New(color.Faint)
			fmt.Println("No aliases configured.")
			fmt.Println()
			dim.Println("Add an alias:  nostr alias <name> <npub|nip05>")
			dim.Println("Send a DM:     nostr dm <name> <message>")
			return nil
		}

		// Sort for consistent output
		names := make([]string, 0, len(aliases))
		for name := range aliases {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			fmt.Printf("  %s → %s\n", cyan(name), aliases[name])
		}
		fmt.Println()
		dim := color.New(color.Faint)
		dim.Println("Add an alias:     nostr alias <name> <npub|nip05>")
		dim.Println("Remove an alias:  nostr alias rm <name>")
		return nil
	}

	if len(args) < 2 {
		return fmt.Errorf("usage: nostr alias [name] [npub|nip05]")
	}

	name := args[0]
	target := args[1]

	// Resolve target to npub (need active profile for NIP-05 relay lookup)
	activeNpub, _ := config.ActiveProfile()
	targetNpub, err := resolve.ResolveToNpub(activeNpub, target)
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", target, err)
	}

	if err := config.SetGlobalAlias(name, targetNpub); err != nil {
		return err
	}

	green.Printf("✓ Alias %s → %s\n", name, targetNpub)
	return nil
}

func runAliasRm(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)

	name := args[0]
	if err := config.RemoveGlobalAlias(name); err != nil {
		return err
	}

	green.Printf("✓ Removed alias %s\n", name)
	return nil
}
