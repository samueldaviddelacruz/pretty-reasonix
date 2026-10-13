// Package sandbox is the enforcement layer beneath permissions: the writable
// roots, forbidden reads and network policy that a command or file writer
// actually runs under.
//
// Confinement is described by one Spec and implemented per platform, using
// Seatbelt on macOS and bubblewrap on Linux. Where no backend exists the honest
// answer is to refuse to run shell rather than to run it unconfined
// (docs/TECHNICAL_DESIGN.md §6.8).
package sandbox
