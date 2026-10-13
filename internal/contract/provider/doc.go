// Package provider defines the model-vendor interface and the request, message
// and streaming types that every supported wire protocol maps onto.
//
// It is part of the contract layer and imports nothing else in this module
// (docs/TECHNICAL_DESIGN.md §6.2); concrete kinds such as
// internal/provider/openai implement Provider and register themselves from
// internal/assembly.
package provider
