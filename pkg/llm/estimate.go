package llm

const (
	// bytesPerTokenEstimate is how many bytes of text one token is assumed to be.
	// English runs about four, and code and other languages fewer, so three
	// overestimates and the budget is never held too small.
	bytesPerTokenEstimate = 3
	// perMessageOverhead is the tokens a turn costs besides its text.
	perMessageOverhead = 8
)

// EstimateInputTokens estimates the input tokens of a prompt without asking the
// provider. It is an upper-leaning estimate used to hold a budget; the real
// usage the provider reports is what gets committed.
func EstimateInputTokens(system string, messages []Message) int32 {
	bytes := len(system)
	for _, m := range messages {
		bytes += len(m.Content)
	}
	tokens := int64((bytes+bytesPerTokenEstimate-1)/bytesPerTokenEstimate) + int64(len(messages))*perMessageOverhead
	if tokens > int64(1<<31-1) {
		return 1<<31 - 1
	}
	return int32(tokens)
}
