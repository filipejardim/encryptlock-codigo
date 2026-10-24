// Command lockkeeper runs an EncryptLock network node (LockKeeper) and its
// command-line wallet.
package main

import (
	"fmt"
	"os"

	clientv2helpers "cosmossdk.io/client/v2/helpers"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"encryptlock/lockkeeper/app"
	"encryptlock/lockkeeper/cmd/lockkeeper/cmd"
)

func main() {
	app.SetAddressPrefixes()

	rootCmd := cmd.NewRootCmd()
	if args := cmd.StartupArgs(); args != nil {
		// Started by Windows from the Microsoft Store package (no arguments).
		rootCmd.SetArgs(args)
	}
	if err := svrcmd.Execute(rootCmd, clientv2helpers.EnvPrefix, app.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}
