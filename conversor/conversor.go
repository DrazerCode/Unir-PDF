// Package conversor transforma imágenes JPEG en PDF de una página para poder
// unirlas junto a los demás PDF.
package conversor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// EsImagen indica si la ruta es un JPEG (por su extensión).
func EsImagen(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return true
	}
	return false
}

// EsAdmitido indica si la app acepta el archivo (PDF o JPEG).
func EsAdmitido(path string) bool {
	return EsImagen(path) || strings.EqualFold(filepath.Ext(path), ".pdf")
}

// JPGaPDF convierte la imagen src en un PDF de una página A4 en dst. La hoja se
// pone vertical u horizontal según la forma de la foto, la imagen se centra
// ocupando el 95 % de la hoja, y se respeta la orientación EXIF de los móviles.
func JPGaPDF(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return errors.New("no es una imagen JPEG válida")
	}
	w, h := cfg.Width, cfg.Height

	if o := orientacionEXIF(data); o >= 2 && o <= 8 {
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return errors.New("no se pudo leer la imagen JPEG")
		}
		img = aplicarOrientacion(img, o)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
			return err
		}
		data = buf.Bytes()
		w, h = img.Bounds().Dx(), img.Bounds().Dy()
	}

	hoja := "A4P"
	if w > h {
		hoja = "A4L"
	}
	imp, err := pdfcpu.ParseImportDetails("formsize:"+hoja+", position:c, scalefactor:0.95 rel", types.POINTS)
	if err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	conf := model.NewDefaultConfiguration()
	err = api.ImportImages(context.Background(), nil, out, []io.Reader{bytes.NewReader(data)}, imp, conf)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return errors.New("no se pudo convertir la imagen: " + err.Error())
	}
	return nil
}

// orientacionEXIF devuelve la etiqueta Orientation (1-8) del JPEG, o 0 si no tiene.
func orientacionEXIF(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 0
		}
		marker := data[i+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // empiezan los datos de imagen: no hay EXIF
			return 0
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 0
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return orientacionTIFF(seg[6:])
		}
		i += 2 + size
	}
	return 0
}

func orientacionTIFF(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:]) == 0x0112 { // Orientation, tipo SHORT
			v := int(bo.Uint16(t[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
	}
	return 0
}

// aplicarOrientacion gira/voltea la imagen para que quede derecha según EXIF.
func aplicarOrientacion(img image.Image, o int) image.Image {
	b := img.Bounds()
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if o >= 5 { // 5-8 intercambian ancho y alto
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride : y*src.Stride+w*4]
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dy*dst.Stride+dx*4:], row[x*4:x*4+4])
		}
	}
	return dst
}

// Cache convierte cada imagen una sola vez y guarda el PDF resultante en una
// carpeta temporal, que se borra con Borrar. Es segura entre goroutines.
type Cache struct {
	mu  sync.Mutex
	dir string
	m   map[string]string
	n   int
}

func NuevaCache() *Cache { return &Cache{m: map[string]string{}} }

// PDF devuelve un PDF equivalente a path: el propio archivo si ya es PDF, o la
// imagen convertida (convirtiéndola la primera vez).
func (c *Cache) PDF(path string) (string, error) {
	if !EsImagen(path) {
		return path, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().UnixNano())

	c.mu.Lock()
	defer c.mu.Unlock()
	if out, ok := c.m[key]; ok {
		return out, nil
	}
	if c.dir == "" {
		if c.dir, err = os.MkdirTemp("", "UnirPDF-"); err != nil {
			return "", err
		}
	}
	c.n++
	out := filepath.Join(c.dir, fmt.Sprintf("imagen%04d.pdf", c.n))
	if err := JPGaPDF(path, out); err != nil {
		return "", err
	}
	c.m[key] = out
	return out, nil
}

// Borrar elimina los PDF temporales.
func (c *Cache) Borrar() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dir != "" {
		os.RemoveAll(c.dir)
		c.dir, c.m = "", map[string]string{}
	}
}
