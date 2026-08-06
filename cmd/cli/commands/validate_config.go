package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

// ValidateConfigCmd checks a config file and prints what it says, without
// starting anything.
//
// It takes no AppContext, and it shadows the root's PersistentPreRunE so
// initApp never runs (cmd/cli/main.go). That is the whole point: initApp
// authenticates with Google and connects to the database the config names, so a
// validate command built the ordinary way would reach production every time
// someone checked a production config from a laptop.
//
// The summary it prints is not decoration. The failure this exists for was a
// config whose preallocations were silently dropped by a format change, so the
// counts an operator can compare against what they meant to write are the
// useful output — "valid" alone would have printed for the broken file too.
func ValidateConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate-config <path>",
		Short: "Validate a config file without connecting to anything",
		Long: `Validate a config file and print a summary of what it configures.

Reads only the named file: no database connection, no Google credentials, no
token cache. Safe to run against a production config from a laptop, which is
what scripts/deploy-config.sh does before it ships one to the droplet.

The environment is required because some rules depend on it — chiefly that a
devMode block is only permitted under "dev".`,
		Args: cobra.ExactArgs(1),
		// An invalid config is this command's ordinary result, not a misuse of the
		// command. Dumping the flag list underneath buries the line that says which
		// key is wrong.
		SilenceUsage: true,
		// Cobra runs the nearest PersistentPreRunE it finds walking up from the
		// command, so defining one here — even doing nothing — is what keeps the
		// root's initApp out of this command's path.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			env, _ := cmd.Flags().GetString("env")
			if env == "" {
				return fmt.Errorf("required flag \"env\" not set")
			}

			path := args[0]
			cfg, err := config.LoadPathWithEnv(path, env)
			if err != nil {
				return fmt.Errorf("%s is not a valid %s config: %w", path, env, err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s is a valid %s config\n", path, env)
			// Who is pinned is not here either: Config Preallocations were
			// deleted in issue #131, and the Standing Preallocations that
			// replaced them are Rota Defaults, in the database.
			fmt.Fprintf(out, "  overrides:  %s\n", plural(len(cfg.RotaOverrides), "rota override"))
			// When a shift runs and what it asks for are not here any more: they
			// are Rota Defaults, set on the Settings screen and read from the
			// database (ADR 0006), so this command — which opens the file and
			// nothing else — cannot report them and should not pretend to.
			fmt.Fprintf(out, "  server:     %s\n", describeServer(cfg))
			if cfg.DevMode != nil {
				fmt.Fprintf(out, "  devMode:    ON — roster from %s, login as %s\n",
					cfg.DevMode.VolunteersCSV, cfg.DevMode.AdminEmail)
			}
			// Ignored keys do not stop the server starting, so this line is the only
			// place an operator finds out that a section of their file configures
			// nothing — which is either a key kept on purpose for another build, or
			// the silent drop this command exists to catch.
			if unknown := config.UnknownKeys(path); len(unknown) > 0 {
				fmt.Fprintf(out, "  ignored:    %s — %s\n",
					plural(len(unknown), "unknown key"), strings.Join(unknown, ", "))
			}

			return nil
		},
	}
}

func describeServer(cfg *config.Config) string {
	if cfg.Server == nil {
		return "not configured — the CLI runs, the web server does not"
	}
	return fmt.Sprintf("port %d, %s", cfg.Server.Port, plural(len(cfg.Server.AdminEmails), "admin email"))
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
