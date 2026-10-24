package cmd

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"encryptlock/lockkeeper/chip"
)

// chipCmd shows whether this computer's security chip is accepted for a
// LockKeeper (one computer = one LockKeeper).
//
// The secret test needs administrator rights on Windows (tested 9 Oct 2026:
// as a normal user the chip refuses before opening the question; with
// rights it passes through Windows' key provider and directly). The app runs
// it once with Windows' "Allow?" window and --out, and reads the result file.
//
// With --checker (the network's REST address) and --account (the computer's
// helper), the secret test is done by the network's chip checker and the
// result holds its ticket, which the helper sends to the network.
func chipCmd() *cobra.Command {
	var out, checker, account string
	c := &cobra.Command{
		Use:          "chip",
		Short:        "Check this computer's security chip (TPM 2.0): real computer, maker and the chip's code",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var maker, code string
			var ticket []byte
			var err error
			if checker != "" {
				var t *chip.Ticket
				if t, err = chip.ProveWith(strings.TrimRight(checker, "/"), account); err == nil {
					maker, code, ticket = t.Maker, t.Code, t.Ticket
				}
			} else {
				maker, code, err = checkChip()
			}
			if out != "" {
				res := map[string]any{"ok": err == nil, "maker": maker, "code": code}
				if ticket != nil {
					res["account"], res["ticket"] = account, ticket
				}
				if err != nil {
					res["error"] = err.Error()
				}
				bz, _ := json.Marshal(res)
				if werr := os.WriteFile(out, bz, 0o644); werr != nil {
					return werr
				}
			}
			if err != nil {
				return err
			}
			cmd.Println("real computer: yes")
			cmd.Println("chip maker:", maker)
			cmd.Println("chip code:", code)
			cmd.Println("secret test: passed")
			return nil
		},
	}
	c.Flags().StringVar(&out, "out", "", "also write the result as JSON to this file")
	c.Flags().StringVar(&checker, "checker", "", "the network's REST address: its chip checker does the secret test and signs a ticket")
	c.Flags().StringVar(&account, "account", "", "with --checker: the account the ticket is for (this computer's helper)")
	return c
}

// checkChip reads the chip, checks its maker's certificate and does the
// secret test, both sides on this computer.
func checkChip() (maker, code string, err error) {
	c, err := chip.Read()
	if err != nil {
		return "", "", err
	}
	if maker, err = chip.Check(c); err != nil {
		return "", "", err
	}
	code = c.Code()
	a, ak, err := chip.NewAnswerer()
	if err != nil {
		return maker, code, err
	}
	defer a.Close()
	want, q, err := chip.Ask(c, ak)
	if err != nil {
		return maker, code, err
	}
	got, err := a.Answer(q)
	if err != nil {
		return maker, code, err
	}
	return maker, code, chip.Right(want, got)
}
