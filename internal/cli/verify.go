package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/snap"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify [ID]",
	Short: "Comprueba que el repositorio y un snapshot (por defecto el último) están íntegros, sin abrir sesión",
	Long: `Sin levantar Flowsint ni Docker: ejecuta "restic check" sobre el almacén,
restaura el snapshot a la tmpfs, comprueba que trae todos sus ficheros y que su
sha256 coincide con el manifest, y lo borra.

Responde a "¿mi copia está entera y se puede leer con esta frase?". NO carga
los volcados en Postgres/Neo4j: esa prueba completa la hace "selftest" (con un
almacén desechable) o el propio "up", que verifica los recuentos al restaurar.`,
	Args: cobra.MaximumNArgs(1),
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
		fmt.Println("✔ la frase abre el almacén:", cfg.VaultPath)
		fmt.Println("· comprobando la estructura del repositorio (restic check)...")
		if err := store.Check(se); err != nil {
			return err
		}
		fmt.Println("✔ repositorio íntegro")
		snaps, err := store.Snapshots(se)
		if err != nil {
			return err
		}
		if len(snaps) == 0 {
			return fmt.Errorf("el almacén no tiene ningún snapshot")
		}
		elegido := snaps[len(snaps)-1]
		if len(args) == 1 {
			ok := false
			for _, s := range snaps {
				if len(args[0]) >= 4 && len(s.ID) >= len(args[0]) && s.ID[:len(args[0])] == args[0] {
					elegido, ok = s, true
				}
			}
			if !ok {
				return fmt.Errorf("no hay ningún snapshot cuyo ID empiece por %q (mínimo 4 caracteres; ver `flowsint-desk snapshots`)", args[0])
			}
		}
		// los volcados van en claro: solo a la tmpfs del run dir, y se borran siempre
		dir := filepath.Join(runDir, fmt.Sprintf("verify.%d", os.Getpid()))
		defer os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := store.Restore(se, elegido.ID, dir); err != nil {
			return err
		}
		m, err := snap.LoadAndVerify(dir)
		if err != nil {
			return err
		}
		fmt.Printf("✔ snapshot %s íntegro (%s, host %s, hace %s): tipo=%s, %d nodos, %d relaciones, %d tablas pg, sha256 de %d ficheros OK\n",
			elegido.ShortID, elegido.Time.Local().Format("2006-01-02 15:04"), elegido.Host,
			time.Since(elegido.Time).Round(time.Minute), m.Kind, m.Neo4jTotalN, m.Neo4jTotalR, len(m.PGRows), len(m.SHA256))
		return nil
	},
}
