package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"encryptlock/lockkeeper/chip"
	"encryptlock/lockkeeper/x/rotation"
	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
)

// chipCheckerCmd makes this LockKeeper the network's chip checker (its key
// goes to config/chip-checker.key, its public key into genesis) and lists the
// computers that start the network, which need no chip proof. Everything is
// in the first block for everyone to see.
func chipCheckerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chip-checker [starting-lockkeeper-address...]",
		Short: "Make this LockKeeper the network's chip checker and list the computers that start the network (genesis)",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			for _, a := range args {
				if _, err := sdk.AccAddressFromBech32(a); err != nil {
					return fmt.Errorf("invalid address %s: %w", a, err)
				}
			}
			config := server.GetServerContextFromCmd(cmd).Config
			config.SetRoot(clientCtx.HomeDir)
			keyFile := filepath.Join(clientCtx.HomeDir, "config", chip.CheckerKeyFile)
			if _, err := os.Stat(keyFile); err == nil {
				return fmt.Errorf("%s already exists", keyFile)
			}
			secret, pub, err := chip.NewCheckerKey()
			if err != nil {
				return err
			}

			genFile := config.GenesisFile()
			appGenesis, err := genutiltypes.AppGenesisFromFile(genFile)
			if err != nil {
				return err
			}
			var appState map[string]json.RawMessage
			if err := json.Unmarshal(appGenesis.AppState, &appState); err != nil {
				return err
			}
			g, err := rotation.ParseGenesis(appState[rotationtypes.ModuleName])
			if err != nil {
				return err
			}
			g.ChipCheckers = append(g.ChipCheckers, pub)
			g.ChipFree = append(g.ChipFree, args...)
			if appState[rotationtypes.ModuleName], err = json.Marshal(g); err != nil {
				return err
			}
			if appGenesis.AppState, err = json.Marshal(appState); err != nil {
				return err
			}
			if err := os.WriteFile(keyFile, []byte(secret), 0o600); err != nil {
				return err
			}
			return genutil.ExportGenesisFile(appGenesis, genFile)
		},
	}
}
