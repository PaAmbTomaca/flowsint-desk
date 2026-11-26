package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"
	"flowsint-desk/internal/snap"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Snapshot inmediato de una sesión en marcha (vía SIGUSR1)",
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
		if err != nil || !sess.Alive() {
			return fmt.Errorf("no hay ninguna sesión `up` viva")
		}
		se, limpiar, err := openStore(cfg, runDir)
		if err != nil {
			return err
		}
		defer limpiar()
		before, _ := store.Latest(se)
		fmt.Printf("· pidiendo snapshot a pid %d...\n", sess.PID)
		if err := syscall.Kill(sess.PID, syscall.SIGUSR1); err != nil {
			return err
		}
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			after, err := store.Latest(se)
			if err == nil && after != before && after != "" {
				fmt.Printf("✔ snapshot creado: %s\n", after[:8])
				return nil
			}
			time.Sleep(2 * time.Second)
		}
		return fmt.Errorf("no apareció ningún snapshot nuevo en 180 s (mira el log de `up`)")
	},
}

var snapshotsCmd = &cobra.Command{
	Use:   "snapshots",
	Short: "Lista los snapshots (fecha, host, tamaño, recuentos del manifest)",
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
		snaps, err := store.Snapshots(se)
		if err != nil {
			return err
		}
		fmt.Printf("%-10s %-20s %-10s %-12s %s\n", "ID", "FECHA", "HOST", "TAMAÑO", "CONTENIDO")
		for i := len(snaps) - 1; i >= 0; i-- {
			s := snaps[i]
			tags := strings.Join(s.Tags, ",")
			desc := tags
			// leer manifest.json del snapshot para los recuentos
			paths, err := store.Ls(se, s.ID)
			if err == nil {
				for _, p := range paths {
					if strings.HasSuffix(p, "/manifest.json") || p == "manifest.json" {
						if out, err := store.Dump(se, s.ID, p); err == nil {
							var m snap.Manifest
							if json.Unmarshal([]byte(out), &m) == nil {
								desc = fmt.Sprintf("tipo=%s nodos=%d rels=%d tablas_pg=%d", m.Kind, m.Neo4jTotalN, m.Neo4jTotalR, len(m.PGRows))
							}
						}
						break
					}
				}
			}
			fmt.Printf("%-10s %-20s %-10s %-12s %s\n",
				s.ShortID, s.Time.Local().Format("2006-01-02 15:04:05"), s.Host,
				fmt.Sprintf("%.1f MiB", float64(s.Summary.TotalSizeProcessed)/(1024*1024)), desc)
		}
		return nil
	},
}
