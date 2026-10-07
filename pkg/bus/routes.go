package bus

import "slices"

// routes maps an event type (a domain noun and verb such as "job.added") to
// the consumer services that must receive it. Consumer names are service
// names (for example "taiko"). Entries are added together with the producer
// and consumer of each event, as listed in the Shogun LLD event catalog.
var routes = map[string][]string{
	"job.added":              {"fude", "sensei"},
	"job.status_changed":     {"sensei"},
	"job.follow_up_due":      {"taiko", "sensei"},
	"contact.added":          {"sensei"},
	"contact.status_changed": {"fude", "sensei"},
	"contact.follow_up_due":  {"taiko", "sensei"},
	"draft.ready":            {"taiko"},
	"draft.failed":           {"taiko"},
	"draft.approved":         {"sensei"},
	"draft.sent":             {"fude", "kagami", "sensei"},
	"draft.send_failed":      {"fude", "taiko"},
	"mail.classified":        {"kagami", "taiko", "sensei"},
	"mail.reply_detected":    {"kagami", "taiko", "sensei"},
	"cost.threshold_reached": {"taiko", "sensei"},
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
