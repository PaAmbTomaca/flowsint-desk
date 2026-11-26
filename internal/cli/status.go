package cli

import (
	"fmt"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/lock"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Estado: corriendo o no, bloqueo (host, antigüedad del latido), último snapshot y su tamaño",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		runDir, err := run.Dir()
		if err != nil {
			return err
		}
		sess, err := session.Read(runDir)
		if err == nil && sess.Alive() && !sess.Ready() {
			fmt.Printf("estado: ARRANCANDO (pid %d; todavía sin interfaz)\n", sess.PID)
		} else if err == nil && sess.Alive() {
			st, _ := time.Parse(time.RFC3339, sess.Started)
			fmt.Printf("estado: CORRIENDO\n  pid: %d\n  host: %s (boot %s)\n  url: http://127.0.0.1:%d\n  arrancado: %s (hace %s)\n",
				sess.PID, sess.Host, short(sess.BootID), sess.Port, sess.Started, time.Since(st).Round(time.Second))
		} else if err == nil {
			fmt.Println("estado: NO corriendo (hay restos de una sesión muerta en", runDir+")")
		} else {
			fmt.Println("estado: NO corriendo")
		}
		// bloqueo en el almacén
		if l, err := lock.Read(cfg.VaultPath); err == nil {
			hb, _ := time.Parse(time.RFC3339, l.Heartbeat)
			fmt.Printf("bloqueo: host=%s pid=%d latido hace %s (caducado: %v)\n",
				l.Host, l.PID, time.Since(hb).Round(time.Second), time.Since(hb) >= lock.StaleMinutes*time.Minute)
		} else {
			fmt.Println("bloqueo: ninguno")
		}
		// salud de las copias
		edad := func(ts string) string {
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return "nunca"
			}
			return "hace " + time.Since(t).Round(time.Hour).String()
		}
		fmt.Printf("integridad del almacén: comprobada %s", edad(cfg.LastVerify))
		if cfg.VerifyEveryDays > 0 {
			fmt.Printf(" (se comprueba sola cada %d días al arrancar)\n", cfg.VerifyEveryDays)
		} else {
			fmt.Println(" (comprobación automática desactivada)")
		}
		if cfg.MirrorPath != "" {
			fmt.Printf("segunda copia: %s, actualizada %s\n", cfg.MirrorPath, edad(cfg.LastMirror))
		} else {
			fmt.Println("segunda copia: ninguna (todo depende del almacén; ver `flowsint-desk mirror`)")
		}
		// último snapshot
		runDir2, err := run.Ensure()
		if err == nil {
			if se, limpiar, err := openStore(cfg, runDir2); err == nil {
				defer limpiar()
				if snaps, err := store.Snapshots(se); err == nil && len(snaps) > 0 {
					last := snaps[len(snaps)-1]
					fmt.Printf("último snapshot: %s (%s, host %s, %.1f MiB, tags %v)\n",
						last.ShortID, last.Time.Local().Format(time.RFC3339), last.Host,
						float64(last.Summary.TotalSizeProcessed)/(1024*1024), last.Tags)
				} else if err == nil {
					fmt.Println("último snapshot: ninguno todavía")
				}
			} else {
				fmt.Println("último snapshot: no se pudo leer el almacén:", err)
			}
		}
		return nil
	},
}
