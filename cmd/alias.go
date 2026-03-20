package cmd

import (
	"fmt"
	"os"
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

	// Create symlink ~/.nostr/profiles/<alias> -> ~/.nostr/profiles/<npub>
	if err := config.CreateProfileSymlink(name, targetNpub); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create profile symlink: %v\n", err)
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

	// Remove profile symlink if it exists
	_ = config.RemoveProfileSymlink(name)

	green.Printf("✓ Removed alias %s\n", name)
	return nil
}
