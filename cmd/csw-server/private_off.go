//go:build !private

package main

import "github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"

// registerPrivateAdapters is a no-op in the default public build.
// The `private` build tag flips this out for
// `private_on.go` where the user's private adapters call
// gateway.Registry.Add. Keeping the hook here (not inside the
// gateway package) means the public binary has zero trace of
// whatever custom types the private fork registers.
func registerPrivateAdapters(_ gateway.Registry) {}
