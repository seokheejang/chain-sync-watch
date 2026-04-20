package blockscout

import "github.com/seokheejang/chain-sync-watch/internal/chain"

// DefaultBaseURL maps a chain id to the Blockscout instance we point
// at by default. Callers override via WithBaseURL when they run a
// private mirror or need a different instance.
var DefaultBaseURL = map[chain.ChainID]string{
	chain.OptimismMainnet: "https://optimism.blockscout.com",
	chain.EthereumMainnet: "https://eth.blockscout.com",
}
