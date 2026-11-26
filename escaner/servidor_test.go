package escaner

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestLimpiarNombre(t *testing.T) {
	casos := map[string]string{
		"  Juan   Pérez  ":     "Juan Pérez",
		`Ana/Gómez:"x"?`:       "Ana Gómez x",
		"...":                  "",
		"con":                  "",
		"María José. ":         "María José",
		"a\x00b\nc":            "a b c",
		"Martin Paves Ibaceta": "Martin Paves Ibaceta",
		"":                     "",
	}
	for in, want := range casos {
		if got := LimpiarNombre(in); got != want {
			t.Errorf("LimpiarNombre(%q) = %q, quiero %q", in, got, want)
		}
	}
}

func jpegDePrueba(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func enviar(t *testing.T, h http.Handler, ruta, paciente, campo string, archivos ...[]byte) (int, map[string]any) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("paciente", paciente)
	for i, a := range archivos {
		fw, _ := mw.CreateFormFile(campo, "f"+string(rune('a'+i)))
		fw.Write(a)
	}
	mw.Close()
	req := httptest.NewRequest("POST", ruta, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var j map[string]any
	json.Unmarshal(rec.Body.Bytes(), &j)
	return rec.Code, j
}

func TestEscaneo(t *testing.T) {
	dir := t.TempDir()
	recibidos := make(chan string, 4)
	s := &Servidor{Destino: dir, AlRecibir: func(r, p string) { recibidos <- r }}
	h := s.Handler()

	code, j := enviar(t, h, "/api/escaneo", "Juan Pérez", "paginas", jpegDePrueba(t, 300, 400), jpegDePrueba(t, 400, 300), jpegDePrueba(t, 200, 280))
	if code != 200 || j["archivo"] != "Juan Pérez.pdf" || j["paginas"] != float64(3) {
		t.Fatalf("respuesta inesperada: %d %v", code, j)
	}
	ruta := filepath.Join(dir, "Juan Pérez", "Juan Pérez.pdf")
	if n, err := api.PageCountFile(context.Background(), ruta); err != nil || n != 3 {
		t.Fatalf("PDF: %d páginas, %v", n, err)
	}
	if r := <-recibidos; r != ruta {
		t.Fatalf("AlRecibir con %q", r)
	}

	// Segundo envío del mismo paciente: no se sobrescribe.
	code, j = enviar(t, h, "/api/escaneo", "Juan Pérez", "paginas", jpegDePrueba(t, 300, 400))
	if code != 200 || j["archivo"] != "Juan Pérez (2).pdf" {
		t.Fatalf("respuesta inesperada: %d %v", code, j)
	}

	// PDF ya armado (app Android).
	pdf, _ := os.ReadFile(ruta)
	code, j = enviar(t, h, "/api/subir", "Ana Soto", "pdf", pdf)
	if code != 200 || j["archivo"] != "Ana Soto.pdf" || j["paginas"] != float64(3) {
		t.Fatalf("subir: %d %v", code, j)
	}

	// Errores.
	if code, _ := enviar(t, h, "/api/escaneo", "  ", "paginas", jpegDePrueba(t, 10, 10)); code != 400 {
		t.Errorf("sin paciente: %d", code)
	}
	if code, _ := enviar(t, h, "/api/escaneo", "X", "paginas", []byte("hola")); code != 500 && code != 400 {
		t.Errorf("no JPEG: %d", code)
	}
	if code, _ := enviar(t, h, "/api/subir", "X", "pdf", []byte("%PDF-roto")); code != 400 {
		t.Errorf("PDF roto: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "X")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, "X"))
		if len(entries) > 0 {
			t.Errorf("se guardó algo con errores: %v", entries)
		}
	}
}

func TestPagina(t *testing.T) {
	h := (&Servidor{Equipo: "PC1"}).Handler()
	for _, ruta := range []string{"/", "/app.js", "/imagen.js", "/estilo.css", "/api/ping"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", ruta, nil))
		if rec.Code != 200 {
			t.Errorf("GET %s: %d", ruta, rec.Code)
		}
	}
}
