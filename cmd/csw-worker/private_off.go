//go:build !private

package main

import "github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"

func registerPrivateAdapters(_ gateway.Registry) {}
