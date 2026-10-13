// Package anthropic implements the "anthropic" provider kind: the Messages API,
// streaming over server-sent events.
//
// It shares retry, backoff and the idle watchdog with the other kinds through
// internal/httpclient, so a vendor difference is confined to request shape and
// stream decoding.
package anthropic
