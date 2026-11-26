package cli

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Pide a una sesión `up` viva que se cierre de forma ordenada",
	RunE: func(cmd *cobra.Command, args []string) error {
		runDir, err := run.Dir()
		if err != nil {
			return err
		}
		sess, err := session.Read(runDir)
		if err != nil {
			return fmt.Errorf("no hay sesión registrada en %s", runDir)
		}
		if !sess.Alive() {
			fmt.Println("· la sesión previa ya terminó; limpio restos si los hay")
			_ = run.Cleanup(runDir)
			return nil
		}
		fmt.Printf("· enviando SIGTERM a pid %d (puerto %d)...\n", sess.PID, sess.Port)
		if err := syscall.Kill(sess.PID, syscall.SIGTERM); err != nil {
			return err
		}
		// el snapshot final de un grafo grande sobre pCloud puede tardar
		pedido := time.Now()
		deadline := pedido.Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			if !sess.Alive() {
				fmt.Println("✔ sesión cerrada ordenadamente")
				return nil
			}
			if st, err := os.Stat(finalFailedMarker(runDir)); err == nil && st.ModTime().After(pedido.Add(-time.Second)) {
				return fmt.Errorf("el snapshot final ha FALLADO y la sesión sigue abierta para no perder el trabajo sin guardar (mira la salida de `up` para ver la causa). "+
					"Arréglalo y repite `flowsint-desk down`; para cerrar SIN guardar, repite `flowsint-desk down` antes de %s", closeGrace)
			}
			time.Sleep(1 * time.Second)
		}
		return fmt.Errorf("el pid %d sigue vivo tras 5 min: mira la salida de `up` (¿snapshot final todavía en curso?)", sess.PID)
	},
}
