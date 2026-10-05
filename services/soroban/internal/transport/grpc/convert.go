package grpc

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

const dateLayout = "2006-01-02"

func domainScope(s string) domain.ScopeType { return domain.ScopeType(s) }
func domainPeriod(s string) domain.Period   { return domain.Period(s) }
func domainMode(s string) domain.Mode       { return domain.Mode(s) }

// priceToProto converts one price.
func priceToProto(p db.Price) *sorobanv1.Price {
	return &sorobanv1.Price{
		Model:                   p.Model,
		EffectiveFrom:           p.EffectiveFrom.Format(dateLayout),
		InputMicrosPerMtok:      p.InputMicrosPerMtok,
		OutputMicrosPerMtok:     p.OutputMicrosPerMtok,
		CacheReadMicrosPerMtok:  p.CacheReadMicrosPerMtok,
		CacheWriteMicrosPerMtok: p.CacheWriteMicrosPerMtok,
	}
}

// ledgerToProto converts one ledger row.
func ledgerToProto(e db.Ledger) *sorobanv1.LedgerEntry {
	return &sorobanv1.LedgerEntry{
		Id:            e.ID.String(),
		ReservationId: e.ReservationID.String(),
		Service:       e.Service,
		Feature:       e.Feature,
		Model:         e.Model,
		Usage: &sorobanv1.Usage{
			InputTokens:      e.InputTokens,
			OutputTokens:     e.OutputTokens,
			CacheReadTokens:  e.CacheReadTokens,
			CacheWriteTokens: e.CacheWriteTokens,
		},
		CostMicros: e.CostMicros,
		OccurredAt: timestamppb.New(e.OccurredAt),
	}
}
