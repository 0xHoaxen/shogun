package domain

import "errors"

// tokensPerMtok is the token count a per-million-token price is quoted for.
const tokensPerMtok = 1_000_000

// Usage is the token counts of one call.
type Usage struct {
	InputTokens      int32
	OutputTokens     int32
	CacheReadTokens  int32
	CacheWriteTokens int32
}

// Price is a model's rates in micro-dollars per million tokens.
type Price struct {
	InputMicrosPerMtok      int64
	OutputMicrosPerMtok     int64
	CacheReadMicrosPerMtok  int64
	CacheWriteMicrosPerMtok int64
}

// ErrNegativeUsage means a token count was below zero.
var ErrNegativeUsage = errors.New("domain: token counts must not be negative")

// Cost prices u at p in micro-dollars, rounded up so that a metered call is
// never undercounted. All arithmetic is integer, so there is no float
// rounding.
func Cost(p Price, u Usage) (int64, error) {
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 {
		return 0, ErrNegativeUsage
	}
	scaled := int64(u.InputTokens)*p.InputMicrosPerMtok +
		int64(u.OutputTokens)*p.OutputMicrosPerMtok +
		int64(u.CacheReadTokens)*p.CacheReadMicrosPerMtok +
		int64(u.CacheWriteTokens)*p.CacheWriteMicrosPerMtok
	return (scaled + tokensPerMtok - 1) / tokensPerMtok, nil
}
