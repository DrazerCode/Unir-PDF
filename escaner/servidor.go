// Package escaner sirve, dentro de la red local del hospital, una página web
// para escanear documentos con el teléfono (Android o iPhone) y recibe el
// resultado como un PDF con el nombre del paciente.
//
// Todo el procesamiento de las fotos (recorte, enderezado, filtros) ocurre en
// el propio teléfono; aquí solo se reciben las páginas ya listas en JPEG, se
// convierten a PDF y se guardan en la carpeta de destino. No se usa internet:
// la página y todo su código van incrustados en el .exe.
package escaner

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"unirpdf/conversor"
)

//go:embed web
var webFS embed.FS

// Puertos que se prueban en orden; el primero libre es el que se usa.
var Puertos = []int{8080, 8081, 8082, 8083, 8084, 8085, 8086, 8087, 8088, 8089}

const (
	maxSubida  = 300 << 20 // 300 MB por envío
	maxPaginas = 200
)

// Servidor es el servidor web del escáner.
type Servidor struct {
	Destino   string                      // carpeta donde se crean las subcarpetas de cada paciente
	AlRecibir func(ruta, paciente string) // se llama (en otra goroutine) al guardar un PDF
	Equipo    string                      // nombre del PC que se muestra en la página

	mu     sync.Mutex // serializa la escritura de archivos
	srv    *http.Server
	puerto int
}

// Iniciar abre el servidor en el primer puerto libre de Puertos y lo deja
// atendiendo en segundo plano.
func (s *Servidor) Iniciar() error {
	var ln net.Listener
	var err error
	for _, p := range Puertos {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err == nil {
			s.puerto = p
			break
		}
	}
	if ln == nil {
		return fmt.Errorf("no hay ningún puerto libre (%d-%d): %v", Puertos[0], Puertos[len(Puertos)-1], err)
	}
	s.srv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go s.srv.Serve(ln)
	return nil
}

// Cerrar detiene el servidor.
func (s *Servidor) Cerrar() {
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
	}
}

// Puerto devuelve el puerto en uso (0 si no se ha iniciado).
func (s *Servidor) Puerto() int { return s.puerto }

// URLs devuelve las direcciones con las que se puede abrir la página desde
// otro equipo de la red, empezando por las de redes privadas (192.168.x.x,
// 10.x.x.x, 172.16-31.x.x).
func (s *Servidor) URLs() []string {
	var priv, otras []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		u := fmt.Sprintf("http://%s:%d", ipn.IP.String(), s.puerto)
		if ipn.IP.IsPrivate() {
			priv = append(priv, u)
		} else {
			otras = append(otras, u)
		}
	}
	return append(priv, otras...)
}

// Handler devuelve el manejador HTTP (útil también para pruebas).
func (s *Servidor) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	archivos := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Sin caché agresiva: así una versión nueva del .exe se nota al recargar.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		archivos.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /api/estado", func(w http.ResponseWriter, r *http.Request) {
		responder(w, http.StatusOK, map[string]any{"ok": true, "equipo": s.Equipo})
	})
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		responder(w, http.StatusOK, map[string]any{"ok": true, "app": "UnirPDF", "equipo": s.Equipo})
	})
	mux.HandleFunc("POST /api/escaneo", s.recibir)
	mux.HandleFunc("POST /api/subir", s.recibirPDF)
	return mux
}

func responder(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fallo(w http.ResponseWriter, code int, msg string) {
	responder(w, code, map[string]any{"ok": false, "error": msg})
}

// leerFormulario lee el envío multipart y el nombre del paciente. Si algo
// falla ya respondió al cliente y devuelve "".
func leerFormulario(w http.ResponseWriter, r *http.Request) string {
	r.Body = http.MaxBytesReader(w, r.Body, maxSubida)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			fallo(w, http.StatusRequestEntityTooLarge, "El envío es demasiado grande.")
			return ""
		}
		fallo(w, http.StatusBadRequest, "Envío incompleto o dañado. Inténtalo de nuevo.")
		return ""
	}
	paciente := LimpiarNombre(r.FormValue("paciente"))
	if paciente == "" {
		r.MultipartForm.RemoveAll()
		fallo(w, http.StatusBadRequest, "Falta el nombre del paciente.")
	}
	return paciente
}

// recibir guarda un escaneo hecho en la página web: campo "paciente" y una o
// más imágenes JPEG en el campo "paginas", en el orden en que deben quedar.
func (s *Servidor) recibir(w http.ResponseWriter, r *http.Request) {
	paciente := leerFormulario(w, r)
	if paciente == "" {
		return
	}
	defer r.MultipartForm.RemoveAll()
	paginas := r.MultipartForm.File["paginas"]
	if len(paginas) == 0 {
		fallo(w, http.StatusBadRequest, "No se recibió ninguna página.")
		return
	}
	if len(paginas) > maxPaginas {
		fallo(w, http.StatusBadRequest, fmt.Sprintf("Demasiadas páginas (máximo %d).", maxPaginas))
		return
	}

	tmp, err := os.MkdirTemp("", "UnirPDF-escaneo-")
	if err != nil {
		fallo(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tmp)
	unido, err := unirPaginas(tmp, paginas)
	if err == nil {
		err = validarPDF(unido)
	}
	s.terminar(w, paciente, unido, err)
}

// recibirPDF guarda un PDF ya armado (p. ej. por la app de Android): campo
// "paciente" y el archivo en el campo "pdf".
func (s *Servidor) recibirPDF(w http.ResponseWriter, r *http.Request) {
	paciente := leerFormulario(w, r)
	if paciente == "" {
		return
	}
	defer r.MultipartForm.RemoveAll()
	fhs := r.MultipartForm.File["pdf"]
	if len(fhs) != 1 {
		fallo(w, http.StatusBadRequest, "Se esperaba un archivo PDF en el campo \"pdf\".")
		return
	}
	tmp, err := os.MkdirTemp("", "UnirPDF-escaneo-")
	if err != nil {
		fallo(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tmp)
	pdf := filepath.Join(tmp, "recibido.pdf")
	if err := copiarSubida(fhs[0], pdf, []byte("%PDF-"), "no es un archivo PDF"); err != nil {
		fallo(w, http.StatusBadRequest, "El archivo recibido "+err.Error()+".")
		return
	}
	if err := validarPDF(pdf); err != nil {
		fallo(w, http.StatusBadRequest, "El PDF recibido está dañado: "+err.Error())
		return
	}
	s.terminar(w, paciente, pdf, nil)
}

// terminar mueve el PDF a su carpeta definitiva y responde al cliente.
func (s *Servidor) terminar(w http.ResponseWriter, paciente, pdf string, err error) {
	var ruta string
	if err == nil {
		ruta, err = s.guardar(paciente, pdf)
	}
	if err != nil {
		fallo(w, http.StatusInternalServerError, "No se pudo guardar el PDF: "+err.Error())
		return
	}
	paginas, _ := api.PageCountFile(context.Background(), ruta)
	if s.AlRecibir != nil {
		go s.AlRecibir(ruta, paciente)
	}
	responder(w, http.StatusOK, map[string]any{
		"ok":      true,
		"archivo": filepath.Base(ruta),
		"carpeta": filepath.Base(filepath.Dir(ruta)),
		"paginas": paginas,
	})
}

func validarPDF(p string) error {
	n, err := api.PageCountFile(context.Background(), p)
	if err == nil && n == 0 {
		err = errors.New("no tiene páginas")
	}
	return err
}

// unirPaginas convierte cada JPEG en una página A4 y las une en un PDF dentro de tmp.
func unirPaginas(tmp string, paginas []*multipart.FileHeader) (string, error) {
	pdfs := make([]string, len(paginas))
	for i, fh := range paginas {
		jpg := filepath.Join(tmp, fmt.Sprintf("pagina%03d.jpg", i+1))
		if err := copiarSubida(fh, jpg, []byte{0xFF, 0xD8, 0xFF}, "no es una imagen JPEG"); err != nil {
			return "", fmt.Errorf("página %d: %v", i+1, err)
		}
		pdfs[i] = filepath.Join(tmp, fmt.Sprintf("pagina%03d.pdf", i+1))
		if err := conversor.JPGaPDF(jpg, pdfs[i]); err != nil {
			return "", fmt.Errorf("página %d: %v", i+1, err)
		}
	}
	if len(pdfs) == 1 {
		return pdfs[0], nil
	}
	unido := filepath.Join(tmp, "resultado.pdf")
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	if err := api.MergeCreateFile(context.Background(), pdfs, unido, false, conf); err != nil {
		return "", err
	}
	return unido, nil
}

// guardar copia el PDF a Destino\<paciente>\<paciente>.pdf (o
// "<paciente> (2).pdf"... si ya existe) y devuelve la ruta final.
func (s *Servidor) guardar(paciente, pdf string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.Destino, paciente)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, paciente+".pdf")
	for n := 2; existe(dst); n++ {
		dst = filepath.Join(dir, fmt.Sprintf("%s (%d).pdf", paciente, n))
	}
	// Se escribe primero con otro nombre y luego se renombra, para que Unir PDF
	// nunca vea un archivo a medio escribir.
	parcial := dst + ".parcial"
	if err := copiarArchivo(pdf, parcial); err != nil {
		os.Remove(parcial)
		return "", err
	}
	if err := os.Rename(parcial, dst); err != nil {
		os.Remove(parcial)
		return "", err
	}
	return dst, nil
}

func existe(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// copiarSubida guarda un archivo recibido comprobando que empiece por firma.
func copiarSubida(fh *multipart.FileHeader, dst string, firma []byte, motivo string) error {
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	cab := make([]byte, len(firma))
	if _, err := io.ReadFull(f, cab); err != nil || string(cab) != string(firma) {
		return errors.New(motivo)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func copiarArchivo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// Nombres que Windows no permite para archivos o carpetas.
var reservados = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

func init() {
	for i := 1; i <= 9; i++ {
		reservados["COM"+strconv.Itoa(i)] = true
		reservados["LPT"+strconv.Itoa(i)] = true
	}
}

// LimpiarNombre convierte el nombre escrito en el teléfono en un nombre de
// archivo válido en Windows: quita los caracteres prohibidos (\ / : * ? " < > |),
// junta los espacios repetidos y lo limita a 80 caracteres. Devuelve "" si no
// queda nada útil.
func LimpiarNombre(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune(`\/:*?"<>|`, r), unicode.IsControl(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	n := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(n); len(r) > 80 {
		n = string(r[:80])
	}
	n = strings.TrimRight(n, ". ")
	if n == "" || strings.Trim(n, ".") == "" || reservados[strings.ToUpper(n)] {
		return ""
	}
	return n
}
