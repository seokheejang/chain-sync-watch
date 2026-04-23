//go:build private

package main

import (
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"
	"github.com/seokheejang/chain-sync-watch/private/myindexer"
)

func registerPrivateAdapters(reg gateway.Registry) {
	myindexer.Register(reg)
}
