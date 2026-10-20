package grpc_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/envelope"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
)

// appCtx is a context for calling the service directly, as the gRPC interceptor
// would have built it for owner.
func appCtx(owner string) context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{OwnerID: owner, RequestID: "test"})
}

const secretKey = "sk-test-0123456789-SECRETWXYZ"

func goodProvider() *sorobanv1.SetProviderRequest {
	return &sorobanv1.SetProviderRequest{BaseUrl: "https://llm.example.com/v1", Model: "claude-sonnet-5-5", ApiKey: secretKey}
}

func (h *harness) setProvider(t *testing.T, owner string, req *sorobanv1.SetProviderRequest) *sorobanv1.Provider {
	t.Helper()
	res, err := h.client.SetProvider(h.ctxFor(t, owner), req)
	if err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	return res.GetProvider()
}

func TestSetProviderStoresItAndShowsEverythingButTheKey(t *testing.T) {
	// Arrange
	h := newBareHarness(t)

	// Act
	set := h.setProvider(t, h.owner, goodProvider())
	got, err := h.client.GetProvider(h.ctx(t), &sorobanv1.GetProviderRequest{})
	// Assert
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	for _, p := range []*sorobanv1.Provider{set, got.GetProvider()} {
		if p.GetBaseUrl() != "https://llm.example.com/v1" || p.GetModel() != "claude-sonnet-5-5" || p.GetKeyLast4() != "WXYZ" || p.GetVersion() != 1 {
			t.Fatalf("provider = %v", p)
		}
		if strings.Contains(p.String(), secretKey) {
			t.Fatalf("the response carries the key: %v", p)
		}
	}
}

func TestTheKeyIsSealedAtRestAndOpensOnlyForItsOwner(t *testing.T) {
	// Arrange
	h := newBareHarness(t)
	h.setProvider(t, h.owner, goodProvider())
	var ciphertext []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT api_key_ciphertext FROM providers WHERE owner_id = $1`, h.owner).Scan(&ciphertext); err != nil {
		t.Fatalf("read row: %v", err)
	}

	// Act
	secret, openErr := h.svc.ProviderSecret(appCtx(h.owner))
	thief := uuid.New()
	_, copyErr := h.pool.Exec(context.Background(), `INSERT INTO providers (owner_id, base_url, model, api_key_ciphertext, key_id, key_last4)
		SELECT $1, base_url, model, api_key_ciphertext, key_id, key_last4 FROM providers WHERE owner_id = $2`, thief, h.owner)
	_, stolenErr := h.svc.ProviderSecret(appCtx(thief.String()))

	// Assert
	if bytes.Contains(ciphertext, []byte(secretKey)) || bytes.Contains(ciphertext, []byte("SECRETWXYZ")) {
		t.Fatal("the key is stored in the clear")
	}
	if openErr != nil || secret.APIKey != secretKey || secret.BaseURL != "https://llm.example.com/v1" || secret.Model != "claude-sonnet-5-5" {
		t.Fatalf("secret = %+v, %v; want the stored provider with its key", secret, openErr)
	}
	if copyErr != nil || stolenErr == nil {
		t.Fatalf("copy %v, open as another owner %v; want a copied key to stay sealed", copyErr, stolenErr)
	}
}

func TestUpdatingWithoutAKeyKeepsTheStoredOneAndANewKeyReplacesIt(t *testing.T) {
	// Arrange
	h := newBareHarness(t)
	first := h.setProvider(t, h.owner, goodProvider())

	// Act
	renamed := h.setProvider(t, h.owner, &sorobanv1.SetProviderRequest{
		BaseUrl: "https://other.example.com", Model: "claude-opus-5-5", Version: first.GetVersion(),
	})
	kept, keptErr := h.svc.ProviderSecret(appCtx(h.owner))
	rekeyed := h.setProvider(t, h.owner, &sorobanv1.SetProviderRequest{
		BaseUrl: "https://other.example.com", Model: "claude-opus-5-5", ApiKey: "sk-new-key-ABCD1234", Version: renamed.GetVersion(),
	})
	replaced, replacedErr := h.svc.ProviderSecret(appCtx(h.owner))

	// Assert
	if keptErr != nil || kept.APIKey != secretKey || kept.Model != "claude-opus-5-5" || renamed.GetVersion() != 2 || renamed.GetKeyLast4() != "WXYZ" {
		t.Fatalf("after a rename: %+v, %v, provider %v; want the old key kept and version 2", kept, keptErr, renamed)
	}
	if replacedErr != nil || replaced.APIKey != "sk-new-key-ABCD1234" || rekeyed.GetKeyLast4() != "1234" || rekeyed.GetVersion() != 3 {
		t.Fatalf("after a new key: %+v, %v, provider %v", replaced, replacedErr, rekeyed)
	}
}

func TestSettingTheFirstProviderNeedsAKeyAndAStaleVersionIsRefused(t *testing.T) {
	h := newBareHarness(t)
	noKey := goodProvider()
	noKey.ApiKey = ""
	_, missing := h.client.SetProvider(h.ctx(t), noKey)
	first := h.setProvider(t, h.owner, goodProvider())

	_, again := h.client.SetProvider(h.ctx(t), goodProvider()) // version 0 when one exists
	stale := goodProvider()
	stale.Version = first.GetVersion() + 5
	_, wrong := h.client.SetProvider(h.ctx(t), stale)

	requireStatus(t, missing, codes.InvalidArgument, "API_KEY_REQUIRED")
	requireStatus(t, again, codes.Aborted, "VERSION_CONFLICT")
	requireStatus(t, wrong, codes.Aborted, "VERSION_CONFLICT")
}

func TestSetProviderRefusesWhatIsNotAProvider(t *testing.T) {
	one, huge := int64(1), int64(2_000_000_000_000)
	tests := []struct {
		name   string
		edit   func(*sorobanv1.SetProviderRequest)
		reason string
	}{
		{"plain http", func(r *sorobanv1.SetProviderRequest) { r.BaseUrl = "http://llm.example.com" }, "INVALID_PROVIDER_URL"},
		{"loopback", func(r *sorobanv1.SetProviderRequest) { r.BaseUrl = "https://127.0.0.1" }, "INVALID_PROVIDER_URL"},
		{"cloud metadata", func(r *sorobanv1.SetProviderRequest) { r.BaseUrl = "https://169.254.169.254/latest" }, "INVALID_PROVIDER_URL"},
		{"an internal name", func(r *sorobanv1.SetProviderRequest) { r.BaseUrl = "https://soroban.shogun.svc.cluster.local" }, "INVALID_PROVIDER_URL"},
		{"credentials in the address", func(r *sorobanv1.SetProviderRequest) { r.BaseUrl = "https://a:b@llm.example.com" }, "INVALID_PROVIDER_URL"},
		{"a bad model", func(r *sorobanv1.SetProviderRequest) { r.Model = "has space" }, "INVALID_PROVIDER_MODEL"},
		{"a short key", func(r *sorobanv1.SetProviderRequest) { r.ApiKey = "short" }, "INVALID_API_KEY"},
		{"a key with a space", func(r *sorobanv1.SetProviderRequest) { r.ApiKey = "sk with a space 123" }, "INVALID_API_KEY"},
		{"half a price", func(r *sorobanv1.SetProviderRequest) { r.InputMicrosPerMtok = &one }, "INVALID_PRICE"},
		{"an absurd price", func(r *sorobanv1.SetProviderRequest) { r.InputMicrosPerMtok, r.OutputMicrosPerMtok = &one, &huge }, "INVALID_PRICE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newBareHarness(t)
			req := goodProvider()
			tt.edit(req)

			_, err := h.client.SetProvider(h.ctx(t), req)

			requireStatus(t, err, codes.InvalidArgument, tt.reason)
			if got, _ := h.client.GetProvider(h.ctx(t), &sorobanv1.GetProviderRequest{}); got.GetProvider() != nil {
				t.Fatal("a refused provider was stored")
			}
		})
	}
}

func TestAPriceForAnUnpricedModelIsKeptWithTheProvider(t *testing.T) {
	h := newBareHarness(t)
	in, out := int64(3_000_000), int64(15_000_000)
	req := goodProvider()
	req.InputMicrosPerMtok, req.OutputMicrosPerMtok = &in, &out

	set := h.setProvider(t, h.owner, req)
	secret, err := h.svc.ProviderSecret(appCtx(h.owner))

	if set.InputMicrosPerMtok == nil || *set.InputMicrosPerMtok != in || *set.OutputMicrosPerMtok != out {
		t.Fatalf("provider = %v, want the prices back", set)
	}
	if err != nil || secret.InputMicrosPerMTok == nil || *secret.InputMicrosPerMTok != in || *secret.OutputMicrosPerMTok != out {
		t.Fatalf("secret = %+v, %v", secret, err)
	}
}

func TestOwnersNeverSeeOrRemoveEachOthersProvider(t *testing.T) {
	// Arrange
	h := newBareHarness(t)
	other := uuid.NewString()
	h.setProvider(t, h.owner, goodProvider())

	// Act
	theirs, getErr := h.client.GetProvider(h.ctxFor(t, other), &sorobanv1.GetProviderRequest{})
	_, delErr := h.client.DeleteProvider(h.ctxFor(t, other), &sorobanv1.DeleteProviderRequest{})
	mine, mineErr := h.client.GetProvider(h.ctx(t), &sorobanv1.GetProviderRequest{})

	// Assert
	if getErr != nil || theirs.GetProvider() != nil {
		t.Fatalf("another owner got %v, %v", theirs, getErr)
	}
	if delErr != nil || mineErr != nil || mine.GetProvider() == nil {
		t.Fatalf("delete by another owner: %v; provider after: %v, %v", delErr, mine, mineErr)
	}
}

func TestDeleteProviderRemovesItAndIsHarmlessTwice(t *testing.T) {
	h := newBareHarness(t)
	h.setProvider(t, h.owner, goodProvider())

	_, first := h.client.DeleteProvider(h.ctx(t), &sorobanv1.DeleteProviderRequest{})
	_, again := h.client.DeleteProvider(h.ctx(t), &sorobanv1.DeleteProviderRequest{})
	got, getErr := h.client.GetProvider(h.ctx(t), &sorobanv1.GetProviderRequest{})
	_, secretErr := h.svc.ProviderSecret(appCtx(h.owner))

	if first != nil || again != nil || getErr != nil || got.GetProvider() != nil {
		t.Fatalf("delete %v and %v, get %v %v", first, again, got, getErr)
	}
	if secretErr == nil {
		t.Fatal("the key is still available after the provider was deleted")
	}
}

func TestProviderCallsNeedAnIdentity(t *testing.T) {
	h := newBareHarness(t)

	_, err := h.client.GetProvider(context.Background(), &sorobanv1.GetProviderRequest{})

	if err == nil {
		t.Fatal("GetProvider answered a caller with no identity")
	}
}

func TestWithoutAKeyringProvidersCannotBeStored(t *testing.T) {
	h := newBareHarness(t)
	noKeys := app.NewService(h.pool, nil)

	_, err := noKeys.SetProvider(appCtx(h.owner), app.ProviderInput{BaseURL: "https://llm.example.com", Model: "m", APIKey: secretKey})

	if err == nil || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("err = %v, want a refusal that does not repeat the key", err)
	}
	var stored int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM providers`).Scan(&stored)
	if stored != 0 {
		t.Fatalf("%d providers stored without a keyring", stored)
	}
}

func TestInsecureProvidersAreAllowedOnlyWhenTheServiceIsToldTo(t *testing.T) {
	h := newBareHarness(t)
	keys, _ := envelope.NewKeyring("k", map[string][]byte{"k": bytes.Repeat([]byte("m"), envelope.KeySize)})
	local := app.NewService(h.pool, nil, app.WithKeyring(keys), app.WithInsecureProviders())
	in := app.ProviderInput{BaseURL: "http://localhost:9999", Model: "fake-model", APIKey: secretKey}

	_, refused := h.svc.SetProvider(appCtx(h.owner), in)
	_, allowed := local.SetProvider(appCtx(h.owner), in)

	if refused == nil || allowed != nil {
		t.Fatalf("production-mode service: %v; local-mode service: %v", refused, allowed)
	}
}
