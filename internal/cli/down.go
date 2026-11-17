package cli

import (
	"fmt"
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
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if !sess.Alive() {
				fmt.Println("✔ sesión cerrada ordenadamente")
				return nil
			}
			time.Sleep(1 * time.Second)
		}
		return fmt.Errorf("el pid %d no terminó en 90 s", sess.PID)
	},
}
