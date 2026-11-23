//go:build !windows

package nombre

import "os"

// CarpetasSistema (fuera de Windows) solo considera la carpeta del usuario.
func CarpetasSistema() []string {
	h, _ := os.UserHomeDir()
	return []string{h}
}
