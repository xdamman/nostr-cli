package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var profileFlag string

var rootCmd = &cobra.Command{
	Use:   "nostr-cli",
	Short: "A command-line client for the Nostr protocol",
	Long:  "nostr-cli lets you manage Nostr identities, publish notes, and interact with relays from the terminal.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "npub of the profile to use (default: active profile)")
}
