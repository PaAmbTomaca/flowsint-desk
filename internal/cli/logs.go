package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/secrets"

	"github.com/spf13/cobra"
)

var logsTail int

// redactValues reúne los valores a enmascarar (secretos del runDir + frase de
// paso) sin devolverlos nunca: solo se usan para sustituirlos por "***".
func redactValues(runDir string) []string {
	var vals []string
	if sec, err := secrets.ReadEnv(filepath.Join(runDir, "secrets.env")); err == nil {
		for k, v := range sec {
			// usuario y nombre de BD ("flowsint", "neo4j") no son secretos:
			// enmascararlos convertía cada "flowsint" de los logs en ***
			if !isSecretKey(k) || len(v) < 8 {
				continue
			}
			vals = append(vals, v)
			// la clave maestra va como "base64:<clave>": enmascarar también la
			// clave suelta, que es como la manejaría (y podría loguear) la app
			if raw, ok := strings.CutPrefix(v, "base64:"); ok && len(raw) >= 8 {
				vals = append(vals, raw)
			}
		}
	}
	// la frase puede estar en `passphrase` (la deja init) o solo en
	// `restic-pass` (frase venida del llavero o del prompt)
	for _, f := range []string{"passphrase", "restic-pass"} {
		if b, err := os.ReadFile(filepath.Join(runDir, f)); err == nil {
			if p := strings.TrimRight(string(b), "\r\n"); len(p) >= 8 {
				vals = append(vals, p)
			}
		}
	}
	// los más largos primero: si un valor contiene a otro, no deja restos
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	return vals
}

// isSecretKey distingue los valores secretos de secrets.env de los que son
// simple configuración (usuario, nombre de BD).
func isSecretKey(k string) bool {
	for _, s := range []string{"PASSWORD", "SECRET", "KEY"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

func redactText(s string, vals []string) string {
	for _, v := range vals {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

// dumpContainerLogs vuelca a stderr los logs (tail) de los contenedores dados,
// saneados: cualquier secreto o la frase de paso aparece como ***.
func dumpContainerLogs(env []string, runDir string, names []string, tail int) {
	vals := redactValues(runDir)
	for _, n := range names {
		fmt.Fprintf(os.Stderr, "── logs de %s (tail %d) ──\n", n, tail)
		out, err := dock.Logs(env, n, tail)
		if err != nil {
			fmt.Fprintf(os.Stderr, "(no se pudieron leer los logs: %v)\n", err)
			continue
		}
		fmt.Fprint(os.Stderr, redactText(out, vals))
		if !strings.HasSuffix(out, "\n") {
			fmt.Fprintln(os.Stderr)
		}
	}
}

var logsCmd = &cobra.Command{
	Use:   "logs [--tail N] [filtro]",
	Short: "Logs de los contenedores de la sesión viva, saneados (secretos → ***)",
	Long: `Muestra los logs (json-file, dentro del data-root en tmpfs) de los
contenedores flowsint-desk-* de la sesion "up" viva. Antes de imprimirlos se
enmascaran los secretos de Flowsint y la frase de paso (***): los avisos y logs
de la CLI nunca son comprometedores. Con [filtro] solo los contenedores cuyo
nombre lo contenga (p. ej. "logs api" -> flowsint-desk-api).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runDir, err := run.Dir()
		if err != nil {
			return err
		}
		sess, err := sessionRead(runDir)
		if err != nil || !sess.Alive() {
			return fmt.Errorf("no hay ninguna sesion 'up' viva (mira 'flowsint-desk status')")
		}
		env := dock.Env(runDir)
		out, err := dock.Run(env, "docker", "ps", "--format", "{{.Names}}")
		if err != nil {
			return fmt.Errorf("docker ps: %v: %s", err, redactText(out, redactValues(runDir)))
		}
		filtro := ""
		if len(args) == 1 {
			filtro = args[0]
		}
		var names []string
		for _, n := range strings.Split(out, "\n") {
			n = strings.TrimSpace(n)
			if strings.HasPrefix(n, "flowsint-desk-") && (filtro == "" || strings.Contains(n, filtro)) {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			return fmt.Errorf("no hay contenedores flowsint-desk-* vivos (filtro %q)", filtro)
		}
		vals := redactValues(runDir)
		for _, n := range names {
			fmt.Printf("── logs de %s (tail %d) ──\n", n, logsTail)
			lout, lerr := dock.Logs(env, n, logsTail)
			if lerr != nil {
				fmt.Printf("(no se pudieron leer los logs: %v)\n", lerr)
				continue
			}
			fmt.Print(redactText(lout, vals))
			if !strings.HasSuffix(lout, "\n") {
				fmt.Println()
			}
		}
		return nil
	},
}

func init() {
	logsCmd.Flags().IntVar(&logsTail, "tail", 200, "número de líneas por contenedor")
}
