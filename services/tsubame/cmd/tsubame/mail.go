package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/envelope"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/jobs"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail/gmail"
	kagamisource "github.com/0xHoaxen/shogun/services/tsubame/internal/transport/kagami"
)

const (
	masterKeyEnv    = "TSUBAME_TOKEN_MASTER_KEY"
	masterKeyIDEnv  = "TSUBAME_TOKEN_KEY_ID"
	masterKeyIDDflt = "tsubame-1"

	gmailClientIDEnv     = "TSUBAME_GMAIL_CLIENT_ID"
	gmailClientSecretEnv = "TSUBAME_GMAIL_CLIENT_SECRET"
	gmailRedirectURLEnv  = "TSUBAME_GMAIL_REDIRECT_URL"

	hankoKeysEnv = "TSUBAME_HANKO_VERIFY_KEYS"

	sorobanAddrEnv = "SOROBAN_ADDR"
	kagamiAddrEnv  = "KAGAMI_ADDR"
	apiKeyEnv      = "ANTHROPIC_API_KEY"
)

// loadKeyring reads the master key that wraps the data key of every stored
// token: 32 bytes, base64 (make one with `openssl rand -base64 32`). It is
// required, so a tsubame without it does not start. The key never appears in
// an error.
func loadKeyring(lookup config.LookupFunc) (*envelope.Keyring, error) {
	raw, err := config.Required(lookup, masterKeyEnv)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not base64", masterKeyEnv)
	}
	if len(key) != envelope.KeySize {
		return nil, fmt.Errorf("%s must decode to %d bytes, got %d", masterKeyEnv, envelope.KeySize, len(key))
	}
	id := config.String(lookup, masterKeyIDEnv, masterKeyIDDflt)
	return envelope.NewKeyring(id, map[string][]byte{id: key})
}

// newGmail builds the Gmail client from the Google client id, secret and
// redirect URL, which are required.
func newGmail(lookup config.LookupFunc) (*gmail.Client, error) {
	var cfg gmail.Config
	for _, f := range []struct {
		env string
		dst *string
	}{
		{gmailClientIDEnv, &cfg.ClientID},
		{gmailClientSecretEnv, &cfg.ClientSecret},
		{gmailRedirectURLEnv, &cfg.RedirectURL},
	} {
		v, err := config.Required(lookup, f.env)
		if err != nil {
			return nil, err
		}
		*f.dst = v
	}
	return gmail.New(cfg)
}

// loadHankoKeys reads the public keys that may have signed an approval:
// comma-separated "key-id=base64" pairs, each a 32-byte Ed25519 public key. More
// than one is allowed so fude's key can be rotated. They are public, but they
// are required: without one nothing can be sent.
func loadHankoKeys(lookup config.LookupFunc) (map[string]ed25519.PublicKey, error) {
	raw, err := config.Required(lookup, hankoKeysEnv)
	if err != nil {
		return nil, err
	}
	keys := map[string]ed25519.PublicKey{}
	for _, pair := range strings.Split(raw, ",") {
		id, b64, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok || id == "" {
			return nil, fmt.Errorf("%s must be key-id=base64 pairs separated by commas", hankoKeysEnv)
		}
		key, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%s: key %q is not a base64 %d-byte Ed25519 public key", hankoKeysEnv, id, ed25519.PublicKeySize)
		}
		keys[id] = key
	}
	return keys, nil
}

// mailServices is what the mail use cases need, built once.
type mailServices struct {
	connector *app.Connector
	sender    *app.Sender
	setup     jobs.Setup
	close     func()
}

// newMailServices wires account connection, the scheduled sync and message
// classification. Classification calls kagami to link mail and soroban to meter
// the model; with no ANTHROPIC_API_KEY (allowed outside production) the rules
// still run and only mail they are unsure about fails to classify.
func newMailServices(
	ctx context.Context,
	lookup config.LookupFunc,
	cfg config.Base,
	pool *pgxpool.Pool,
	keys *envelope.Keyring,
	signer grpcclient.Signer,
	log *slog.Logger,
) (*mailServices, error) {
	client, err := newGmail(lookup)
	if err != nil {
		return nil, err
	}
	sorobanAddr, err := config.Required(lookup, sorobanAddrEnv)
	if err != nil {
		return nil, err
	}
	kagamiAddr, err := config.Required(lookup, kagamiAddrEnv)
	if err != nil {
		return nil, err
	}
	apiKey := config.String(lookup, apiKeyEnv, "")
	if apiKey == "" && cfg.IsProduction() {
		return nil, errNoAPIKey
	}
	queue, err := jobs.NewRiverQueue(pool)
	if err != nil {
		return nil, err
	}

	sorobanConn, err := grpcclient.Dial(ctx, sorobanAddr, grpcclient.WithSigner(signer))
	if err != nil {
		return nil, fmt.Errorf("dial soroban: %w", err)
	}
	kagamiConn, err := grpcclient.Dial(ctx, kagamiAddr, grpcclient.WithSigner(signer))
	if err != nil {
		_ = sorobanConn.Close()
		return nil, fmt.Errorf("dial kagami: %w", err)
	}
	closeConns := func() { _ = sorobanConn.Close(); _ = kagamiConn.Close() }

	completer, err := newCompleter(lookup, apiKey, sorobanv1.NewSorobanServiceClient(sorobanConn), log)
	if err != nil {
		closeConns()
		return nil, err
	}
	hankoKeys, err := loadHankoKeys(lookup)
	if err != nil {
		closeConns()
		return nil, err
	}
	accounts := app.NewAccounts(pool, keys)
	sender, err := app.NewSender(pool, accounts, client, hankoKeys, log, nil)
	if err != nil {
		closeConns()
		return nil, err
	}
	syncer := app.NewSyncer(pool, accounts, client, queue, log, nil)
	classifier := app.NewClassifier(pool, completer, kagamisource.New(kagamiv1.NewKagamiServiceClient(kagamiConn)))
	return &mailServices{
		connector: app.NewConnector(accounts, client, client, keys, log, nil),
		sender:    sender,
		setup:     jobs.NewSetup(syncer, classifier, sender, log),
		close:     closeConns,
	}, nil
}

// errNoAPIKey is what classification fails with while no API key is set.
var errNoAPIKey = errors.New(apiKeyEnv + " is not set")

// newCompleter returns the metered Claude client, or one that fails every call
// when there is no API key.
func newCompleter(lookup config.LookupFunc, apiKey string, meter llm.Meter, log *slog.Logger) (app.Completer, error) {
	if apiKey == "" {
		log.Warn("model classification is off: " + errNoAPIKey.Error())
		return noKeyCompleter{}, nil
	}
	cfg, err := llm.NewConfig(serviceName, lookup)
	if err != nil {
		return nil, err
	}
	return llm.New(cfg, llm.NewAnthropicAPI(apiKey), meter, llm.WithLogger(log))
}

type noKeyCompleter struct{}

func (noKeyCompleter) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return llm.Response{}, errNoAPIKey
}
