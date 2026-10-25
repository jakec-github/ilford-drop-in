package commands

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// AppContext holds the application dependencies shared across all commands
type AppContext struct {
	Cfg          *config.Config
	SheetsClient *sheetsclient.Client
	Database     *db.DB
	Logger       *zap.Logger
	Ctx          context.Context
	UserEmail    string
}

// Organisation picks the Organisation a command acts on: the one named, or the
// only one configured when none is. Several configured and none named is
// refused rather than guessed, since every command reads one drop-in's rota.
func (a *AppContext) Organisation(id string) (*config.Organisation, error) {
	if id == "" {
		if len(a.Cfg.Organisations) != 1 {
			return nil, fmt.Errorf("the config lists %d organisations: name one with --org", len(a.Cfg.Organisations))
		}
		return &a.Cfg.Organisations[0], nil
	}
	org, ok := a.Cfg.Organisation(id)
	if !ok {
		return nil, fmt.Errorf("no organisation %q in the config", id)
	}
	return org, nil
}
