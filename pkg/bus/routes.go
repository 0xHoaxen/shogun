package bus

import "slices"

// routes maps an event type (a domain noun and verb such as "job.added") to
// the consumer services that must receive it. Consumer names are service
// names (for example "taiko"). Entries are added together with the producer
// and consumer of each event, as listed in the Shogun LLD event catalog.
var routes = map[string][]string{
	"job.added":              {"fude"},
	"job.follow_up_due":      {"taiko"},
	"contact.status_changed": {"fude"},
	"contact.follow_up_due":  {"taiko"},
	"draft.ready":            {"taiko"},
	"draft.failed":           {"taiko"},
	"draft.sent":             {"fude", "kagami"},
	"draft.send_failed":      {"fude", "taiko"},
	"mail.classified":        {"kagami", "taiko"},
	"mail.reply_detected":    {"kagami", "taiko"},
	"cost.threshold_reached": {"taiko"},
	"cost.budget_exhausted":  {"taiko"},

	"profile.suggestion_ready": {"taiko"},
	"discovery.match_found":    {"taiko"},

	"learning.activity_added": {"fude"},
	"learning.item_completed": {"fude", "katana"},
}

// Consumers returns the services subscribed to eventType, or nil when there
// are none. The result is a copy and may be modified by the caller.
func Consumers(eventType string) []string {
	return consumersIn(routes, eventType)
}

func consumersIn(table map[string][]string, eventType string) []string {
	c, ok := table[eventType]
	if !ok {
		return nil
	}
	return slices.Clone(c)
}
