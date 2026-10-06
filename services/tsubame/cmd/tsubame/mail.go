package main

import (
	"encoding/base64"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/envelope"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail/gmail"
)

const (
	masterKeyEnv    = "TSUBAME_TOKEN_MASTER_KEY"
	masterKeyIDEnv  = "TSUBAME_TOKEN_KEY_ID"
	masterKeyIDDflt = "tsubame-1"

	gmailClientIDEnv     = "TSUBAME_GMAIL_CLIENT_ID"
	gmailClientSecretEnv = "TSUBAME_GMAIL_CLIENT_SECRET"
	gmailRedirectURLEnv  = "TSUBAME_GMAIL_REDIRECT_URL"
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

// newConnector wires account connection to Gmail. The Google client id,
// secret and redirect URL are required.
func newConnector(lookup config.LookupFunc, pool *pgxpool.Pool, keys *envelope.Keyring, log *slog.Logger) (*app.Connector, error) {
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
	client, err := gmail.New(cfg)
	if err != nil {
		return nil, err
	}
	return app.NewConnector(app.NewAccounts(pool, keys), client, client, keys, log, nil), nil
}
