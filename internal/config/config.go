package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// ServerConfig holds settings for the HTTP server
type ServerConfig struct {
	Port int `yaml:"port" validate:"required,min=1,max=65535"`
	// SessionSecret signs session cookies (HMAC). Keep it secret and stable;
	// rotating it invalidates all live sessions.
	SessionSecret string `yaml:"sessionSecret" validate:"required,min=16"`
	// RedirectURI names which of the OAuth client's registered redirect URIs to
	// use for the login flow. Optional: when empty the server picks one by
	// locality. Set it where the default guess is wrong — chiefly a git worktree,
	// whose frontend runs on its own port (see docs/agents/worktrees.md).
	RedirectURI string `yaml:"redirectURI,omitempty" validate:"omitempty,uri"`
}

// Organisation is one drop-in the server runs a rota for (ADR 0011): who may
// organise it, which sheets it reads and writes, and what it is called. It is
// served under /org/<ID>/, and its rows in the database carry ID.
type Organisation struct {
	// ID names the Organisation in its URL and in the database. Lower-case
	// letters, digits and hyphens. It is permanent: calendar subscriptions and
	// emailed links carry it, and they cannot be corrected once sent.
	ID string `yaml:"id" validate:"required"`
	// DisplayName is what the Organisation is called on its pages, in its
	// calendar feeds and in the emails it sends.
	DisplayName string `yaml:"displayName" validate:"required"`
	// OrganiserEmails is the allowlist of Google accounts permitted to act as
	// this Organisation's Organiser, who can make every edit the app offers. At
	// least one is required: without an Organiser nobody can set it up.
	// Compared case-insensitively and re-checked on every request, like
	// RotaEditorEmails.
	OrganiserEmails []string `yaml:"organiserEmails" validate:"required,min=1,dive,email"`
	// RotaEditorEmails is the allowlist of Google accounts permitted to act as
	// this Organisation's Rota Editor, who can change shifts and nothing else.
	// Optional. Someone on both lists is an Organiser.
	RotaEditorEmails     []string `yaml:"rotaEditorEmails,omitempty" validate:"omitempty,dive,email"`
	VolunteerSheetID     string   `yaml:"volunteerSheetID" validate:"required"`
	ServiceVolunteersTab string   `yaml:"serviceVolunteersTab" validate:"required"`
	RotaSheetID          string   `yaml:"rotaSheetID" validate:"required"`
}

// organisationID is what an Organisation id may be: the same rule the
// organisation table's CHECK holds it to, so a bad id fails here, at load,
// rather than at the first insert.
var organisationID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// checkOrganisations enforces what the tags cannot: every id well-formed and
// unique, since two entries with one id would share one URL and one set of rows.
func checkOrganisations(orgs []Organisation) error {
	seen := make(map[string]bool, len(orgs))
	for _, org := range orgs {
		if !organisationID.MatchString(org.ID) {
			return fmt.Errorf("organisation id %q must be lower-case letters, digits and hyphens, starting with a letter or digit", org.ID)
		}
		if seen[org.ID] {
			return fmt.Errorf("organisation id %q is listed twice", org.ID)
		}
		seen[org.ID] = true
	}
	return nil
}

// Organisation returns the configured Organisation with this id.
func (c *Config) Organisation(id string) (*Organisation, bool) {
	for i := range c.Organisations {
		if c.Organisations[i].ID == id {
			return &c.Organisations[i], true
		}
	}
	return nil, false
}

// retiredKeys are keys that configured one drop-in before there could be
// several, and are refused by name rather than warned about (ADR 0011). Each
// now lives on an entry in organisations, and ignoring one would be worse than
// an outage: a server that ignored the allowlists would boot with nobody able
// to sign in, and one that ignored the base path would move every calendar
// feed.
var retiredKeys = []struct{ section, key string }{
	{"", "volunteerSheetID"},
	{"", "serviceVolunteersTab"},
	{"", "rotaSheetID"},
	{"server", "basePath"},
	{"server", "organiserEmails"},
	{"server", "rotaEditorEmails"},
}

// checkRetiredKeys refuses a config still carrying any of retiredKeys.
func checkRetiredKeys(data []byte) error {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil // the real decode reports a malformed document
	}

	var found []string
	for _, retired := range retiredKeys {
		section := doc
		if retired.section != "" {
			nested, ok := doc[retired.section].(map[string]any)
			if !ok {
				continue
			}
			section = nested
		}
		if _, ok := section[retired.key]; ok {
			name := retired.key
			if retired.section != "" {
				name = retired.section + "." + retired.key
			}
			found = append(found, name)
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("config still carries %s, which moved onto each entry in organisations (ADR 0011): move them there, with the id taken from the last segment of the old server.basePath", strings.Join(found, ", "))
	}
	return nil
}

// DevEnv is the only environment the development stubs may run in. It is
// checked by name rather than by "not prod" so a new environment is
// credential-backed unless someone deliberately calls it dev.
const DevEnv = "dev"

// DevModeConfig turns on the credential-free development stubs: the roster is
// read from a CSV file instead of Google Sheets, and login mints a session for
// OrganiserEmail or RotaEditorEmail instead of redirecting to Google. Present only in
// env/dev/drop_in_config.yaml — see checkDevMode. Omit the block entirely for a
// normal, Google-backed server.
type DevModeConfig struct {
	// OrganiserEmail is the account login signs in as. It must be an
	// Organiser of some Organisation, or the session it mints carries the
	// wrong authority.
	OrganiserEmail string `yaml:"organiserEmail" validate:"required,email"`
	// RotaEditorEmail is the account /auth/login?level=rotaEditor signs in as,
	// so the Rota Editor's narrower screens can be driven too. Optional; when
	// set it must be a Rota Editor of some Organisation and an Organiser of
	// none.
	RotaEditorEmail string `yaml:"rotaEditorEmail,omitempty" validate:"omitempty,email"`
	// VolunteersCSV is a CSV export of the volunteer sheet — same header row,
	// same columns — read at startup and on each Organiser's sync. Relative paths
	// resolve from the server's working directory.
	VolunteersCSV string `yaml:"volunteersCSV" validate:"required"`
}

// Config represents the application configuration
type Config struct {
	// Organisations are the drop-ins this server runs, each under /org/<id>/
	// (ADR 0011). At least one.
	Organisations []Organisation `yaml:"organisations" validate:"required,min=1,dive"`
	DatabaseURL   string         `yaml:"databaseURL" validate:"required"`
	Server        *ServerConfig  `yaml:"server,omitempty"`
	DevMode       *DevModeConfig `yaml:"devMode,omitempty"`
	// gmailUserID and gmailSender are gone too (#256). Nothing had read them
	// since mail began to be sent as the signed-in Organiser, through the
	// gmail.send grant they give when they send a round.
	//
	// shiftStartTime, shiftEndTime and shiftTimezone used to live here, and so
	// did maxAllocationFrequency, requiresMale and defaultShiftSize. They are
	// all settings now, edited on the Settings screen (ADR 0006, #128, #129 and
	// #130): when the drop-in runs, what a shift asks for, and which optional
	// allocator rules apply are an Organiser's decisions, not an operator's, and
	// none of them should take a redeploy.
	//
	// The two allocator keys were also two halves of one idea in two places —
	// requiresMale said whether the male-cover rule applied, while the rule's
	// membership of the solver's default list said the same thing again. There
	// is one answer now, and it is a toggle.
	//
	// defaultShiftSize could only ever describe a rota with one Role, since
	// every other Role's count was its ceiling by construction; the default
	// Shape states every Role's Seats.
	//
	// rotaOverrides is gone too (#136), and with it the last domain setting in
	// this file. Everything an override could say has a home an Organiser can reach:
	// whether the drop-in runs on a date is Closed on the Shift (#132), who is
	// pinned to it comes from the Standing Preallocations (#131), and how big it
	// is comes from the default Shape (#129). It was a list of recurrence rules
	// because there was no screen to state any of that on; there is one now.
	//
	// What is left is deployment: which drop-ins to serve and which sheets each
	// reads, which database to talk to, and how to run the server. Those are an
	// operator's, and a redeploy is the right way to change them.
	//
	// A config file still carrying any of the departed keys warns and is
	// otherwise ignored, like any key this build does not know.
}

var validate *validator.Validate

func init() {
	validate = validator.New()
}

// LoadWithEnv loads and validates an environment's configuration, from
// EnvPath(env, ConfigFile) — env="test" reads env/test/drop_in_config.yaml.
func LoadWithEnv(env string) (*Config, error) {
	configPath, err := findEnvFile(env, ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to find config file: %w", err)
	}

	return LoadPathWithEnv(configPath, env)
}

// LoadPathWithEnv loads and validates the configuration at an explicit path,
// applying the environment-dependent rules that LoadFromPath cannot.
//
// It is the whole of what loading a config means, minus finding the file — so
// a config can be vetted before it is anywhere it would be found, which is what
// scripts/deploy-config.sh does before it ships one to the droplet. It opens
// nothing but the file: no database, no Google client.
func LoadPathWithEnv(path, env string) (*Config, error) {
	cfg, err := LoadFromPath(path)
	if err != nil {
		return nil, err
	}

	// The env is only known here, not in LoadFromPath, so this is where the dev
	// stubs can be pinned to the dev environment.
	if err := checkDevMode(cfg, env); err != nil {
		return nil, err
	}

	return cfg, nil
}

// checkDevMode rejects a devMode block outside the dev environment. The stubs
// replace Google identity with a session minted for a configured address, so
// enabling them anywhere real would hand Organiser to anyone who can reach
// /auth/login. Failing the load is deliberate: silently ignoring the block
// would leave an operator believing a gate they set is off when it is on.
func checkDevMode(cfg *Config, env string) error {
	if cfg.DevMode == nil || env == DevEnv {
		return nil
	}
	return fmt.Errorf("devMode is only permitted in the %q environment, not %q: remove the devMode block from the config or run with -env %s", DevEnv, env, DevEnv)
}

// LoadFromPath loads and validates the configuration from a specific path.
//
// A key the struct does not know is a warning, not an error — see unknownKeys
// for why the file is allowed to carry keys this build has never heard of.
func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&cfg); err != nil {
		// A Decoder reports an empty document as io.EOF, where Unmarshal used to
		// return a zero Config. Say what it is: a truncated copy is the likeliest
		// way to get here, and "EOF" does not suggest looking at the file's size.
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config file %s is empty", path)
		}
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := checkRetiredKeys(data); err != nil {
		return nil, err
	}

	for _, unknown := range unknownKeys(data) {
		slog.Warn("config key is not one this version of the app knows; ignoring it",
			"path", path, "key", unknown.Key, "line", unknown.Line)
	}

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// UnknownKey is a key in a config file that this build does not know, and the
// line it sits on. Line is 0 when yaml worded its message in a way this cannot
// read, in which case Key holds that message whole rather than losing it.
type UnknownKey struct {
	Key  string
	Line int
}

// String renders a key the way an operator reads it, not the way yaml phrases
// it: "databaseSheetID (line 59)".
func (u UnknownKey) String() string {
	if u.Line == 0 {
		return u.Key
	}
	return fmt.Sprintf("%s (line %d)", u.Key, u.Line)
}

// UnknownKeys reports the keys in the config file at path that this build does
// not know. Loading warns about them; this is for callers that want to show
// them, chiefly `cli validate-config`.
//
// A file that cannot be read has no keys to report and no error to raise here —
// loading it is what says so.
func UnknownKeys(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	keys := unknownKeys(data)
	described := make([]string, 0, len(keys))
	for _, key := range keys {
		described = append(described, key.String())
	}
	return described
}

// yamlUnknownKey matches the message yaml composes for a key the struct does
// not have — "line 58: field customPreallocations not found in type
// config.Config" — which is the only place the key and its line are exposed.
var yamlUnknownKey = regexp.MustCompile(`^line (\d+): field (\S+) not found in type`)

// unknownKeys reports the keys in a config document that the Config struct does
// not know, each carrying the line number yaml puts in its message.
//
// They are warned about rather than rejected, because a config file outlives any
// one build of the server. The same file is read by whatever version is
// deployed — including the previous one during a rollback, and a branch cut
// before a key existed or after it was removed — so failing the load on a key
// mismatch turns an ordinary version skew into an outage. That is not
// hypothetical: strict decoding took the site down the day it shipped.
//
// The warning is still worth printing, because the other way to get here is a
// key that used to configure something and now configures nothing — which is
// how a live config lost every preallocation to a format change in silence
// (issue #100). A warning names it; only the operator can tell the two apart.
func unknownKeys(data []byte) []UnknownKey {
	var probe Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(&probe)
	if err == nil {
		return nil
	}

	// Anything that is not a TypeError already failed the real decode above, so
	// there is nothing here to report on top of it.
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return nil
	}

	var unknown []UnknownKey
	for _, message := range typeErr.Errors {
		// A TypeError also carries mismatches such as "cannot unmarshal !!str into
		// int". Those failed the real decode, so only the unknown-key messages are
		// this function's business.
		if !strings.Contains(message, "not found in type") {
			continue
		}

		match := yamlUnknownKey.FindStringSubmatch(message)
		if match == nil {
			unknown = append(unknown, UnknownKey{Key: message})
			continue
		}
		line, err := strconv.Atoi(match[1])
		if err != nil {
			unknown = append(unknown, UnknownKey{Key: message})
			continue
		}
		unknown = append(unknown, UnknownKey{Key: match[2], Line: line})
	}

	return unknown
}

// Validate checks the configuration struct against its field tags, plus the
// rules a tag cannot express: every Organisation id well-formed and unique.
//
// It used to parse the rrule on every rota override as well, but overrides went
// in #136 along with the rest of the domain settings. What remains is
// deployment keys.
//
// It deliberately touches nothing but the config it was handed:
// scripts/deploy-config.sh runs it from a laptop against a production config,
// which is only safe because it connects to nothing.
func Validate(cfg *Config) error {
	if err := validate.Struct(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return checkOrganisations(cfg.Organisations)
}
