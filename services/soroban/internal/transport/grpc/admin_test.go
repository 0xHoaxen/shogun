package grpc_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

func (h *harness) listBudgets(t *testing.T) []*sorobanv1.Budget {
	t.Helper()
	res, err := h.client.ListBudgets(h.ctx(t), &sorobanv1.ListBudgetsRequest{})
	if err != nil {
		t.Fatalf("ListBudgets: %v", err)
	}
	return res.GetBudgets()
}

func findBudget(t *testing.T, budgets []*sorobanv1.Budget, scope sorobanv1.ScopeType, value string, period sorobanv1.BudgetPeriod) *sorobanv1.Budget {
	t.Helper()
	for _, b := range budgets {
		if b.GetScopeType() == scope && b.GetScopeValue() == value && b.GetPeriod() == period {
			return b
		}
	}
	t.Fatalf("no %s %q %s budget in %d budgets", scope, value, period, len(budgets))
	return nil
}

func TestListBudgetsCreatesDefaultsAndShowsCurrentSpend(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.reserve(t, 1000) // holds 4000 micros against the global budget

	// Act
	budgets := h.listBudgets(t)

	// Assert
	if len(budgets) != 11 {
		t.Fatalf("got %d budgets, want the 11 defaults", len(budgets))
	}
	global := findBudget(t, budgets, sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, "", sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY)
	if global.GetLimitMicros() != 20_000_000 || global.GetMode() != sorobanv1.BudgetMode_BUDGET_MODE_HARD {
		t.Errorf("global budget = %v, want $20 hard", global)
	}
	if global.GetReservedMicros() != 4000 {
		t.Errorf("global reserved = %d, want 4000", global.GetReservedMicros())
	}
	if got, want := global.GetResetsAt().AsTime(), time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("global resets at %s, want %s", got, want)
	}
	idle := findBudget(t, budgets, sorobanv1.ScopeType_SCOPE_TYPE_SERVICE, "katana", sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY)
	if idle.GetReservedMicros() != 0 || idle.GetSpentMicros() != 0 {
		t.Errorf("an unused budget shows spend: %v", idle)
	}
}

func TestSetBudgetCreatesThenUpdatesWithVersionCheck(t *testing.T) {
	// Arrange
	h := newHarness(t)
	create := &sorobanv1.SetBudgetRequest{
		ScopeType: sorobanv1.ScopeType_SCOPE_TYPE_FEATURE, ScopeValue: "katana.extra",
		Period: sorobanv1.BudgetPeriod_BUDGET_PERIOD_DAILY, LimitMicros: 500_000,
		Mode: sorobanv1.BudgetMode_BUDGET_MODE_SOFT, Enabled: true,
	}

	// Act
	created, err := h.client.SetBudget(h.ctx(t), create)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	b := created.GetBudget()
	updated, err := h.client.SetBudget(h.ctx(t), &sorobanv1.SetBudgetRequest{
		Id: b.GetId(), Version: b.GetVersion(), LimitMicros: 900_000,
		Mode: sorobanv1.BudgetMode_BUDGET_MODE_HARD, Enabled: true,
		ScopeType: b.GetScopeType(), Period: b.GetPeriod(), ScopeValue: b.GetScopeValue(),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	_, staleErr := h.client.SetBudget(h.ctx(t), &sorobanv1.SetBudgetRequest{
		Id: b.GetId(), Version: b.GetVersion(), LimitMicros: 1,
		Mode: sorobanv1.BudgetMode_BUDGET_MODE_HARD, Enabled: true,
		ScopeType: b.GetScopeType(), Period: b.GetPeriod(), ScopeValue: b.GetScopeValue(),
	})
	_, duplicateErr := h.client.SetBudget(h.ctx(t), create)

	// Assert
	if len(b.GetThresholds()) != 3 || b.GetVersion() != 1 {
		t.Errorf("created budget = %v, want default thresholds and version 1", b)
	}
	if u := updated.GetBudget(); u.GetLimitMicros() != 900_000 || u.GetMode() != sorobanv1.BudgetMode_BUDGET_MODE_HARD || u.GetVersion() != 2 {
		t.Errorf("updated budget = %v, want $0.90 hard at version 2", u)
	}
	requireStatus(t, staleErr, codes.Aborted, "VERSION_CONFLICT")
	requireStatus(t, duplicateErr, codes.AlreadyExists, "ALREADY_EXISTS")
}

func TestSetBudgetOfAnotherOwnerIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.seedDefaults(t)
	global := findBudget(t, h.listBudgets(t), sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, "", sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY)

	_, err := h.client.SetBudget(h.ctxFor(t, uuid.NewString()), &sorobanv1.SetBudgetRequest{
		Id: global.GetId(), Version: global.GetVersion(), LimitMicros: 1,
		Mode: sorobanv1.BudgetMode_BUDGET_MODE_HARD, Enabled: true,
		ScopeType: global.GetScopeType(), Period: global.GetPeriod(),
	})

	requireStatus(t, err, codes.NotFound, "RESOURCE_NOT_FOUND")
}

func TestSetBudgetRejectsInvalidBudgets(t *testing.T) {
	valid := func() *sorobanv1.SetBudgetRequest {
		return &sorobanv1.SetBudgetRequest{
			ScopeType: sorobanv1.ScopeType_SCOPE_TYPE_SERVICE, ScopeValue: "fude",
			Period: sorobanv1.BudgetPeriod_BUDGET_PERIOD_DAILY, LimitMicros: 1,
			Mode: sorobanv1.BudgetMode_BUDGET_MODE_HARD, Enabled: true,
		}
	}
	tests := []struct {
		name   string
		mutate func(*sorobanv1.SetBudgetRequest)
	}{
		{"unspecified scope", func(r *sorobanv1.SetBudgetRequest) { r.ScopeType = sorobanv1.ScopeType_SCOPE_TYPE_UNSPECIFIED }},
		{"unspecified period", func(r *sorobanv1.SetBudgetRequest) { r.Period = sorobanv1.BudgetPeriod_BUDGET_PERIOD_UNSPECIFIED }},
		{"unspecified mode", func(r *sorobanv1.SetBudgetRequest) { r.Mode = sorobanv1.BudgetMode_BUDGET_MODE_UNSPECIFIED }},
		{"negative limit", func(r *sorobanv1.SetBudgetRequest) { r.LimitMicros = -1 }},
		{"global with a scope value", func(r *sorobanv1.SetBudgetRequest) { r.ScopeType = sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL }},
		{"service without a name", func(r *sorobanv1.SetBudgetRequest) { r.ScopeValue = "" }},
		{"service named like a feature", func(r *sorobanv1.SetBudgetRequest) { r.ScopeValue = "fude.post" }},
		{"feature without a service", func(r *sorobanv1.SetBudgetRequest) {
			r.ScopeType = sorobanv1.ScopeType_SCOPE_TYPE_FEATURE
		}},
		{"threshold above 100", func(r *sorobanv1.SetBudgetRequest) { r.Thresholds = []int32{150} }},
		{"threshold of zero", func(r *sorobanv1.SetBudgetRequest) { r.Thresholds = []int32{0} }},
		{"id that is not a UUID", func(r *sorobanv1.SetBudgetRequest) { r.Id = "nope" }},
	}
	h := newHarness(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid()
			tt.mutate(req)

			_, err := h.client.SetBudget(h.ctx(t), req)

			requireStatus(t, err, codes.InvalidArgument, "INVALID_BUDGET")
		})
	}
}

func TestRaisingALimitThroughSetBudgetChangesWhatReserveAllows(t *testing.T) {
	// Arrange: a tiny global limit blocks a $0.004 reservation.
	h := newHarness(t)
	global := findBudget(t, h.listBudgets(t), sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, "", sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY)
	set := func(limit int64, version int32) *sorobanv1.Budget {
		res, err := h.client.SetBudget(h.ctx(t), &sorobanv1.SetBudgetRequest{
			Id: global.GetId(), Version: version, LimitMicros: limit, Enabled: true,
			Mode: sorobanv1.BudgetMode_BUDGET_MODE_HARD, ScopeType: global.GetScopeType(), Period: global.GetPeriod(),
		})
		if err != nil {
			t.Fatalf("SetBudget: %v", err)
		}
		return res.GetBudget()
	}
	lowered := set(1000, global.GetVersion())
	_, blocked := h.client.Reserve(h.ctx(t), reserveReq(1000))

	// Act
	set(10_000, lowered.GetVersion())
	_, allowed := h.client.Reserve(h.ctx(t), reserveReq(1000))

	// Assert
	requireStatus(t, blocked, codes.ResourceExhausted, "BUDGET_EXHAUSTED")
	if allowed != nil {
		t.Fatalf("Reserve after raising the limit: %v", allowed)
	}
}

func TestSetPriceChangesFutureEstimatesAndIsListed(t *testing.T) {
	// Arrange
	h := newHarness(t)
	price := &sorobanv1.Price{
		Model: "claude-opus-5-5", EffectiveFrom: "2026-10-01",
		InputMicrosPerMtok: 8_000_000, OutputMicrosPerMtok: 40_000_000,
	}

	// Act
	set, err := h.client.SetPrice(h.ctx(t), &sorobanv1.SetPriceRequest{Price: price})
	if err != nil {
		t.Fatalf("SetPrice: %v", err)
	}
	res, err := h.client.Reserve(h.ctx(t), reserveReq(1000))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	listed, err := h.client.ListPrices(h.ctx(t), &sorobanv1.ListPricesRequest{})
	if err != nil {
		t.Fatalf("ListPrices: %v", err)
	}

	// Assert
	if set.GetPrice().GetEffectiveFrom() != "2026-10-01" {
		t.Errorf("stored price = %v", set.GetPrice())
	}
	if res.GetEstMicros() != 8000 {
		t.Errorf("estimate = %d, want 8000 at the new price", res.GetEstMicros())
	}
	var opus []string
	for _, p := range listed.GetPrices() {
		if p.GetModel() == "claude-opus-5-5" {
			opus = append(opus, p.GetEffectiveFrom())
		}
	}
	if len(opus) != 2 || opus[0] != "2026-10-01" || opus[1] != "2026-01-01" {
		t.Errorf("opus prices listed %v, want the new one first and the seeded one kept", opus)
	}
}

func TestSetPriceRejectsInvalidPrices(t *testing.T) {
	tests := []struct {
		name  string
		price *sorobanv1.Price
	}{
		{"no model", &sorobanv1.Price{EffectiveFrom: "2026-10-01"}},
		{"bad date", &sorobanv1.Price{Model: "m", EffectiveFrom: "01/10/2026"}},
		{"negative rate", &sorobanv1.Price{Model: "m", EffectiveFrom: "2026-10-01", InputMicrosPerMtok: -1}},
	}
	h := newHarness(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.SetPrice(h.ctx(t), &sorobanv1.SetPriceRequest{Price: tt.price})

			requireStatus(t, err, codes.InvalidArgument, "INVALID_PRICE")
		})
	}
}

// spendFixture commits three calls: two on fude with opus, one on tsubame with
// haiku. They cost 4000, 2000 and 1000 micros.
func (h *harness) spendFixture(t *testing.T) {
	t.Helper()
	calls := []struct {
		service, feature, model string
		tokens                  int32
	}{
		{"fude", "fude.cover_letter", "claude-opus-5-5", 1000},
		{"fude", "fude.post", "claude-opus-5-5", 500},
		{"tsubame", "tsubame.classify", "claude-haiku-4-5", 1000},
	}
	for _, c := range calls {
		res, err := h.client.Reserve(h.ctx(t), &sorobanv1.ReserveRequest{
			Service: c.service, Feature: c.feature, Model: c.model, EstInputTokens: c.tokens,
		})
		if err != nil {
			t.Fatalf("Reserve %s: %v", c.feature, err)
		}
		h.commit(t, res.GetReservationId(), c.tokens)
	}
}

func TestGetSpendGroupsTheLedger(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.spendFixture(t)
	window := func(group sorobanv1.SpendGroup) *sorobanv1.GetSpendRequest {
		return &sorobanv1.GetSpendRequest{
			From:    timestamppb.New(fixedNow.Add(-24 * time.Hour)),
			To:      timestamppb.New(fixedNow.Add(24 * time.Hour)),
			GroupBy: group,
		}
	}
	tests := []struct {
		name  string
		group sorobanv1.SpendGroup
		want  map[string]int64
	}{
		{"by service", sorobanv1.SpendGroup_SPEND_GROUP_SERVICE, map[string]int64{"fude": 6000, "tsubame": 1000}},
		{"by feature", sorobanv1.SpendGroup_SPEND_GROUP_FEATURE, map[string]int64{"fude.cover_letter": 4000, "fude.post": 2000, "tsubame.classify": 1000}},
		{"by model", sorobanv1.SpendGroup_SPEND_GROUP_MODEL, map[string]int64{"claude-opus-5-5": 6000, "claude-haiku-4-5": 1000}},
		{"by day in IST", sorobanv1.SpendGroup_SPEND_GROUP_DAY, map[string]int64{"2026-10-05": 7000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			res, err := h.client.GetSpend(h.ctx(t), window(tt.group))
			// Assert
			if err != nil {
				t.Fatalf("GetSpend: %v", err)
			}
			got := map[string]int64{}
			for _, r := range res.GetRows() {
				got[r.GetKey()] = r.GetCostMicros()
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("%s = %d, want %d", k, got[k], want)
				}
			}
			if res.GetTotalMicros() != 7000 {
				t.Errorf("total = %d, want 7000", res.GetTotalMicros())
			}
		})
	}
}

func TestGetSpendIsPerOwnerAndPerRange(t *testing.T) {
	h := newHarness(t)
	h.spendFixture(t)
	req := &sorobanv1.GetSpendRequest{
		From:    timestamppb.New(fixedNow.Add(-24 * time.Hour)),
		To:      timestamppb.New(fixedNow.Add(24 * time.Hour)),
		GroupBy: sorobanv1.SpendGroup_SPEND_GROUP_SERVICE,
	}
	tomorrow := &sorobanv1.GetSpendRequest{
		From: timestamppb.New(fixedNow.Add(time.Hour)), To: timestamppb.New(fixedNow.Add(48 * time.Hour)),
		GroupBy: sorobanv1.SpendGroup_SPEND_GROUP_SERVICE,
	}

	stranger, err := h.client.GetSpend(h.ctxFor(t, uuid.NewString()), req)
	if err != nil {
		t.Fatalf("stranger: %v", err)
	}
	later, err := h.client.GetSpend(h.ctx(t), tomorrow)
	if err != nil {
		t.Fatalf("later: %v", err)
	}

	if stranger.GetTotalMicros() != 0 || later.GetTotalMicros() != 0 {
		t.Errorf("stranger saw %d and an empty range %d, want 0 and 0", stranger.GetTotalMicros(), later.GetTotalMicros())
	}
}

func TestGetSpendRejectsBadRanges(t *testing.T) {
	h := newHarness(t)
	from, to := timestamppb.New(fixedNow), timestamppb.New(fixedNow.Add(time.Hour))
	tests := []struct {
		name string
		req  *sorobanv1.GetSpendRequest
	}{
		{"no group", &sorobanv1.GetSpendRequest{From: from, To: to}},
		{"no range", &sorobanv1.GetSpendRequest{GroupBy: sorobanv1.SpendGroup_SPEND_GROUP_DAY}},
		{"from after to", &sorobanv1.GetSpendRequest{From: to, To: from, GroupBy: sorobanv1.SpendGroup_SPEND_GROUP_DAY}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.GetSpend(h.ctx(t), tt.req)

			requireStatus(t, err, codes.InvalidArgument, "INVALID_RANGE")
		})
	}
}

func TestSetBudgetUpdateNeedsOnlyTheIDAndVersion(t *testing.T) {
	// Arrange
	h := newHarness(t)
	global := findBudget(t, h.listBudgets(t), sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, "", sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY)

	// Act
	res, err := h.client.SetBudget(h.ctx(t), &sorobanv1.SetBudgetRequest{
		Id: global.GetId(), Version: global.GetVersion(), LimitMicros: 30_000_000,
		Mode: sorobanv1.BudgetMode_BUDGET_MODE_SOFT, Enabled: false, Thresholds: []int32{90},
	})
	// Assert
	if err != nil {
		t.Fatalf("SetBudget: %v", err)
	}
	b := res.GetBudget()
	if b.GetScopeType() != sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL || b.GetPeriod() != sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY {
		t.Errorf("scope and period changed: %v", b)
	}
	if b.GetLimitMicros() != 30_000_000 || b.GetMode() != sorobanv1.BudgetMode_BUDGET_MODE_SOFT || b.GetEnabled() ||
		len(b.GetThresholds()) != 1 || b.GetThresholds()[0] != 90 {
		t.Errorf("budget = %v, want $30 soft, disabled, thresholds [90]", b)
	}
}
