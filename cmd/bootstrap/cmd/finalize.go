package cmd

import (
	"fmt"
	"strings"

	"github.com/dapperlabs/flow-go/model/flow"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	flagConfig             string
	flagPartnerNodeInfoDir string
	flagPartnerStakes      string
)

type PartnerStakes map[flow.Identifier]uint64

// finalizeCmd represents the finalize command
var finalizeCmd = &cobra.Command{
	Use:   "finalize",
	Short: "Finalize the bootstrapping process",
	Long: `Finalize the bootstrapping process, which includes generating of internal networking and staking keys,
running the DKG for generating the random beacon keys, generating genesis execution state, seal, block and QC.`,
	Run: func(cmd *cobra.Command, args []string) {
		internalNodesPub, internalNodesPriv := genNetworkAndStakingKeys()

		partnerNodes := assemblePartnerNodes()
		stakingNodes := append(internalNodesPub, partnerNodes...)

		writeJSON(filenameNodeInfosPub, stakingNodes)

		dkgDataPub, dkgDataPriv := runDKG(filterConsensusNodes(stakingNodes))

		stateCommitment := genGenesisExecutionState()

		block := constructGenesisBlock(stateCommitment, stakingNodes, dkgDataPub)

		constructGenesisQC(&block, filterConsensusNodes(stakingNodes),
			filterConsensusNodesPriv(internalNodesPriv),
			dkgDataPriv)
	},
}

func init() {
	rootCmd.AddCommand(finalizeCmd)

	finalizeCmd.Flags().StringVarP(&flagConfig, "config", "c", "",
		"path to a JSON file containing multiple node configurations (fields Role, Address, Stake)")
	finalizeCmd.MarkFlagRequired("config")
	finalizeCmd.Flags().StringVarP(&flagPartnerNodeInfoDir, "partner-dir", "p", "", fmt.Sprintf("path to directory "+
		"containing one JSON file ending with %v for every partner node (fields Role, Address, NodeID, "+
		"NetworkPubKey, StakingPubKey)", filenamePartnerNodeInfoSuffix))
	finalizeCmd.MarkFlagRequired("partner-dir")
	finalizeCmd.Flags().StringVarP(&flagPartnerStakes, "partner-stakes", "s", "", "path to a JSON file containing "+
		"a map from partner node's NodeID to their stake")
	finalizeCmd.MarkFlagRequired("partner-stakes")
}

func assemblePartnerNodes() []NodeInfoPub {
	partners := readPartnerNodes()
	log.Info().Msgf("read %v partner node configurations", len(partners))

	var stakes PartnerStakes
	readJSON(flagPartnerStakes, &stakes)
	log.Info().Msgf("read %v stakes for partner nodes", len(stakes))

	var nodes []NodeInfoPub
	for _, partner := range partners {
		// validate every single partner node
		nodeID := validateNodeID(partner.NodeID)
		networkPubKey := validateNetworkPubKey(partner.NetworkPubKey)
		stakingPubKey := validateStakingPubKey(partner.StakingPubKey)
		stake := validateStake(stakes[partner.NodeID])

		nodes = append(nodes, NodeInfoPub{
			Role:          partner.Role,
			Address:       partner.Address,
			NodeID:        nodeID,
			NetworkPubKey: networkPubKey,
			StakingPubKey: stakingPubKey,
			Stake:         stake,
		})
	}

	return nodes
}

func validateNodeID(nodeID flow.Identifier) flow.Identifier {
	if nodeID == flow.ZeroID {
		log.Fatal().Msg("NodeID must not be zero")
	}
	return nodeID
}

func validateNetworkPubKey(key EncodableNetworkPubKey) EncodableNetworkPubKey {
	if key.PublicKey == nil {
		log.Fatal().Msg("NetworkPubKey must not be nil")
	}
	return key
}

func validateStakingPubKey(key EncodableStakingPubKey) EncodableStakingPubKey {
	if key.PublicKey == nil {
		log.Fatal().Msg("StakingPubKey must not be nil")
	}
	return key
}

func validateStake(stake uint64) uint64 {
	if stake == 0 {
		log.Fatal().Msg("Stake must be bigger than 0")
	}
	return stake
}

func readPartnerNodes() []PartnerNodeInfoPub {
	var partners []PartnerNodeInfoPub
	files, err := filesInDir(flagPartnerNodeInfoDir)
	if err != nil {
		log.Fatal().Err(err).Msg("could not read partner node infos")
	}
	for _, f := range files {
		// skip files that do not include node-infos
		if !strings.HasSuffix(f, filenamePartnerNodeInfoSuffix) {
			continue
		}

		// read file and append to partners
		var p PartnerNodeInfoPub
		readJSON(f, &p)
		partners = append(partners, p)
	}
	return partners
}
