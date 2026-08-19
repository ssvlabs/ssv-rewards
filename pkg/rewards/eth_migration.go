package rewards

import "github.com/ethereum/go-ethereum/crypto"

// ClusterMigratedToETHEvent is the event name for cluster ETH-fee migration.
const ClusterMigratedToETHEvent = "ClusterMigratedToETH"

// ClusterMigratedToETHTopic is keccak256 of the ClusterMigratedToETH event signature.
var ClusterMigratedToETHTopic = crypto.Keccak256Hash([]byte(
	"ClusterMigratedToETH(address,uint64[],uint256,uint256,uint32,(uint32,uint64,uint64,bool,uint256))",
))
