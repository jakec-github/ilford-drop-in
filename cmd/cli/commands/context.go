package commands

import (
	"context"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/gmailclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// AppContext holds the application dependencies shared across all commands
type AppContext struct {
	Cfg          *config.Config
	SheetsClient *sheetsclient.Client
	FormsClient  *formsclient.Client
	GmailClient  *gmailclient.Client
	Database     db.Database
	Logger       *zap.Logger
	Ctx          context.Context
}
