// Package nombre propone el nombre del PDF combinado según dónde están los archivos.
//
// Regla: se busca la carpeta más profunda que contiene a todos los archivos.
//   - Si es una carpeta "de sistema" (Escritorio, Descargas, Documentos, la
//     carpeta del usuario, la raíz de un disco...) o los archivos están en
//     discos distintos, los archivos están "sueltos" y se propone "PDF combinados".
//   - Si no, se usa el nombre de esa carpeta. Así, si todo está en
//     Hospital Castro\Martin Paves Ibaceta, el nombre es "Martin Paves Ibaceta"
//     (la subcarpeta tiene prioridad sobre la carpeta).
package nombre

import (
	"path/filepath"
	"strings"
)

// PorDefecto es el nombre cuando los archivos están sueltos.
const PorDefecto = "PDF combinados"

// Nombres de carpetas que se consideran "sueltas" aunque no estén en la lista
// de carpetas del sistema (p. ej. redirigidas a OneDrive).
var nombresGenericos = map[string]bool{
	"escritorio": true, "desktop": true,
	"descargas": true, "downloads": true,
	"documentos": true, "documents": true, "mis documentos": true,
	"imágenes": true, "imagenes": true, "pictures": true,
	"onedrive": true, "users": true, "usuarios": true,
}

// Propuesto devuelve el nombre sugerido (sin ".pdf") y la carpeta común de los
// archivos ("" si no la hay). sistema son las carpetas que cuentan como "sueltas".
func Propuesto(paths []string, sistema []string) (nom, dir string) {
	dir = CarpetaComun(paths)
	if dir == "" || esGenerica(dir, sistema) {
		return PorDefecto, dir
	}
	n := filepath.Base(dir)
	if n == "" || n == "." || n == string(filepath.Separator) {
		return PorDefecto, dir
	}
	return n, dir
}

// CarpetaComun devuelve la carpeta más profunda que contiene a todos los
// archivos, o "" si no comparten ninguna (p. ej. discos distintos).
func CarpetaComun(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	c := filepath.Clean(filepath.Dir(paths[0]))
	for _, p := range paths[1:] {
		d := filepath.Clean(filepath.Dir(p))
		for !contiene(c, d) {
			up := filepath.Dir(c)
			if up == c {
				return ""
			}
			c = up
		}
	}
	return c
}

// contiene indica si dir es igual a sub o un antecesor suyo (sin distinguir mayúsculas, como Windows).
func contiene(dir, sub string) bool {
	if strings.EqualFold(dir, sub) {
		return true
	}
	pre := dir
	if !strings.HasSuffix(pre, string(filepath.Separator)) {
		pre += string(filepath.Separator)
	}
	return len(sub) > len(pre) && strings.EqualFold(sub[:len(pre)], pre)
}

func esGenerica(dir string, sistema []string) bool {
	if filepath.Dir(dir) == dir { // raíz de un disco
		return true
	}
	for _, s := range sistema {
		if s != "" && strings.EqualFold(filepath.Clean(s), dir) {
			return true
		}
	}
	return nombresGenericos[strings.ToLower(filepath.Base(dir))]
}
