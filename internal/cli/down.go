package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/lock"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Cierra la sesión `up` viva de forma ordenada, o limpia los restos de una que murió",
	Long: `Con una sesión viva le pide que haga el snapshot final y se cierre. Si el
snapshot final falla, la sesión NO se cierra (no se pierde el trabajo sin
guardar): repetir "down" dentro del plazo que indica el aviso la cierra sin
guardar.

Si la sesión murió sin cerrar (kill -9, cuelgue), "down" desmonta lo que quedó
vivo en RAM —daemon rootless, contenedores, datos descifrados— sin esperar al
siguiente "up", y suelta el bloqueo del almacén.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		runDir, err := run.Dir()
		if err != nil {
			return err
		}
		sess, err := session.Read(runDir)
		if err != nil || !sess.Alive() {
			return cleanDead(runDir, sess)
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

// cleanDead desmonta los restos de una sesión que ya no tiene proceso `up`
// (sess puede ser nil si ni siquiera quedó session.json).
func cleanDead(runDir string, sess *session.Session) error {
	// solo se toca un directorio que exista y sea nuestro (/tmp es de todos)
	if run.Private(runDir) != nil {
		fmt.Println("· no hay ninguna sesión ni restos que limpiar")
		return nil
	}
	_, sockErr := os.Stat(run.Socket(runDir))
	_, pidErr := os.Stat(filepath.Join(runDir, "dockerd.pid"))
	if sess == nil && sockErr != nil && pidErr != nil {
		fmt.Println("· no hay ninguna sesión viva (nada que cerrar)")
		return nil
	}
	fmt.Println("· la sesión anterior murió sin cerrar (kill -9 / cuelgue): se desmonta lo que dejó en RAM")
	fmt.Println("  (lo trabajado después de su último snapshot no se puede recuperar; el próximo `up` restaura ese snapshot)")
	teardown(runDir, dock.Env(runDir))
	if sess != nil {
		if cfg, err := config.Load(); err == nil {
			if lock.Mine(cfg.VaultPath, sess.Host, sess.PID) {
				if err := lock.Release(cfg.VaultPath, sess.Host, sess.PID); err != nil {
					fmt.Fprintln(os.Stderr, "aviso: no se pudo soltar el bloqueo del almacén:", err)
				} else {
					fmt.Println("✔ bloqueo del almacén soltado")
				}
			}
		}
	}
	return nil
}
