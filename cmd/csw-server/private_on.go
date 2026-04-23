//go:build private

package main

import (
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"
	"github.com/seokheejang/chain-sync-watch/private/myindexer"
)

// registerPrivateAdapters wires every private-build-only adapter
// into the gateway Registry. The public default build compiles
// the `!private` stub in private_off.go instead, so this file (and
// the packages it imports from under private/) are invisible in
// OSS artifacts.
//
// Add additional `somepkg.Register(reg)` calls here when you
// stand up more private adapters.
func registerPrivateAdapters(reg gateway.Registry) {
	myindexer.Register(reg)
}
