package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/lock"

	"github.com/spf13/cobra"
)

var unlockForce bool

var unlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Rompe un bloqueo caducado de otro host (--force con confirmación)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		l, err := lock.Read(cfg.VaultPath)
		if err != nil {
			return fmt.Errorf("no hay ningún bloqueo en %s", cfg.VaultPath)
		}
		host, _ := os.Hostname()
		hb, _ := time.Parse(time.RFC3339, l.Heartbeat)
		stale := time.Since(hb) >= lock.StaleMinutes*time.Minute
		fmt.Printf("bloqueo actual: host=%s pid=%d boot=%s latido hace %s (%s)\n",
			l.Host, l.PID, l.BootID[:8], time.Since(hb).Round(time.Second),
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
}
