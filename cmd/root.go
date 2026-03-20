package cmd

import (
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var profileFlag string

var rootCmd = &cobra.Command{
	Use:   "nostr",
	Short: "A command-line client for the Nostr protocol",
	Long:  "nostr lets you manage Nostr identities, publish notes, and interact with relays from the terminal.",
	// Catch-all: treat unknown first arg as user lookup
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return runUserLookup(args)
		}
		return cmd.Help()
	},
	Args: cobra.ArbitraryArgs,
}

func Execute() {
	// Intercept unknown subcommands to treat as user lookup
	rootCmd.TraverseChildren = true
	if err := rootCmd.Execute(); err != nil {
		// If the error is about unknown command, try user lookup
		errStr := err.Error()
		if len(os.Args) > 1 && (contains(errStr, "unknown command") || contains(errStr, "unknown flag")) {
			if lookupErr := runUserLookup(os.Args[1:]); lookupErr == nil {
				return
			}
		}
		color.New(color.FgRed).Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func init() {
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "npub of the profile to use (default: active profile)")
}
