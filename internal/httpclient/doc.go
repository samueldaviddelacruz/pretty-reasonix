// Package httpclient is the single owner of outbound HTTP behaviour: bounded
// retry with backoff and jitter, server-sent-event reading, the stream idle
// watchdog, and proxy policy.
//
// Provider kinds contain no retry logic of their own. The default HTTP client
// is never used (rule [#81] of the Go practice baseline): timeouts and the
// transport are configured explicitly here.
package httpclient
