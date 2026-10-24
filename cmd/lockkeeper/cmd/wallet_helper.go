package cmd

import (
	"github.com/spf13/cobra"

	"encryptlock/lockkeeper/i18n"
	rotationtypes "encryptlock/lockkeeper/x/rotation/types"
)

func computerHelperCmd(help i18n.Translator) *cobra.Command {
	return withTxFlags(&cobra.Command{
		Use:   "computer-helper [name] [helper-address]",
		Short: help.T("helper.short"),
		Args:  cobra.ExactArgs(2),
		RunE:  runComputerHelper,
	})
}

// runComputerHelper lets the helper account of a computer say "I am on" for
// the LockKeeper of this wallet. Used for the network's first LockKeeper,
// which is created in the first block and so never went through the app's
// "Make this computer a blockchain LockKeeper" (that sets the helper too).
func runComputerHelper(cmd *cobra.Command, args []string) error {
	clientCtx, tr, err := clientFor(cmd)
	if err != nil {
		return err
	}
	addr, err := address(clientCtx, tr, args[0])
	if err != nil {
		return err
	}
	if _, err := broadcast(cmd, clientCtx, tr, args[0], &rotationtypes.MsgSetHelper{Keeper: addr.String(), Helper: args[1]}); err != nil {
		return err
	}
	cmd.Println(tr.T("helper.done", args[1]))
	return nil
}
