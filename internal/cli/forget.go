package cli

import (
	"fmt"
	"strings"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var forgetCmd = &cobra.Command{
	Use:   "forget",
	Short: "Aplica la retención (por defecto 24/h, 30/d, 12/m) + prune",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		runDir, err := run.Ensure()
		if err != nil {
			return err
		}
		se, limpiar, err := openStore(cfg, runDir)
		if err != nil {
			return err
		}
		defer limpiar()
		fmt.Printf("· retención: keep-hourly=%d keep-daily=%d keep-monthly=%d + prune\n",
			cfg.KeepHourly, cfg.KeepDaily, cfg.KeepMonthly)
		out, err := store.Forget(se, cfg.KeepHourly, cfg.KeepDaily, cfg.KeepMonthly, true)
		if err != nil {
			return err
		}
		cfg.MarkForgotten()
		_ = cfg.Save()
		fmt.Println(strings.TrimSpace(out))
		return nil
	},
}
