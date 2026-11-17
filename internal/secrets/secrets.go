// Package secrets genera y gestiona los secretos internos de Flowsint
// (AUTH_SECRET, MASTER_VAULT_KEY_V1, contraseñas de Postgres y Neo4j).
// Viven solo en ficheros 0600 dentro del directorio de ejecución (tmpfs)
// y viajan cifrados dentro del snapshot restic.
package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strings"
)

func hexRand(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out), nil
}

// Generate crea un conjunto nuevo de secretos.
func Generate() (map[string]string, error) {
	pgPass, err := hexRand(16)
	if err != nil {
		return nil, err
	}
	neoPass, err := hexRand(16)
	if err != nil {
		return nil, err
	}
	authSecret, err := hexRand(32)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return map[string]string{
		"POSTGRES_USER":         "flowsint",
		"POSTGRES_DB":           "flowsint",
		"POSTGRES_PASSWORD":     pgPass,
		"NEO4J_USERNAME":        "neo4j",
		"NEO4J_PASSWORD":        neoPass,
		"AUTH_SECRET":           authSecret,
		"MASTER_VAULT_KEY_V1":   "base64:" + base64.StdEncoding.EncodeToString(key),
	}, nil
}

// WriteEnv escribe un mapa KEY=VAL a path con permisos 0600, ordenado.
func WriteEnv(path string, m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", k, m[k])
	}
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

// ReadEnv lee un fichero KEY=VAL.
func ReadEnv(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		m[k] = v
	}
	return m, nil
}
