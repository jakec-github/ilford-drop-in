package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// InteractiveCmd creates the interactive command
func InteractiveCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interactive",
		Short: "Start an interactive session (authenticate once, run multiple commands)",
		Long: `Start an interactive session where you can run multiple commands without re-authenticating.
The session will keep running until you type 'exit' or 'quit'.

Type 'help' to see available commands.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("\n🚀 Starting interactive session...")
			fmt.Println("Type 'help' for available commands, 'exit' or 'quit' to leave")

			// Get all sibling commands (excluding interactive itself)
			rootCmd := cmd.Parent()
			commands := make(map[string]*cobra.Command)
			for _, subCmd := range rootCmd.Commands() {
				if subCmd.Name() != "interactive" && subCmd.Name() != "completion" && subCmd.Name() != "help" {
					commands[subCmd.Name()] = subCmd
				}
			}

			scanner := bufio.NewScanner(os.Stdin)

			for {
				fmt.Print("> ")

				if !scanner.Scan() {
					break
				}

				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}

				// Parse command (respecting quotes)
				parts, err := parseCommandLine(line)
				if err != nil {
					fmt.Printf("❌ Error parsing command: %v\n\n", err)
					continue
				}
				if len(parts) == 0 {
					continue
				}
				cmdName := parts[0]
				cmdArgs := parts[1:]

				// Handle exit
				if cmdName == "exit" || cmdName == "quit" {
					fmt.Println("👋 Goodbye!")
					return nil
				}

				// Handle help
				if cmdName == "help" {
					printInteractiveHelp(commands)
					continue
				}

				// Execute command via Cobra
				targetCmd, exists := commands[cmdName]
				if !exists {
					fmt.Printf("❌ Unknown command: %s (type 'help' for available commands)\n\n", cmdName)
					continue
				}

				// Reset command flags and args
				targetCmd.Flags().VisitAll(func(flag *pflag.Flag) {
					flag.Changed = false
					flag.Value.Set(flag.DefValue)
				})

				// Execute the command's RunE directly, bypassing the full Execute() flow
				// This avoids re-running PersistentPreRunE which would call initApp() again
				if err := targetCmd.ParseFlags(cmdArgs); err != nil {
					fmt.Printf("❌ Error parsing flags: %v\n\n", err)
					continue
				}

				// Get non-flag args after parsing flags
				cmdArgs = targetCmd.Flags().Args()

				// Validate args
				if err := targetCmd.Args(targetCmd, cmdArgs); err != nil {
					fmt.Printf("❌ Error: %v\n\n", err)
					continue
				}

				// Execute the RunE function directly
				if targetCmd.RunE != nil {
					if err := targetCmd.RunE(targetCmd, cmdArgs); err != nil {
						fmt.Printf("❌ Error: %v\n\n", err)
					}
				} else if targetCmd.Run != nil {
					targetCmd.Run(targetCmd, cmdArgs)
				}
			}

			if err := scanner.Err(); err != nil {
				return fmt.Errorf("error reading input: %w", err)
			}

			return nil
		},
	}

	return cmd
}

func printInteractiveHelp(commands map[string]*cobra.Command) {
	fmt.Println("\nAvailable commands:")

	// Get command names and sort them
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}

	// Print each command with its short description
	for _, name := range names {
		cmd := commands[name]
		fmt.Printf("  %-30s %s\n", cmd.Use, cmd.Short)
	}

	fmt.Println("\n  help                           Show this help message")
	fmt.Println("  exit, quit                     Exit the interactive session")
}

// parseCommandLine splits a command line into arguments, respecting quoted strings
// Supports both single and double quotes
func parseCommandLine(line string) ([]string, error) {
	var args []string
	var current strings.Builder
	var inQuote rune // 0 if not in quote, '"' or '\'' if in quote

	for i, r := range line {
		switch {
		case inQuote != 0:
			// Inside a quote
			if r == inQuote {
				// End quote
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			// Start quote
			inQuote = r
		case unicode.IsSpace(r):
			// Whitespace outside quotes - end current argument
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			// Regular character
			current.WriteRune(r)
		}

		// Check for unclosed quote at end
		if i == len(line)-1 && inQuote != 0 {
			return nil, fmt.Errorf("unclosed quote: %c", inQuote)
		}
	}

	// Add final argument if present
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}
