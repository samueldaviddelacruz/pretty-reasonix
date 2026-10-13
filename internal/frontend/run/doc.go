// Package run is the headless frontend behind `prx run` and `prx -p`.
//
// It drives the same controller as the terminal UI and differs only in its sink
// and its approver, which is what keeps the two frontends from diverging
// (docs/TECHNICAL_DESIGN.md §6.13). It is also the path the end-to-end suite
// exercises.
package run
