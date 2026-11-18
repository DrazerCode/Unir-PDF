// Package visor dibuja páginas de PDF como imágenes usando PDFium (el motor
// de PDF de Chrome) compilado a WebAssembly, de modo que todo va dentro del
// .exe sin dependencias externas.
package visor

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// Visor es seguro para usarse desde varias goroutines: las llamadas a PDFium
// se serializan internamente.
type Visor struct {
	mu   sync.Mutex
	pool pdfium.Pool
	inst pdfium.Pdfium

	// Último documento abierto, para no volver a leerlo al pasar de página.
	openPath string
	openMod  time.Time
	openDoc  references.FPDF_DOCUMENT
	openData []byte
}

// Nuevo inicializa PDFium. La primera vez tarda unos segundos (compila el
// módulo WebAssembly); el resultado se guarda en la caché del usuario para
// que los siguientes arranques sean rápidos.
func Nuevo() (*Visor, error) {
	rc := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
	if dir, err := os.UserCacheDir(); err == nil {
		if cache, err := wazero.NewCompilationCacheWithDir(filepath.Join(dir, "UnirPDF", "pdfium")); err == nil {
			rc = rc.WithCompilationCache(cache)
		}
	}
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:       1,
		MaxIdle:       1,
		MaxTotal:      1,
		RuntimeConfig: rc,
	})
	if err != nil {
		return nil, err
	}
	inst, err := pool.GetInstance(time.Minute)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &Visor{pool: pool, inst: inst}, nil
}

// Cerrar libera PDFium.
func (v *Visor) Cerrar() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closeDoc()
	v.inst.Close()
	v.pool.Close()
}

func (v *Visor) closeDoc() {
	if v.openPath != "" {
		v.inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: v.openDoc})
		v.openPath, v.openData = "", nil
	}
}

// open abre path, reutilizando el documento si ya estaba abierto y no cambió.
func (v *Visor) open(path string) (references.FPDF_DOCUMENT, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if v.openPath == path && v.openMod.Equal(st.ModTime()) {
		return v.openDoc, nil
	}
	v.closeDoc()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	doc, err := v.inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return "", traducir(err)
	}
	v.openPath, v.openMod, v.openDoc, v.openData = path, st.ModTime(), doc.Document, data
	return doc.Document, nil
}

// Paginas devuelve el número de páginas del PDF.
func (v *Visor) Paginas(path string) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	doc, err := v.open(path)
	if err != nil {
		return 0, err
	}
	r, err := v.inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc})
	if err != nil {
		return 0, err
	}
	return r.PageCount, nil
}

// Dibujar renderiza la página (base 0) para que quepa en maxW x maxH píxeles,
// sobre fondo blanco.
func (v *Visor) Dibujar(ctx context.Context, path string, page, maxW, maxH int) (*image.RGBA, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err // la petición quedó obsoleta mientras esperaba
	}
	doc, err := v.open(path)
	if err != nil {
		return nil, err
	}
	r, err := v.inst.RenderPageInPixels(&requests.RenderPageInPixels{
		Page:       requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: page}},
		Width:      maxW,
		Height:     maxH,
		RenderForm: true,
		Document:   &doc,
	})
	if err != nil {
		return nil, err
	}
	defer r.Cleanup() // en WebAssembly el búfer deja de ser válido tras Cleanup: copiamos antes
	src := r.Result.RenderedImage
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Over)
	return out, nil
}

func traducir(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "password"):
		return errors.New("el PDF está protegido con contraseña")
	case strings.Contains(msg, "format"), strings.Contains(msg, "file"):
		return errors.New("el archivo está dañado o no es un PDF válido")
	}
	return err
}
