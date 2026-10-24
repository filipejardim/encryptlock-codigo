package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"encryptlock/lockkeeper/x/market"
	markettypes "encryptlock/lockkeeper/x/market/types"
)

// setPolygonCmd writes the network's Polygon side into genesis: every
// LockKeeper then watches USDC payments there by itself. Without it no offer
// can be taken.
func setPolygonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-polygon [chain-id] [vault] [usdc] [start-block]",
		Short: "Set where the network watches USDC payments (Polygon 137 or Amoy 80002, swap vault, USDC contract, vault's first block)",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			chainID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}
			start, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return err
			}
			c := market.PolygonConfig{ChainID: chainID, Vault: args[1], USDC: args[2], StartBlock: start}
			if !c.Valid() {
				return fmt.Errorf("need a chain id and two 0x addresses")
			}
			clientCtx := client.GetClientContextFromCmd(cmd)
			config := server.GetServerContextFromCmd(cmd).Config
			config.SetRoot(clientCtx.HomeDir)
			genFile := config.GenesisFile()
			appGenesis, err := genutiltypes.AppGenesisFromFile(genFile)
			if err != nil {
				return err
			}
			var appState map[string]json.RawMessage
			if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
				return err
			}
			var gs market.GenesisState
			if err := json.Unmarshal(appState[markettypes.ModuleName], &gs); err != nil {
				return err
			}
			gs.Polygon = &c
			if appState[markettypes.ModuleName], err = json.Marshal(gs); err != nil {
				return err
			}
			if appGenesis.AppState, err = json.Marshal(appState); err != nil {
				return err
			}
			return genutil.ExportGenesisFile(appGenesis, genFile)
		},
	}
}
