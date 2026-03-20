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
	Long:  "Create, list, or remove aliases. Aliases are profile-scoped shortcuts for npubs.",
	RunE:  runAlias,
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
}

func runAlias(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan).SprintFunc()

	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		// List aliases
		aliases, err := resolve.LoadAliases(npub)
		if err != nil {
			return err
		}
		if len(aliases) == 0 {
			fmt.Println("No aliases configured.")
			fmt.Println("Usage: nostr alias [name] [npub|nip05]")
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
		return nil
	}

	if len(args) < 2 {
		return fmt.Errorf("usage: nostr alias [name] [npub|nip05]")
	}

	name := args[0]
	target := args[1]

	// Resolve target to npub
	targetNpub, err := resolve.ResolveToNpub(npub, target)
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", target, err)
	}

	aliases, err := resolve.LoadAliases(npub)
	if err != nil {
		return err
	}

	aliases[name] = targetNpub
	if err := resolve.SaveAliases(npub, aliases); err != nil {
		return err
	}

	green.Printf("✓ Alias %s → %s\n", name, targetNpub)
	return nil
}

func runAliasRm(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)

	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	name := args[0]
	aliases, err := resolve.LoadAliases(npub)
	if err != nil {
		return err
	}

	if _, ok := aliases[name]; !ok {
		return fmt.Errorf("alias %q not found", name)
	}

	delete(aliases, name)
	if err := resolve.SaveAliases(npub, aliases); err != nil {
		return err
	}

	green.Printf("✓ Removed alias %s\n", name)
	return nil
}
