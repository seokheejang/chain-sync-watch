package routescan

import (
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// BaseURL returns Routescan's Etherscan-compatible endpoint for a
// chain id. Routescan serves every chain through the same template
// so we just substitute the id.
func BaseURL(id chain.ChainID) string {
	return fmt.Sprintf("https://api.routescan.io/v2/network/mainnet/evm/%d/etherscan/api", id.Uint64())
}
