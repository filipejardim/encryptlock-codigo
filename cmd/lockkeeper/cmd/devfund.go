package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"encryptlock/lockkeeper/x/transfers"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
)

// setDevFundCmd writes the development fund address into genesis. It can
// never be changed after the network starts.
func setDevFundCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-dev-fund [address]",
		Short: "Set the development fund address in genesis (fixed forever once the network starts)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			if _, err := sdk.AccAddressFromBech32(args[0]); err != nil {
				return fmt.Errorf("invalid address: %w", err)
			}

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
			var gs transfers.GenesisState
			if err := json.Unmarshal(appState[transferstypes.ModuleName], &gs); err != nil {
				return err
			}
			gs.DevFundAddress = args[0]
			if appState[transferstypes.ModuleName], err = json.Marshal(gs); err != nil {
				return err
			}
			if appGenesis.AppState, err = json.Marshal(appState); err != nil {
				return err
			}
			return genutil.ExportGenesisFile(appGenesis, genFile)
		},
	}
}
