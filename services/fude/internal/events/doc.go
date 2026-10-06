// Package events holds the fude inbox handlers: the events fude reacts to.
// Each handler runs inside the inbox transaction, so a draft and the record
// that its event was seen are saved together.
package events
