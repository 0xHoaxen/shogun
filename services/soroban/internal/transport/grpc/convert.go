package grpc

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

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
