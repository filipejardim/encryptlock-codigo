// Package cmd builds the lockkeeper command line, based on the Cosmos SDK
// v0.55 reference app (simd).
package cmd

import (
	"errors"
	"os"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"cosmossdk.io/log/v2"
	confixcmd "cosmossdk.io/tools/confix/cmd"

	cmtmldsa "github.com/cometbft/cometbft/crypto/mldsa65"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/debug"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"encryptlock/lockkeeper/app"
	"encryptlock/lockkeeper/hybrid"
)

// NewRootCmd creates the root lockkeeper command.
func NewRootCmd() *cobra.Command {
	// Instantiate the app once to get its encoding configuration.
	tempApp := app.New(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(app.DefaultNodeHome))

	initClientCtx := client.Context{}.
		WithCodec(tempApp.AppCodec()).
		WithInterfaceRegistry(tempApp.InterfaceRegistry()).
		WithTxConfig(tempApp.TxConfig()).
		WithLegacyAmino(tempApp.LegacyAmino()).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithKeyringOptions(hybrid.KeyringOption()).
		WithHomeDir(app.DefaultNodeHome).
		WithViper("")

	rootCmd := &cobra.Command{
		Use:           "lockkeeper",
		Short:         "LockKeeper: EncryptLock network node and command-line wallet",
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			initClientCtx = initClientCtx.WithCmdContext(cmd.Context())
			initClientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
			if err != nil {
				return err
			}
			initClientCtx, err = config.ReadFromClientConfig(initClientCtx)
			if err != nil {
				return err
			}
			if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
				return err
			}

			srvCfg := serverconfig.DefaultConfig()
			// Transfer fees are charged by EncryptLock's own rules (Phase 1,
			// step 6), not by Cosmos gas prices.
			srvCfg.MinGasPrices = "0" + app.BaseDenom
			if err := server.InterceptConfigsPreRunHandler(cmd, serverconfig.DefaultConfigTemplate, *srvCfg, cometConfig()); err != nil {
				return err
			}
			applyLightSettings(cmd)
			return nil
		},
	}

	initRootCmd(rootCmd, tempApp.TxConfig(), tempApp.BasicModuleManager)

	autoCliOpts := tempApp.AutoCliOpts()
	autoCliOpts.ClientCtx = initClientCtx
	nodeCmds := nodeservice.NewNodeCommands()
	autoCliOpts.ModuleOptions[nodeCmds.Name()] = nodeCmds.AutoCLIOptions()
	if err := autoCliOpts.EnhanceRootCommand(rootCmd); err != nil {
		panic(err)
	}

	return rootCmd
}

func initRootCmd(rootCmd *cobra.Command, txConfig client.TxConfig, basicManager module.BasicManager) {
	sdk.GetConfig().Seal()

	rootCmd.AddCommand(
		initCmd(basicManager),
		debug.Cmd(),
		confixcmd.ConfigCommand(),
	)

	server.AddCommandsWithStartCmdOptions(rootCmd, app.DefaultNodeHome, newApp, appExport, server.StartCmdOptions{
		PostSetup: dietPostSetup, // the disk clean-up next to the LockKeeper (diet.go)
	})

	rootCmd.AddCommand(
		server.StatusCommand(),
		genesisCmd(txConfig, basicManager),
		queryCommand(),
		txCommand(),
		keys.Commands(),
		walletCmd(),
		pcHeartbeatCmd(),
		pcAutostartCmd(),
		chipCmd(),
	)
}

// initCmd is the standard init command, but LockKeepers sign their votes with
// the post-quantum ML-DSA-65 key by default.
func initCmd(basicManager module.BasicManager) *cobra.Command {
	cmd := genutilcli.InitCmd(basicManager, app.DefaultNodeHome)
	flag := cmd.Flags().Lookup(genutilcli.FlagConsensusKeyAlgo)
	if err := flag.Value.Set(cmtmldsa.KeyType); err != nil {
		panic(err)
	}
	flag.DefValue = cmtmldsa.KeyType
	return cmd
}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		rpc.WaitTxCmd(),
		server.QueryBlockCmd(),
		authcmd.QueryTxsByEventsCmd(),
		server.QueryBlocksCmd(),
		authcmd.QueryTxCmd(),
		server.QueryBlockResultsCmd(),
	)
	return cmd
}

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetValidateSignaturesCommand(),
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		authcmd.GetSimulateCmd(),
	)
	return cmd
}

func newApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions) servertypes.Application {
	return app.New(logger, db, true, appOpts, server.DefaultBaseappOptions(appOpts)...)
}

func appExport(
	logger log.Logger,
	db dbm.DB,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	if _, ok := appOpts.(*viper.Viper); !ok {
		return servertypes.ExportedApp{}, errors.New("appOpts is not viper.Viper")
	}

	var lockApp *app.LockKeeperApp
	if height != -1 {
		lockApp = app.New(logger, db, false, appOpts)
		if err := lockApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else {
		lockApp = app.New(logger, db, true, appOpts)
	}
	return lockApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}

// genesisCmd is the standard genesis command plus set-dev-fund and
// chip-checker.
func genesisCmd(txConfig client.TxConfig, basicManager module.BasicManager) *cobra.Command {
	cmd := genutilcli.Commands(txConfig, basicManager, app.DefaultNodeHome)
	cmd.AddCommand(setDevFundCmd(), chipCheckerCmd(), setPolygonCmd())
	return cmd
}

// cometConfig is CometBFT's default configuration with EncryptLock's block
// timing: a new block about every second (white paper target: 1–2 s).
// It must differ from CometBFT's own default (1 s): the Cosmos SDK replaces
// that exact value with 5 s when it writes a new config.toml.
func cometConfig() *cmtcfg.Config {
	cfg := cmtcfg.DefaultConfig()
	cfg.Consensus.TimeoutCommit = 900 * time.Millisecond
	return cfg
}
