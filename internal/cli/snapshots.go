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
		if !sess.Ready() {
			return fmt.Errorf("la sesión (pid %d) todavía está arrancando: aún no hay nada que guardar", sess.PID)
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
				fmt.Printf("✔ snapshot creado: %s\n", short(after))
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
			if m, ok := manifestOf(se, s); ok {
				desc = fmt.Sprintf("tipo=%s nodos=%d rels=%d tablas_pg=%d", m.Kind, m.Neo4jTotalN, m.Neo4jTotalR, len(m.PGRows))
			}
			fmt.Printf("%-10s %-20s %-10s %-12s %s\n",
				s.ShortID, s.Time.Local().Format("2006-01-02 15:04:05"), s.Host,
				fmt.Sprintf("%.1f MiB", float64(s.Summary.TotalSizeProcessed)/(1024*1024)), desc)
		}
		return nil
	},
}

// manifestOf lee el manifest.json de un snapshot. El snapshot respalda UN
// directorio (el staging), así que la ruta se deduce de sus `paths` y basta un
// `restic dump`; solo si eso falla se lista el snapshot para buscarlo.
func manifestOf(se store.Env, s store.Snap) (snap.Manifest, bool) {
	var m snap.Manifest
	for _, base := range s.Paths {
		if out, err := store.Dump(se, s.ID, base+"/manifest.json"); err == nil && json.Unmarshal([]byte(out), &m) == nil {
			return m, true
		}
	}
	paths, err := store.Ls(se, s.ID)
	if err != nil {
		return m, false
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "/manifest.json") {
			if out, err := store.Dump(se, s.ID, p); err == nil && json.Unmarshal([]byte(out), &m) == nil {
				return m, true
			}
			break
		}
	}
	return m, false
}
