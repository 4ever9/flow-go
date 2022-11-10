package message

import (
	"fmt"
)

var (
	validationExclusionList = []string{TestMessage}
)

// init is called first time this package is imported.
// It creates and initializes AuthorizationConfigs for each message type.
func init() {
	initializeMessageAuthConfigsMap()
	validateMessageAuthConfigsMap(validationExclusionList)
}

func validateMessageAuthConfigsMap(excludeList []string) {
	for _, msgAuthConf := range authorizationConfigs {
		if excludeConfig(msgAuthConf.Name, excludeList) {
			continue
		}

		for _, config := range msgAuthConf.Config {
			if len(config.AllowedProtocols) != 1 {
				panic(fmt.Errorf("error: message authorization config for message type %s should have a single allowed protocol found %d: %s", msgAuthConf.Name, len(config.AllowedProtocols), config.AllowedProtocols))
			}
		}
	}
}

func excludeConfig(name string, excludeList []string) bool {
	for _, s := range excludeList {
		if s == name {
			return true
		}
	}

	return false
}

// string constants for all message types sent on the network
const (
	BlockProposal        = "BlockProposal"
	BlockVote            = "BlockVote"
	SyncRequest          = "SyncRequest"
	SyncResponse         = "SyncResponse"
	RangeRequest         = "RangeRequest"
	BatchRequest         = "BatchRequest"
	BlockResponse        = "BlockResponse"
	ClusterBlockProposal = "ClusterBlockProposal"
	ClusterBlockVote     = "ClusterBlockVote"
	ClusterBlockResponse = "ClusterBlockResponse"
	CollectionGuarantee  = "CollectionGuarantee"
	TransactionBody      = "TransactionBody"
	ExecutionReceipt     = "ExecutionReceipt"
	ResultApproval       = "ResultApproval"
	ChunkDataRequest     = "ChunkDataRequest"
	ChunkDataResponse    = "ChunkDataResponse"
	ApprovalRequest      = "ApprovalRequest"
	ApprovalResponse     = "ApprovalResponse"
	EntityRequest        = "EntityRequest"
	EntityResponse       = "EntityResponse"
	TestMessage          = "TestMessage"
	DKGMessage           = "DKGMessage"
)
