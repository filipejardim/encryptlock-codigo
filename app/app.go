// Package app wires the LockKeeper blockchain application.
//
// It is a reduced version of the Cosmos SDK v0.55 reference app (simapp):
// no on-chain governance (nobody can change the rules by vote), no default
// mint module (EncryptLock has its own emission schedule), no authz/feegrant,
// and no direct coin sends (see ante.go).
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cast"

	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	nodeservice "github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/posthandler"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/consensus"
	consensusparamkeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/evidence"
	evidencekeeper "github.com/cosmos/cosmos-sdk/x/evidence/keeper"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"encryptlock/lockkeeper/chip"
	"encryptlock/lockkeeper/x/emission"
	"encryptlock/lockkeeper/x/market"
	markettypes "encryptlock/lockkeeper/x/market/types"
	"encryptlock/lockkeeper/x/powercap"
	"encryptlock/lockkeeper/x/recovery"
	recoverytypes "encryptlock/lockkeeper/x/recovery/types"
	"encryptlock/lockkeeper/x/registry"
	registrytypes "encryptlock/lockkeeper/x/registry/types"
	"encryptlock/lockkeeper/x/rotation"
	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
	"encryptlock/lockkeeper/x/transfers"
	transferstypes "encryptlock/lockkeeper/x/transfers/types"
	"encryptlock/lockkeeper/x/vault"
	vaulttypes "encryptlock/lockkeeper/x/vault/types"
)

// module account permissions
var maccPerms = map[string][]string{
	authtypes.FeeCollectorName:          nil,
	distrtypes.ModuleName:               nil,
	stakingtypes.BondedPoolName:         {authtypes.Burner, authtypes.Staking},
	stakingtypes.NotBondedPoolName:      {authtypes.Burner, authtypes.Staking},
	stakingtypes.KeyRotationFeePoolName: {authtypes.Burner},
	emission.ModuleName:                 {authtypes.Minter},
	transferstypes.ModuleName:           nil, // holds pending transfers
	vaulttypes.ModuleName:               nil, // holds locked coins
	markettypes.ModuleName:              nil, // holds the LOCK of offers and trades
	rotationtypes.ModuleName:            nil, // holds the LockKeepers' earnings until the hourly payout
}

var (
	_ runtime.AppI            = (*LockKeeperApp)(nil)
	_ servertypes.Application = (*LockKeeperApp)(nil)
)

// GenesisState is the raw genesis of every module, keyed by module name.
type GenesisState map[string]json.RawMessage

// LockKeeperApp is the EncryptLock blockchain application.
type LockKeeperApp struct {
	*baseapp.BaseApp
	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry types.InterfaceRegistry

	keys map[string]*storetypes.KVStoreKey

	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.BaseKeeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	EvidenceKeeper        evidencekeeper.Keeper
	ConsensusParamsKeeper consensusparamkeeper.Keeper
	EmissionKeeper        emission.Keeper
	RegistryKeeper        registry.Keeper
	TransfersKeeper       transfers.Keeper
	RecoveryKeeper        recovery.Keeper
	VaultKeeper           vault.Keeper
	MarketKeeper          market.Keeper
	RotationKeeper        rotation.Keeper

	// polygon watches USDC payments on Polygon (oracle.go).
	polygon polygonWatch

	// keepBlocks: whole blocks this LockKeeper keeps (retain.go); 0 keeps
	// what the Cosmos SDK decides.
	keepBlocks int64

	ModuleManager      *module.Manager
	BasicModuleManager module.BasicManager

	configurator module.Configurator
}

// New returns an initialized LockKeeperApp.
func New(
	logger log.Logger,
	db dbm.DB,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *LockKeeperApp {
	interfaceRegistry, _ := types.NewInterfaceRegistryWithOptions(types.InterfaceRegistryOptions{
		ProtoFiles: proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:          address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32AccountAddrPrefix()},
			ValidatorAddressCodec: address.Bech32Codec{Bech32Prefix: sdk.GetConfig().GetBech32ValidatorAddrPrefix()},
		},
	})
	appCodec := codec.NewProtoCodec(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()
	txConfig := authtx.NewTxConfig(appCodec, authtx.DefaultSignModes)

	if err := interfaceRegistry.SigningContext().Validate(); err != nil {
		panic(err)
	}

	std.RegisterLegacyAminoCodec(legacyAmino)
	std.RegisterInterfaces(interfaceRegistry)

	bApp := baseapp.NewBaseApp(AppName, logger, db, txConfig.TxDecoder(), baseAppOptions...)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())

	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey,
		banktypes.StoreKey,
		stakingtypes.StoreKey,
		distrtypes.StoreKey,
		slashingtypes.StoreKey,
		consensusparamtypes.StoreKey,
		upgradetypes.StoreKey,
		evidencetypes.StoreKey,
		emission.ModuleName,
		powercap.ModuleName,
		registrytypes.ModuleName,
		transferstypes.ModuleName,
		recoverytypes.ModuleName,
		vaulttypes.ModuleName,
		markettypes.ModuleName,
		rotationtypes.ModuleName,
	)

	if err := bApp.RegisterStreamingServices(appOpts, keys); err != nil {
		panic(err)
	}

	app := &LockKeeperApp{
		BaseApp:           bApp,
		legacyAmino:       legacyAmino,
		appCodec:          appCodec,
		txConfig:          txConfig,
		interfaceRegistry: interfaceRegistry,
		keys:              keys,
		keepBlocks:        cast.ToInt64(appOpts.Get(server.FlagMinRetainBlocks)),
	}

	authority := RulesAuthority()

	app.ConsensusParamsKeeper = consensusparamkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[consensusparamtypes.StoreKey]),
		authority,
		runtime.EventService{},
	)
	bApp.SetParamStore(app.ConsensusParamsKeeper.ParamsStore)

	app.AccountKeeper = authkeeper.NewAccountKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		maccPerms,
		authcodec.NewBech32Codec(Bech32Prefix),
		Bech32Prefix,
		authority,
	)

	app.BankKeeper = bankkeeper.NewBaseKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[banktypes.StoreKey]),
		app.AccountKeeper,
		BlockedAddresses(),
		authority,
		logger,
	)

	txConfig, err := authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{})
	if err != nil {
		panic(err)
	}
	app.txConfig = txConfig

	app.StakingKeeper = stakingkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[stakingtypes.StoreKey]),
		app.AccountKeeper,
		app.BankKeeper,
		authority,
		authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()),
		authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ConsensusAddrPrefix()),
	)

	app.DistrKeeper = distrkeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[distrtypes.StoreKey]),
		app.AccountKeeper,
		app.BankKeeper,
		app.StakingKeeper,
		authtypes.FeeCollectorName,
		authority,
	)

	app.SlashingKeeper = slashingkeeper.NewKeeper(
		appCodec,
		legacyAmino,
		runtime.NewKVStoreService(keys[slashingtypes.StoreKey]),
		app.StakingKeeper,
		authority,
	)

	app.StakingKeeper.SetHooks(
		stakingtypes.NewMultiStakingHooks(
			app.DistrKeeper.Hooks(),
			app.SlashingKeeper.Hooks(),
		),
	)

	skipUpgradeHeights := map[int64]bool{}
	for _, h := range cast.ToIntSlice(appOpts.Get(server.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}
	homePath := cast.ToString(appOpts.Get(flags.FlagHome))
	// The upgrade module only records module versions here: with no
	// governance, no on-chain upgrade can ever be scheduled. New LockKeeper
	// versions are installed by each operator, by choice.
	app.UpgradeKeeper = upgradekeeper.NewKeeper(
		skipUpgradeHeights,
		runtime.NewKVStoreService(keys[upgradetypes.StoreKey]),
		appCodec,
		homePath,
		app.BaseApp,
		authority,
	)

	evidenceKeeper := evidencekeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[evidencetypes.StoreKey]),
		app.StakingKeeper,
		app.SlashingKeeper,
		app.AccountKeeper.AddressCodec(),
		runtime.ProvideCometInfoService(),
	)
	app.EvidenceKeeper = *evidenceKeeper

	app.RegistryKeeper = registry.NewKeeper(
		runtime.NewKVStoreService(keys[registrytypes.ModuleName]),
		app.AccountKeeper.AddressCodec(),
	)

	app.EmissionKeeper = emission.NewKeeper(
		runtime.NewKVStoreService(keys[emission.ModuleName]),
		app.BankKeeper,
		BaseDenom,
		authtypes.FeeCollectorName,
	)

	app.TransfersKeeper = transfers.NewKeeper(
		runtime.NewKVStoreService(keys[transferstypes.ModuleName]),
		appCodec,
		app.BankKeeper,
		app.RegistryKeeper,
		app.EmissionKeeper, // genesis time, for the fee split period
		app.AccountKeeper.AddressCodec(),
		BaseDenom,
		authtypes.FeeCollectorName,
	)
	// Fair use: the registry forgets accounts that were never used.
	app.RegistryKeeper.SetPruneDeps(app.AccountKeeper, app.BankKeeper, app.TransfersKeeper)

	app.RecoveryKeeper = recovery.NewKeeper(
		runtime.NewKVStoreService(keys[recoverytypes.ModuleName]),
		appCodec,
		app.BankKeeper,
		app.RegistryKeeper,
		app.AccountKeeper.AddressCodec(),
		BaseDenom,
	)
	// An inherited or recovered LockKeeper's guarantee is given back and
	// forwarded.
	app.RecoveryKeeper.SetStaking(app.StakingKeeper)

	app.VaultKeeper = vault.NewKeeper(
		runtime.NewKVStoreService(keys[vaulttypes.ModuleName]),
		appCodec,
		app.BankKeeper,
		app.AccountKeeper.AddressCodec(),
		BaseDenom,
	)

	app.MarketKeeper = market.NewKeeper(
		runtime.NewKVStoreService(keys[markettypes.ModuleName]),
		appCodec,
		app.BankKeeper,
		app.RegistryKeeper,
		marketFees{emission: app.EmissionKeeper, transfers: app.TransfersKeeper},
		app.AccountKeeper.AddressCodec(),
		BaseDenom,
		authtypes.FeeCollectorName,
	)
	// LOCK sold on the market follow the account protection; "Not me" takes
	// the account's open offers down.
	app.MarketKeeper.SetShield(app.TransfersKeeper)
	app.TransfersKeeper.SetFreezeHook(app.MarketKeeper.TakeDownOffers)
	// Inheriting or recovering the development fund's account moves the fund.
	app.RecoveryKeeper.SetDevFund(app.TransfersKeeper)

	// Everyone switched on earns; a drawn, rotating group signs the blocks.
	app.RotationKeeper = rotation.NewKeeper(
		runtime.NewKVStoreService(keys[rotationtypes.ModuleName]),
		appCodec,
		app.StakingKeeper,
		app.BankKeeper,
		app.DistrKeeper,
		app.SlashingKeeper,
		app.AccountKeeper.AddressCodec(),
		BaseDenom,
		authtypes.FeeCollectorName,
	)

	app.ModuleManager = module.NewManager(
		genutil.NewAppModule(app.AccountKeeper, app.StakingKeeper, app, txConfig),
		auth.NewAppModule(appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts),
		bank.NewAppModule(appCodec, app.BankKeeper, app.AccountKeeper),
		slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.interfaceRegistry),
		distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper),
		// staking with the rotating signing group and the 5% voting power cap
		// (golden rule)
		powercap.NewCappedStaking(
			staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper),
			app.StakingKeeper,
			runtime.NewKVStoreService(keys[powercap.ModuleName]),
		).WithSelector(app.RotationKeeper),
		upgrade.NewAppModule(app.UpgradeKeeper, app.AccountKeeper.AddressCodec()),
		evidence.NewAppModule(app.EvidenceKeeper),
		consensus.NewAppModule(appCodec, app.ConsensusParamsKeeper),
		emission.NewAppModule(app.EmissionKeeper),
		registry.NewAppModule(app.RegistryKeeper),
		transfers.NewAppModule(app.TransfersKeeper),
		recovery.NewAppModule(app.RecoveryKeeper),
		vault.NewAppModule(app.VaultKeeper),
		market.NewAppModule(app.MarketKeeper),
		rotation.NewAppModule(app.RotationKeeper),
	)

	app.BasicModuleManager = module.NewBasicManagerFromManager(
		app.ModuleManager,
		map[string]module.AppModuleBasic{
			genutiltypes.ModuleName:  genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			distrtypes.ModuleName:    distrGenesis{distr.NewAppModule(appCodec, app.DistrKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper)},
			slashingtypes.ModuleName: slashingGenesis{slashing.NewAppModule(appCodec, app.SlashingKeeper, app.AccountKeeper, app.BankKeeper, app.StakingKeeper, app.interfaceRegistry)},
			stakingtypes.ModuleName:  stakingGenesis{staking.NewAppModule(appCodec, app.StakingKeeper, app.AccountKeeper, app.BankKeeper)},
		})
	app.BasicModuleManager.RegisterLegacyAminoCodec(legacyAmino)
	app.BasicModuleManager.RegisterInterfaces(interfaceRegistry)

	app.ModuleManager.SetOrderPreBlockers(
		upgradetypes.ModuleName,
		authtypes.ModuleName,
	)
	app.ModuleManager.SetOrderBeginBlockers(
		emission.ModuleName,      // new coins first
		rotationtypes.ModuleName, // then new coins and fees go to everyone switched on (hourly), not to distribution's signers-only payout
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		evidencetypes.ModuleName,
		stakingtypes.ModuleName,
		genutiltypes.ModuleName,
	)
	app.ModuleManager.SetOrderEndBlockers(
		banktypes.ModuleName,
		transferstypes.ModuleName, // automatic refund after 1 hour
		recoverytypes.ModuleName,  // inheritance / recovery after 30 days
		vaulttypes.ModuleName,     // locks back to their owners at their date
		markettypes.ModuleName,    // unpaid trades and expired offers give the LOCK back
		registrytypes.ModuleName,  // fair use: old counts and unused accounts
		stakingtypes.ModuleName,
		genutiltypes.ModuleName,
	)

	// genutil must come after staking (pools) and auth (params).
	genesisModuleOrder := []string{
		authtypes.ModuleName,
		banktypes.ModuleName,
		distrtypes.ModuleName,
		stakingtypes.ModuleName,
		slashingtypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		upgradetypes.ModuleName,
		emission.ModuleName, // after bank: checks the genesis supply
		registrytypes.ModuleName,
		transferstypes.ModuleName,
		recoverytypes.ModuleName,
		vaulttypes.ModuleName,
		markettypes.ModuleName,
		rotationtypes.ModuleName,
		consensusparamtypes.ModuleName,
	}
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(append([]string{consensusparamtypes.ModuleName}, genesisModuleOrder[:len(genesisModuleOrder)-1]...)...)

	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(err)
	}

	autocliv1.RegisterQueryServer(app.GRPCQueryRouter(), runtimeservices.NewAutoCLIQueryService(app.ModuleManager.Modules))

	reflectionSvc, err := runtimeservices.NewReflectionService()
	if err != nil {
		panic(err)
	}
	reflectionv1.RegisterReflectionServiceServer(app.GRPCQueryRouter(), reflectionSvc)

	app.MountKVStores(keys)

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.polygon = newPolygonWatch(appOpts, logger)
	app.setOracleHandlers(bApp)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.setAnteHandler(txConfig)
	app.setPostHandler()

	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			panic(fmt.Errorf("error loading last version: %w", err))
		}
	}

	return app
}

func (app *LockKeeperApp) setAnteHandler(txConfig client.TxConfig) {
	app.SetAnteHandler(newAnteHandler(ante.HandlerOptions{
		AccountKeeper:   app.AccountKeeper,
		BankKeeper:      app.BankKeeper,
		SignModeHandler: txConfig.SignModeHandler(),
		SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
	}, app.AccountKeeper, app.RecoveryKeeper, app.MarketKeeper, app.RegistryKeeper, app.RecoveryKeeper))
}

func (app *LockKeeperApp) setPostHandler() {
	postHandler, err := posthandler.NewPostHandler(posthandler.HandlerOptions{})
	if err != nil {
		panic(err)
	}
	app.SetPostHandler(postHandler)
}

// Name returns the name of the App
func (app *LockKeeperApp) Name() string { return app.BaseApp.Name() }

// PreBlocker application updates every pre block
func (app *LockKeeperApp) PreBlocker(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*sdk.ResponsePreBlock, error) {
	res, err := app.ModuleManager.PreBlock(ctx)
	if err != nil {
		return res, err
	}
	// Trades the LockKeepers confirmed paid on Polygon (T4).
	app.settlePaid(ctx, req.Txs)
	return res, nil
}

// BeginBlocker application updates every begin block
func (app *LockKeeperApp) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

// EndBlocker application updates every end block
func (app *LockKeeperApp) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.ModuleManager.EndBlock(ctx)
}

// Configurator returns the module configurator.
func (app *LockKeeperApp) Configurator() module.Configurator {
	return app.configurator
}

// InitChainer application update at chain initialization
func (app *LockKeeperApp) InitChainer(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	var genesisState GenesisState
	if err := json.Unmarshal(req.AppStateBytes, &genesisState); err != nil {
		return nil, err
	}
	if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
		return nil, err
	}
	return app.ModuleManager.InitGenesis(ctx, app.appCodec, genesisState)
}

// LoadHeight loads a particular height
func (app *LockKeeperApp) LoadHeight(height int64) error {
	return app.LoadVersion(height)
}

// LegacyAmino returns the amino codec.
func (app *LockKeeperApp) LegacyAmino() *codec.LegacyAmino { return app.legacyAmino }

// AppCodec returns the app codec.
func (app *LockKeeperApp) AppCodec() codec.Codec { return app.appCodec }

// InterfaceRegistry returns the InterfaceRegistry.
func (app *LockKeeperApp) InterfaceRegistry() types.InterfaceRegistry { return app.interfaceRegistry }

// TxConfig returns the TxConfig.
func (app *LockKeeperApp) TxConfig() client.TxConfig { return app.txConfig }

// AutoCliOpts returns the autocli options for the app.
func (app *LockKeeperApp) AutoCliOpts() autocli.AppOptions {
	modules := make(map[string]appmodule.AppModule)
	for _, m := range app.ModuleManager.Modules {
		if moduleWithName, ok := m.(module.HasName); ok {
			if appModule, ok := moduleWithName.(appmodule.AppModule); ok {
				modules[moduleWithName.Name()] = appModule
			}
		}
	}

	return autocli.AppOptions{
		Modules:               modules,
		ModuleOptions:         runtimeservices.ExtractAutoCLIOptions(app.ModuleManager.Modules),
		AddressCodec:          authcodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		ValidatorAddressCodec: authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()),
		ConsensusAddressCodec: authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ConsensusAddrPrefix()),
	}
}

// DefaultGenesis returns a default genesis from the registered modules.
func (app *LockKeeperApp) DefaultGenesis() map[string]json.RawMessage {
	return app.BasicModuleManager.DefaultGenesis(app.appCodec)
}

// GetKey returns the KVStoreKey for the provided store key.
func (app *LockKeeperApp) GetKey(storeKey string) *storetypes.KVStoreKey {
	return app.keys[storeKey]
}

// SimulationManager is not used by LockKeeper.
func (app *LockKeeperApp) SimulationManager() *module.SimulationManager { return nil }

// RegisterAPIRoutes registers all application module routes with the API server.
func (app *LockKeeperApp) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	nodeservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}

	// A wallet's history from the transaction index: see history.go.
	apiSvr.Router.Handle(PathHistory, historyHandler(func() historySource {
		if clientCtx.Client == nil {
			return nil
		}
		return clientCtx.Client
	}, &blockTimes{path: filepath.Join(clientCtx.HomeDir, "data", BlockTimesFile)})).Methods(http.MethodGet)

	// The network's chip checker, on the LockKeeper that holds its key
	// (the founder's computer first): see chip/checker.go.
	checker, err := chip.LoadChecker(filepath.Join(clientCtx.HomeDir, "config", chip.CheckerKeyFile), app.ChainID())
	if err != nil {
		panic(err)
	}
	if checker != nil {
		checker.Owner = func(code string) (string, bool, error) {
			res, err := rotationtypes.NewQueryClient(clientCtx).Chip(context.Background(), &rotationtypes.QueryChipRequest{Code: code})
			if err != nil {
				return "", false, err
			}
			return res.Account, res.Working, nil
		}
		h := checker.Handler()
		apiSvr.Router.Handle(chip.PathQuestion, h)
		apiSvr.Router.Handle(chip.PathAnswer, h)
	}
}

// RegisterTxService implements the Application.RegisterTxService method.
func (app *LockKeeperApp) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.GRPCQueryRouter(), clientCtx, app.Simulate, app.interfaceRegistry)
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *LockKeeperApp) RegisterTendermintService(clientCtx client.Context) {
	cmtApp := server.NewCometABCIWrapper(app)
	cmtservice.RegisterTendermintService(clientCtx, app.GRPCQueryRouter(), app.interfaceRegistry, cmtApp.Query)
}

// RegisterNodeService registers the node gRPC service.
func (app *LockKeeperApp) RegisterNodeService(clientCtx client.Context, cfg config.Config) {
	nodeservice.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg, func() int64 {
		return app.CommitMultiStore().EarliestVersion()
	})
}

// GetMaccPerms returns a copy of the module account permissions.
func GetMaccPerms() map[string][]string {
	return maps.Clone(maccPerms)
}

// BlockedAddresses returns the module accounts that cannot receive coins.
func BlockedAddresses() map[string]bool {
	modAccAddrs := make(map[string]bool)
	for acc := range GetMaccPerms() {
		modAccAddrs[authtypes.NewModuleAddress(acc).String()] = true
	}
	return modAccAddrs
}
