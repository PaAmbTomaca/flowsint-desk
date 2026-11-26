package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/lock"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var unlockForce, unlockRestic bool

var unlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Rompe un bloqueo caducado de otro equipo (--force), o los bloqueos huérfanos de restic (--restic)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if unlockRestic {
			// bloqueos internos del repo restic que deja un kill -9 en mitad de
			// un backup/forget; `restic unlock` solo quita los huérfanos
			runDir, err := run.Ensure()
			if err != nil {
				return err
			}
			se, limpiar, err := openStore(cfg, runDir)
			if err != nil {
				return err
			}
			defer limpiar()
			if err := store.Unlock(se); err != nil {
				return err
			}
			fmt.Println("✔ bloqueos huérfanos de restic eliminados (los de procesos vivos no se tocan)")
			return nil
		}
		l, err := lock.Read(cfg.VaultPath)
		if err != nil {
			return fmt.Errorf("no hay ningún bloqueo en %s", cfg.VaultPath)
		}
		host, _ := os.Hostname()
		hb, _ := time.Parse(time.RFC3339, l.Heartbeat)
		stale := time.Since(hb) >= lock.StaleMinutes*time.Minute
		fmt.Printf("bloqueo actual: host=%s pid=%d boot=%s latido hace %s (%s)\n",
			l.Host, l.PID, short(l.BootID), time.Since(hb).Round(time.Second),
			map[bool]string{true: "CADUCADO", false: "reciente"}[stale])
		if lock.IsLocal(l, host) {
			return fmt.Errorf("el bloqueo es de ESTE host (pid %d); si la sesión murió, el siguiente `up` lo limpia solo", l.PID)
		}
		if !stale && !unlockForce {
			return fmt.Errorf("el latido es reciente (< %d min): usa --force solo si estás seguro de que ese equipo está apagado", lock.StaleMinutes)
		}
		if unlockForce {
			fmt.Fprintf(os.Stderr, "¿Seguro que quieres romper el bloqueo de %s? [escribe SI]: ", l.Host)
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimSpace(line) != "SI" {
				return fmt.Errorf("cancelado")
			}
		}
		if err := lock.ForceRemove(cfg.VaultPath); err != nil {
			return err
		}
		fmt.Println("✔ bloqueo eliminado")
		return nil
	},
}

func init() {
	unlockCmd.Flags().BoolVar(&unlockForce, "force", false, "forzar aunque el latido no esté caducado")
	unlockCmd.Flags().BoolVar(&unlockRestic, "restic", false, "quitar los bloqueos huérfanos del repositorio restic (tras un kill -9 en mitad de un backup)")
}
