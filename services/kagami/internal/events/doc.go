// Package events holds the kagami inbox event handlers: the mail and draft
// events kagami reacts to. Each handler runs inside the inbox transaction, so
// its changes and the record that the event was seen are saved together.
package events
