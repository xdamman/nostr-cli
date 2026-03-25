package cmd

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"golang.org/x/term"
)

var (
	profileFlag string
	noColorFlag bool
	timeoutFlag int
	rawFlag     bool
)

var rootCmd = &cobra.Command{
	Use:   "nostr",
	Short: "A command-line client for the Nostr protocol",
	Long: `Nostr is an open protocol for censorship-resistant social networking
and other decentralized applications. It uses cryptographic keys for
identity — no accounts, no servers you depend on.

nostr-cli lets you manage profiles, publish notes, send encrypted DMs,
and interact with relays from the terminal.

A <profile> can be specified as an npub (npub1...), a local alias, or a NIP-05
address (e.g. user@domain.com).

Most commands support --json for machine-readable output.`,
	// Catch-all: treat unknown first arg as user lookup
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Respect --no-color flag and NO_COLOR env var (https://no-color.org/)
		if noColorFlag || os.Getenv("NO_COLOR") != "" {
			color.NoColor = true
		}
		// Auto-migrate per-profile aliases to global aliases.json
		_ = config.MigrateAliases()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			// Check if first arg looks like a NIP reference (nip01, nip-44, NIP01, etc.)
			if isNIPArg(args[0]) {
				return fetchAndDisplayNIP(args[0])
			}
			return runUserLookup(args)
		}
		// nostr --watch: stream all events from followed accounts
		if userWatchFlag {
			return runWatchFeed()
		}
		// If stdin is piped, read it and post as a note
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}
			message := strings.TrimSpace(string(data))
			if message == "" {
				return fmt.Errorf("empty input from pipe")
			}
			return runPost(cmd, []string{message})
		}
		// If logged in, launch interactive shell
		_, err := config.ActiveProfile()
		if err == nil {
			return runShell()
		}
		// Show the specific error (e.g. "run nostr login" or "run nostr switch")
		return err
	},
	Args: cobra.ArbitraryArgs,
}

// colorizeHelp applies color to help output text.
func colorizeHelp(s string) string {
	if color.NoColor {
		return s
	}
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow, color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	dimItalic := color.New(color.Faint).SprintFunc()

	sectionHeaders := map[string]bool{
		"Usage:":                  true,
		"Available Commands:":     true,
		"Additional Commands:":    true,
		"Social Commands:":        true,
		"Profile Commands:":       true,
		"Infrastructure Commands:": true,
		"Reference:":              true,
		"App Commands:":           true,
		"Flags:":                  true,
		"Global Flags:":           true,
		"Aliases:":                true,
		"Examples:":               true,
	}

	lines := strings.Split(s, "\n")
	inExamples := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Section headers
		if sectionHeaders[trimmed] {
			lines[i] = yellow(line)
			inExamples = trimmed == "Examples:"
			continue
		}

		// Example lines (dim)
		if inExamples && strings.HasPrefix(line, "  ") {
			lines[i] = dimItalic(line)
			continue
		}
		if inExamples && trimmed == "" {
			continue
		}
		if inExamples && !strings.HasPrefix(line, " ") {
			inExamples = false
		}

		// Colorize flag names (lines with --)
		if strings.HasPrefix(line, "  ") && strings.Contains(line, "--") {
			flagRe := regexp.MustCompile(`(--?\S+)`)
			lines[i] = flagRe.ReplaceAllStringFunc(line, func(match string) string {
				return green(match)
			})
			continue
		}

		// Usage lines with "nostr" — colorize command name
		if strings.HasPrefix(trimmed, "nostr ") && i > 0 {
			prevTrimmed := strings.TrimSpace(lines[i-1])
			if prevTrimmed == "" || sectionHeaders[prevTrimmed+":"] || strings.HasSuffix(prevTrimmed, ":") {
				lines[i] = "  " + cyan(trimmed)
			}
			continue
		}

		// Command list entries: "  command   description"
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && len(trimmed) > 0 {
			parts := strings.SplitN(trimmed, " ", 2)
			if len(parts) == 2 {
				cmdName := parts[0]
				rest := strings.TrimLeft(parts[1], " ")
				if len(cmdName) > 0 && !strings.HasPrefix(cmdName, "-") && len(cmdName) < 20 {
					padding := strings.TrimPrefix(line, "  "+cmdName)
					paddingBeforeDesc := strings.TrimSuffix(padding, rest)
					lines[i] = "  " + cyan(cmdName) + paddingBeforeDesc + rest
				}
			}
		}

		// "Use ..." footer line
		if strings.HasPrefix(trimmed, "Use \"nostr") {
			lines[i] = dimItalic(line)
		}
	}
	return strings.Join(lines, "\n")
}

// isNIPArg checks if an argument looks like a NIP reference (nip01, nip-44, NIP01, etc.)
func isNIPArg(s string) bool {
	re := regexp.MustCompile(`(?i)^nip[- ]?\d+$`)
	return re.MatchString(s)
}

func Execute() {
	// Initialize built-in commands so we can set their groups
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	for _, cmd := range rootCmd.Commands() {
		switch cmd.Name() {
		case "completion":
			cmd.GroupID = "app"
		case "help":
			cmd.GroupID = "reference"
		}
	}

	// Intercept unknown subcommands to treat as user lookup
	rootCmd.TraverseChildren = true
	if err := rootCmd.Execute(); err != nil {
		// If the error is about unknown command, try user lookup
		errStr := err.Error()
		if len(os.Args) > 1 && (contains(errStr, "unknown command") || contains(errStr, "unknown flag")) {
			// Try NIP lookup first
			if isNIPArg(os.Args[1]) {
				if nipErr := fetchAndDisplayNIP(os.Args[1]); nipErr == nil {
					return
				}
			}
			if lookupErr := runUserLookup(os.Args[1:]); lookupErr == nil {
				return
			}
		}
		color.New(color.FgRed).Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// exactArgs returns a cobra.PositionalArgs that shows help when the wrong number of args is given.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			cmd.Help()
			fmt.Println()
			if len(args) < n {
				return fmt.Errorf("requires %d arg(s), received %d", n, len(args))
			}
			return fmt.Errorf("accepts %d arg(s), received %d", n, len(args))
		}
		return nil
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

// printActiveProfile prints a one-line header showing the active profile name and npub.
func printActiveProfile(npub string) {
	dim := color.New(color.Faint)
	name := resolveProfileName(npub)
	short := npub
	if len(short) > 20 {
		short = short[:20] + "..."
	}
	if name != "" {
		dim.Printf("Using profile %s (%s)\n", name, short)
	} else {
		dim.Printf("Using profile %s\n", short)
	}
}

// resolveProfileName tries to find a display name for an npub via aliases or cached profile metadata.
func resolveProfileName(npub string) string {
	// Check global aliases (reverse lookup)
	aliases, _ := config.LoadGlobalAliases()
	for name, target := range aliases {
		if target == npub {
			return name
		}
	}
	// Try cached profile metadata
	meta, _ := profile.LoadCached(npub)
	if meta != nil {
		if meta.Name != "" {
			return meta.Name
		}
		if meta.DisplayName != "" {
			return meta.DisplayName
		}
	}
	return ""
}

// loadProfile resolves the --profile flag to an npub.
// If no flag is set, returns the active profile.
// The flag value is resolved as: npub → alias → NIP-05 (using the active profile's context).
func loadProfile() (string, error) {
	if profileFlag == "" {
		return config.ActiveProfile()
	}
	if strings.HasPrefix(profileFlag, "npub1") {
		return profileFlag, nil
	}
	// Resolve using the active profile's aliases
	activeNpub, err := config.ActiveProfile()
	if err != nil {
		return "", fmt.Errorf("cannot resolve --profile %q: no active profile to look up aliases", profileFlag)
	}
	return resolve.ResolveToNpub(activeNpub, profileFlag)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "npub, alias, or username of the profile to use (default: active profile)")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().IntVar(&timeoutFlag, "timeout", 2000, "Timeout per relay in milliseconds")
	rootCmd.PersistentFlags().BoolVar(&rawFlag, "raw", false, "Output raw Nostr event JSON (wire format)")

	// Command groups
	rootCmd.AddGroup(
		&cobra.Group{ID: "social", Title: "Social Commands:"},
		&cobra.Group{ID: "profile", Title: "Profile Commands:"},
		&cobra.Group{ID: "infra", Title: "Infrastructure Commands:"},
		&cobra.Group{ID: "reference", Title: "Reference:"},
		&cobra.Group{ID: "app", Title: "App Commands:"},
	)

	// Set custom help function to colorize output
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		// Show Long description before usage
		if cmd.Long != "" {
			fmt.Println(colorizeHelp(cmd.Long))
			fmt.Println()
		}
		usage := cmd.UsageString()
		fmt.Print(colorizeHelp(usage))
	})
}
