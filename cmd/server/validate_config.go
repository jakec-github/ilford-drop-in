package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

// validateConfig checks a config file and prints what it says, without starting
// anything. main runs it for -validate-config and exits before run(), which is
// the whole point: run() connects to the database the config names, so a check
// that went through it would reach production every time someone checked a
// production config from a laptop. scripts/deploy-config.sh runs this before it
// ships one to the droplet.
//
// The environment is required because some rules depend on it — chiefly that a
// devMode block is only permitted under "dev".
//
// The summary it prints is not decoration. The failure this exists for was a
// config whose pins were silently dropped by a format change, and
// "valid" alone would have printed for that file too. What catches it now is
// the ignored-keys line: the domain settings this used to count are rows in the
// database (ADR 0006), and a key that has stopped configuring anything shows up
// as one this build does not know.
func validateConfig(out io.Writer, path, env string) error {
	if env == "" {
		return fmt.Errorf("required flag \"env\" not set")
	}

	cfg, err := config.LoadPathWithEnv(path, env)
	if err != nil {
		return fmt.Errorf("%s is not a valid %s config: %w", path, env, err)
	}
	if err := requireServer(cfg, path); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s is a valid %s config\n", path, env)
	// No domain settings are summarised here any more, because none are in the
	// file. When a shift runs, what it asks for, who is pinned to it and which
	// allocator rules apply are all Rota Defaults, set on the Settings screen and
	// read from the database (ADR 0006) — so this check, which opens the file and
	// nothing else, cannot report them and should not pretend to. What is left is
	// the deployment.
	for _, org := range cfg.Organisations {
		fmt.Fprintf(out, "  org:        %s (%s) — %s, %s\n", org.ID, org.DisplayName,
			plural(len(org.OrganiserEmails), "organiser"),
			plural(len(org.RotaEditorEmails), "rota editor"))
	}
	fmt.Fprintf(out, "  server:     port %d\n", cfg.Server.Port)
	if cfg.DevMode != nil {
		fmt.Fprintf(out, "  devMode:    ON — roster from %s, login as %s\n",
			cfg.DevMode.VolunteersCSV, cfg.DevMode.OrganiserEmail)
	}
	// Ignored keys do not stop the server starting, so this line is the only
	// place an operator finds out that a section of their file configures
	// nothing — which is either a key kept on purpose for another build, or the
	// silent drop this check exists to catch.
	if unknown := config.UnknownKeys(path); len(unknown) > 0 {
		fmt.Fprintf(out, "  ignored:    %s — %s\n",
			plural(len(unknown), "unknown key"), strings.Join(unknown, ", "))
	}

	return nil
}

// requireServer refuses a config with no server block. The config package
// accepts one, but this binary is the only thing left that reads the file, and
// it cannot start without a port.
func requireServer(cfg *config.Config, path string) error {
	if cfg.Server == nil {
		return fmt.Errorf("server config missing: add server.port to %s", path)
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
