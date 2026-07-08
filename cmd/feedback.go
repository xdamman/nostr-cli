package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/ui"
)

// nostrcliNpub is the official nostr-cli account. Feedback notes mention it.
const nostrcliNpub = "npub1rxavy4r7n4y4h3gr97teeqnpj7627gxna8kq4439myqwwkt09yhqyuj3mn"

var feedbackDryRun bool

var feedbackCmd = &cobra.Command{
	Use:     "feedback [message]",
	Short:   "Send public feedback to the nostr-cli team (@nostrcli)",
	GroupID: "social",
	Long: `Send feedback about nostr-cli as a PUBLIC note mentioning @nostrcli.

This publishes a regular kind 1 text note from your active account to your
configured relays. It is public: anyone can read it, and it will appear on
your profile like any other note.

The message can come from:
  • Command argument: nostr feedback "Love the DM picker!"
  • Piped stdin:      echo "Found a bug in ..." | nostr feedback
  • Interactive:      nostr feedback (prompts for input)

Flags:
  --dry-run   Sign but don't publish, output JSON

Examples:
  nostr feedback "The --jsonl output is great for bots"
  nostr feedback                      # Interactive prompt
  echo "Feature request: ..." | nostr feedback --jsonl`,
	RunE: runFeedback,
}

func init() {
	feedbackCmd.Flags().BoolVar(&feedbackDryRun, "dry-run", false, "Sign but don't publish — print the signed event")
	rootCmd.AddCommand(feedbackCmd)
}

func runFeedback(cmd *cobra.Command, args []string) error {
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	npub, err := loadAccount()
	if err != nil {
		return err
	}
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}
	nostrcliHex, err := crypto.NpubToHex(nostrcliNpub)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	promptName := resolveProfileName(npub)
	if promptName == "" {
		promptName = pubHex[:8] + "..."
	}

	// Get message: from args, piped stdin, or interactive prompt
	var message string
	if len(args) > 0 {
		message = strings.Join(args, " ")
	} else if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		message = strings.TrimSpace(string(data))
	} else {
		fmt.Printf("%s Feedback is sent as a %s mentioning %s — anyone can read it.\n",
			yellow("!"), yellow("PUBLIC note"), cyan("@nostrcli"))
		fmt.Println()
		result := ui.RunEditlineInput(ui.EditlineInputConfig{
			Prompt: promptName + "> ",
			Hint:   fmt.Sprintf("enter to send public feedback to @nostrcli via %d relays · shift+enter for newline · ctrl+c to cancel", len(relays)),
		})
		if result.Cancelled {
			return nil
		}
		message = strings.TrimSpace(result.Text)
	}

	if message == "" {
		return fmt.Errorf("feedback message cannot be empty")
	}

	// Build event: mention @nostrcli (NIP-27 nostr: URI + p tag)
	event := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Content:   "nostr:" + nostrcliNpub + " " + message,
		Tags: nostr.Tags{
			{"p", nostrcliHex},
			{"t", "nostrcli"},
		},
	}

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}
	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign event: %w", err)
	}

	if feedbackDryRun {
		if jsonlFlag {
			printJSONL(event)
		} else {
			printJSON(event)
		}
		return nil
	}

	timeout := time.Duration(timeoutFlag) * time.Millisecond

	// Machine-readable output modes
	if rawFlag || jsonFlag || jsonlFlag {
		result, err := ui.PublishEventSilent(npub, event, relays, timeout)
		_ = cache.LogFeedEvent(npub, event)
		if rawFlag {
			printRaw(event)
		} else if jsonlFlag {
			if result != nil {
				printJSONL(result)
			} else {
				printJSONL(event)
			}
		} else {
			if result != nil {
				printJSON(result)
			} else {
				printJSON(event)
			}
		}
		if err != nil && result == nil {
			return err
		}
		return nil
	}

	fmt.Printf("Sending %s note as %s mentioning %s to %d relays\n",
		yellow("PUBLIC"), cyan(promptName), cyan("@nostrcli"), len(relays))
	fmt.Println()
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "Signer:")), npub)
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "Event ID:")), event.ID)
	fmt.Println()

	_, err = ui.PublishEventToRelays(npub, event, relays, timeout)
	if err != nil {
		return err
	}
	_ = cache.LogFeedEvent(npub, event)

	fmt.Println()
	fmt.Println("Thanks for the feedback! 💜")

	return nil
}
