package nombre

import (
	"os"

	"golang.org/x/sys/windows"
)

// CarpetasSistema devuelve las carpetas de Windows donde los archivos se
// consideran "sueltos": perfil del usuario, Escritorio, Descargas, Documentos,
// Imágenes y el Escritorio público (con sus rutas reales, aunque estén en OneDrive).
func CarpetasSistema() []string {
	var out []string
	for _, id := range []*windows.KNOWNFOLDERID{
		windows.FOLDERID_Profile, windows.FOLDERID_Desktop, windows.FOLDERID_Downloads,
		windows.FOLDERID_Documents, windows.FOLDERID_Pictures, windows.FOLDERID_PublicDesktop,
		windows.FOLDERID_PublicDocuments, windows.FOLDERID_PublicDownloads,
	} {
		if p, err := windows.KnownFolderPath(id, 0); err == nil {
			out = append(out, p)
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		out = append(out, h)
	}
	return out
}
