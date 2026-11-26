// UnirPDF: aplicación de escritorio para Windows que combina varios PDF en uno.
package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/skip2/go-qrcode"

	"unirpdf/conversor"
	"unirpdf/escaner"
	"unirpdf/nombre"
	"unirpdf/selector"
	"unirpdf/visor"
)

const version = "Beta 4.0.3"

// Tamaño máximo con el que se dibuja cada página; la vista la ajusta al panel.
const renderW, renderH = 1000, 1400

// fileModel alimenta la lista visible; guarda las rutas completas y las páginas de cada PDF.
type fileModel struct {
	walk.ListModelBase
	paths []string
	pages map[string]int    // páginas por ruta (ausente = aún sin contar)
	errs  map[string]string // ruta -> motivo por el que no se puede leer
}

func (m *fileModel) ItemCount() int { return len(m.paths) }
func (m *fileModel) Value(i int) interface{} {
	p := m.paths[i]
	info := "…"
	if e, ok := m.errs[p]; ok {
		info = "(!) " + e
	} else if n, ok := m.pages[p]; ok {
		if n == 1 {
			info = "1 pág."
		} else {
			info = fmt.Sprintf("%d págs.", n)
		}
		if conversor.EsImagen(p) {
			info += " · imagen"
		}
	}
	return fmt.Sprintf("%d.  %s   (%s)", i+1, filepath.Base(p), info)
}

type app struct {
	mw      *walk.MainWindow
	lb      *walk.ListBox
	status  *walk.Label
	model   *fileModel
	iv      *walk.ImageView
	pvTitle *walk.Label
	pvPos   *walk.Label
	btnPrev *walk.PushButton
	btnNext *walk.PushButton
	btnFst  *walk.PushButton
	btnLst  *walk.PushButton

	visor    *visor.Visor // nil hasta que PDFium termine de cargar
	conv     *conversor.Cache
	visorErr error
	counting bool

	curFile, curPage int // página mostrada en la vista previa (curFile = -1: ninguna)
	navigating       bool
	cancelRender     context.CancelFunc
	bitmap           *walk.Bitmap

	esc    *escaner.Servidor // página del escáner para teléfonos (nil si no pudo iniciarse)
	escErr error
	lblEsc *walk.Label
}

func init() {
	// Una app de ventana (-H windowsgui) no tiene consola y os.Stdout/os.Stderr
	// apuntan a un handle inválido. Los redirigimos a NUL para que ninguna
	// librería falle al intentar usarlos.
	if f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}

func main() {
	a := &app{
		model:   &fileModel{pages: map[string]int{}, errs: map[string]string{}},
		curFile: -1,
		conv:    conversor.NuevaCache(),
	}

	navBtn := func(assign **walk.PushButton, text string, f func()) PushButton {
		return PushButton{AssignTo: assign, Text: text, MaxSize: Size{Width: 40}, Enabled: false, OnClicked: f}
	}

	err := MainWindow{
		AssignTo: &a.mw,
		Title:    "Unir PDF - " + version,
		MinSize:  Size{Width: 820, Height: 500},
		Size:     Size{Width: 1080, Height: 680},
		Layout:   VBox{},
		OnDropFiles: func(files []string) {
			a.addFiles(files)
		},
		Children: []Widget{
			Label{Text: "Añade PDF o imágenes JPG, o una carpeta (también arrastrándolos aquí), ordénalos y pulsa \"Unir PDF\". A la derecha ves cómo quedará el resultado."},
			HSplitter{
				Children: []Widget{
					// Columna izquierda: lista y botones.
					Composite{
						StretchFactor: 5,
						Layout:        HBox{MarginsZero: true},
						Children: []Widget{
							ListBox{
								AssignTo:              &a.lb,
								Model:                 a.model,
								OnCurrentIndexChanged: a.onSelect,
							},
							Composite{
								Layout: VBox{MarginsZero: true},
								Children: []Widget{
									PushButton{Text: "Añadir...", OnClicked: a.onAdd},
									PushButton{Text: "Agregar carpeta...", OnClicked: a.onAddFolder},
									PushButton{Text: "Quitar", OnClicked: a.onRemove},
									PushButton{Text: "Subir ▲", OnClicked: func() { a.move(-1) }},
									PushButton{Text: "Bajar ▼", OnClicked: func() { a.move(1) }},
									PushButton{Text: "Limpiar", OnClicked: a.onClear},
									VSpacer{},
								},
							},
						},
					},
					// Columna derecha: vista previa.
					Composite{
						StretchFactor: 4,
						Layout:        VBox{MarginsZero: true},
						Children: []Widget{
							Label{AssignTo: &a.pvTitle, Text: "Vista previa", Font: Font{Bold: true}},
							ImageView{
								AssignTo:   &a.iv,
								Mode:       ImageViewModeZoom,
								Margin:     8,
								Background: SolidColorBrush{Color: walk.RGB(205, 205, 210)},
							},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									HSpacer{},
									navBtn(&a.btnFst, "|◀", func() { a.goGlobal(0) }),
									navBtn(&a.btnPrev, "◀", func() { a.step(-1) }),
									Label{AssignTo: &a.pvPos, Text: "", MinSize: Size{Width: 300}, TextAlignment: AlignCenter},
									navBtn(&a.btnNext, "▶", func() { a.step(1) }),
									navBtn(&a.btnLst, "▶|", func() { a.goGlobal(a.totalPages() - 1) }),
									HSpacer{},
								},
							},
						},
					},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{AssignTo: &a.status, Text: "0 archivos"},
					HSpacer{},
					PushButton{Text: "Unir PDF", MinSize: Size{Width: 120, Height: 32}, OnClicked: a.onMerge},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "📱 Escáner móvil...", OnClicked: a.onEscaner},
					Label{AssignTo: &a.lblEsc, Text: "Iniciando escáner móvil…", TextColor: walk.RGB(60, 60, 60)},
					HSpacer{},
					Label{
						Text:      "APP desarrollada por TMO Martin Paves Ibaceta",
						Font:      Font{PointSize: 7},
						TextColor: walk.RGB(110, 110, 110),
					},
				},
			},
		},
	}.Create()
	if err != nil {
		walk.MsgBox(nil, "Error", err.Error(), walk.MsgBoxIconError)
		return
	}

	a.updatePreview()
	// Icono de la ventana (barra de título y barra de tareas): el mismo que lleva el .exe.
	if ico, err := walk.NewIconFromResource("APP"); err == nil {
		a.mw.SetIcon(ico)
	}
	a.startVisor()
	a.startEscaner()

	// Archivos pasados como argumentos (p. ej. al soltarlos sobre el .exe).
	if len(os.Args) > 1 {
		a.addFiles(os.Args[1:])
	}
	a.mw.Run()

	// La ventana ya se cerró: borrar los PDF temporales de las imágenes (como
	// mucho 2 s, por si una conversión sigue en curso) y terminar el proceso
	// de inmediato. No se espera a que PDFium u otras tareas en segundo plano
	// acaben: Windows libera toda su memoria al salir, y esperar podía dejar el
	// proceso colgado e invisible tras cerrar la ventana.
	hecho := make(chan struct{})
	go func() {
		a.conv.Borrar()
		close(hecho)
	}()
	select {
	case <-hecho:
	case <-time.After(2 * time.Second):
	}
	os.Exit(0)
}

// ---------- Lista de archivos ----------

func (a *app) refresh(sel int) {
	if sel >= len(a.model.paths) {
		sel = len(a.model.paths) - 1
	}
	a.navigating = true
	a.model.PublishItemsReset()
	a.lb.SetCurrentIndex(sel)
	a.navigating = false
	a.curFile, a.curPage = sel, 0
	a.updatePreview()
	a.updateStatus()
}

func (a *app) updateStatus() {
	n := len(a.model.paths)
	s := fmt.Sprintf("%d archivos", n)
	if n == 1 {
		s = "1 archivo"
	}
	if n > 0 && a.allCounted() {
		s += fmt.Sprintf("  ·  %d páginas en total", a.totalPages())
	}
	if bad := a.badCount(); bad > 0 {
		s += fmt.Sprintf("  ·  (!) %d no se pueden leer", bad)
	}
	a.status.SetText(s)
}

func (a *app) addFiles(files []string) {
	skipped := 0
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.IsDir() {
			a.model.paths = append(a.model.paths, pdfsInFolder(f)...)
			continue
		}
		if conversor.EsAdmitido(f) {
			a.model.paths = append(a.model.paths, f)
		} else {
			skipped++
		}
	}
	a.refresh(len(a.model.paths) - 1)
	a.countPages()
	if skipped > 0 {
		walk.MsgBox(a.mw, "Aviso", fmt.Sprintf("Se ignoraron %d archivo(s) que no son PDF ni JPG.", skipped), walk.MsgBoxIconWarning)
	}
}

func (a *app) onAdd() {
	dlg := &walk.FileDialog{
		Title:  "Selecciona archivos PDF o imágenes JPG",
		Filter: "PDF e imágenes JPG (*.pdf;*.jpg;*.jpeg)|*.pdf;*.jpg;*.jpeg|Archivos PDF (*.pdf)|*.pdf|Imágenes JPG (*.jpg;*.jpeg)|*.jpg;*.jpeg",
	}
	if ok, err := dlg.ShowOpenMultiple(a.mw); err != nil || !ok {
		return
	}
	a.addFiles(dlg.FilePaths)
}

func (a *app) onAddFolder() {
	const titulo = "Selecciona una carpeta con archivos PDF o JPG"
	// Selector moderno de Windows, abierto en el Escritorio.
	dir, ok, err := selector.Carpeta(uintptr(a.mw.Handle()), titulo, selector.Escritorio())
	if err != nil { // si no está disponible, el selector clásico
		dlg := &walk.FileDialog{Title: titulo}
		if ok, err := dlg.ShowBrowseFolder(a.mw); err != nil || !ok {
			return
		}
		dir, ok = dlg.FilePath, true
	}
	if !ok {
		return
	}
	found := pdfsInFolder(dir)
	if len(found) == 0 {
		walk.MsgBox(a.mw, "Agregar carpeta", "La carpeta seleccionada no contiene archivos PDF ni JPG.", walk.MsgBoxIconInformation)
		return
	}
	a.addFiles(found)
}

func (a *app) onRemove() {
	i := a.lb.CurrentIndex()
	if i < 0 {
		return
	}
	a.model.paths = append(a.model.paths[:i], a.model.paths[i+1:]...)
	if i >= len(a.model.paths) {
		i = len(a.model.paths) - 1
	}
	a.refresh(i)
}

func (a *app) move(d int) {
	i := a.lb.CurrentIndex()
	j := i + d
	if i < 0 || j < 0 || j >= len(a.model.paths) {
		return
	}
	p := a.model.paths
	p[i], p[j] = p[j], p[i]
	a.refresh(j)
}

func (a *app) onClear() {
	a.model.paths = nil
	a.refresh(-1)
}

// pdfsInFolder devuelve los PDF y JPG de una carpeta (sin subcarpetas) en orden
// natural: "doc2.pdf" va antes que "doc10.pdf".
func pdfsInFolder(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && conversor.EsAdmitido(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
	}
	return paths
}

// naturalLess compara sin distinguir mayúsculas y tratando los números como valores.
func naturalLess(a, b string) bool {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si := i
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	return len(ra)-i < len(rb)-j
}

// ---------- Vista previa ----------

// startVisor carga PDFium en segundo plano para no bloquear la ventana.
func (a *app) startVisor() {
	go func() {
		v, err := visor.Nuevo()
		a.mw.Synchronize(func() {
			a.visor, a.visorErr = v, err
			a.updatePreview()
			a.countPages()
		})
	}()
}

// countPages cuenta en segundo plano las páginas de los PDF que aún no se conocen.
func (a *app) countPages() {
	if a.visor == nil || a.counting {
		return
	}
	var pending []string
	for _, p := range a.model.paths {
		_, ok1 := a.model.pages[p]
		_, ok2 := a.model.errs[p]
		if !ok1 && !ok2 {
			pending = append(pending, p)
		}
	}
	if len(pending) == 0 {
		return
	}
	a.counting = true
	v := a.visor
	go func() {
		for _, p := range pending {
			n := 0
			pdf, err := a.conv.PDF(p) // las imágenes se convierten aquí, en segundo plano
			if err == nil {
				n, err = v.Paginas(pdf)
			}
			a.mw.Synchronize(func() {
				if err != nil {
					a.model.errs[p] = err.Error()
				} else {
					a.model.pages[p] = n
				}
				a.navigating = true // conservar la página que se está viendo
				a.model.PublishItemsReset()
				a.lb.SetCurrentIndex(a.curFile)
				a.navigating = false
				a.updateStatus()
				if a.curFile >= 0 && a.curFile < len(a.model.paths) && a.model.paths[a.curFile] == p {
					a.updatePreview()
				}
			})
		}
		a.mw.Synchronize(func() {
			a.counting = false
			a.updatePreview() // ya se conoce el total de páginas del resultado
			a.countPages()    // por si se añadieron archivos mientras tanto
		})
	}()
}

func (a *app) pagesOf(i int) (int, bool) {
	n, ok := a.model.pages[a.model.paths[i]]
	return n, ok
}

func (a *app) allCounted() bool {
	for i := range a.model.paths {
		if _, ok := a.pagesOf(i); !ok {
			if _, bad := a.model.errs[a.model.paths[i]]; !bad {
				return false
			}
		}
	}
	return true
}

func (a *app) badCount() int {
	n := 0
	for _, p := range a.model.paths {
		if _, bad := a.model.errs[p]; bad {
			n++
		}
	}
	return n
}

func (a *app) totalPages() int {
	t := 0
	for i := range a.model.paths {
		n, _ := a.pagesOf(i)
		t += n
	}
	return t
}

// globalOf convierte (archivo, página) en la página del documento resultante (base 0).
func (a *app) globalOf(file, page int) int {
	g := page
	for i := 0; i < file; i++ {
		n, _ := a.pagesOf(i)
		g += n
	}
	return g
}

// locate hace lo contrario que globalOf.
func (a *app) locate(g int) (file, page int) {
	for i := range a.model.paths {
		n, _ := a.pagesOf(i)
		if g < n {
			return i, g
		}
		g -= n
	}
	return -1, 0
}

// onSelect se ejecuta al seleccionar un archivo: muestra su primera página.
func (a *app) onSelect() {
	if a.navigating {
		return
	}
	a.curFile, a.curPage = a.lb.CurrentIndex(), 0
	a.updatePreview()
}

func (a *app) goGlobal(g int) {
	f, p := a.locate(g)
	if f < 0 {
		return
	}
	a.curFile, a.curPage = f, p
	a.navigating = true
	a.lb.SetCurrentIndex(f)
	a.navigating = false
	a.updatePreview()
}

// step avanza o retrocede una página del resultado, saltando entre archivos.
func (a *app) step(d int) {
	if a.curFile < 0 {
		return
	}
	a.goGlobal(a.globalOf(a.curFile, a.curPage) + d)
}

// updatePreview actualiza textos, botones e imagen según la página actual.
func (a *app) updatePreview() {
	if a.cancelRender != nil {
		a.cancelRender()
		a.cancelRender = nil
	}
	if a.curFile >= len(a.model.paths) {
		a.curFile = len(a.model.paths) - 1
		a.curPage = 0
	}

	setNav := func(prev, next bool) {
		a.btnFst.SetEnabled(prev)
		a.btnPrev.SetEnabled(prev)
		a.btnNext.SetEnabled(next)
		a.btnLst.SetEnabled(next)
	}
	blank := func(title, pos string) {
		a.pvTitle.SetText(title)
		a.pvPos.SetText(pos)
		setNav(false, false)
		a.setImage(nil)
	}

	switch {
	case a.visorErr != nil:
		blank("Vista previa no disponible", a.visorErr.Error())
		return
	case len(a.model.paths) == 0:
		blank("Vista previa", "Añade archivos para verlos aquí")
		return
	case a.curFile < 0:
		blank("Vista previa", "Selecciona un archivo de la lista")
		return
	}

	path := a.model.paths[a.curFile]
	a.pvTitle.SetText(fmt.Sprintf("%d. %s", a.curFile+1, filepath.Base(path)))

	if e, bad := a.model.errs[path]; bad {
		blank(a.pvTitle.Text(), "(!) No se puede mostrar: "+e)
		return
	}
	n, counted := a.model.pages[path]
	if a.visor == nil || !counted {
		a.pvPos.SetText("Cargando vista previa…")
		setNav(false, false)
		return
	}
	if a.curPage >= n {
		a.curPage = n - 1
	}

	g := a.globalOf(a.curFile, a.curPage)
	pos := fmt.Sprintf("Pág. %d/%d del archivo", a.curPage+1, n)
	if a.allCounted() {
		pos += fmt.Sprintf("  ·  %d/%d del resultado", g+1, a.totalPages())
	}
	a.pvPos.SetText(pos)
	f, _ := a.locate(g + 1)
	setNav(g > 0, f >= 0)

	ctx, cancel := context.WithCancel(context.Background())
	a.cancelRender = cancel
	v, page := a.visor, a.curPage
	go func() {
		pdf, err := a.conv.PDF(path)
		var img *image.RGBA
		if err == nil {
			img, err = v.Dibujar(ctx, pdf, page, renderW, renderH)
		}
		a.mw.Synchronize(func() {
			if ctx.Err() != nil {
				return // el usuario ya cambió de página
			}
			if err != nil {
				a.pvPos.SetText("(!) No se pudo dibujar la página: " + err.Error())
				a.setImage(nil)
				return
			}
			bmp, err := walk.NewBitmapFromImageForDPI(img, 96)
			if err != nil {
				return
			}
			a.setImage(bmp)
		})
	}()
}

func (a *app) setImage(bmp *walk.Bitmap) {
	if bmp == nil {
		a.iv.SetImage(nil)
	} else {
		a.iv.SetImage(bmp)
	}
	if a.bitmap != nil {
		a.bitmap.Dispose()
	}
	a.bitmap = bmp
}

// ---------- Escáner móvil ----------

// startEscaner abre en la red local la página para escanear con el teléfono.
// Los PDF recibidos se guardan en Escritorio\Escaneos\<paciente> y se añaden a la lista.
func (a *app) startEscaner() {
	base := selector.Escritorio()
	if base == "" {
		base, _ = os.UserHomeDir()
	}
	equipo, _ := os.Hostname()
	s := &escaner.Servidor{
		Destino: filepath.Join(base, "Escaneos"),
		Equipo:  equipo,
		AlRecibir: func(ruta, paciente string) {
			a.mw.Synchronize(func() {
				a.addFiles([]string{ruta})
				a.lblEsc.SetText(fmt.Sprintf("Escaneo recibido %s: %s", time.Now().Format("15:04"), filepath.Base(ruta)))
			})
		},
	}
	if err := s.Iniciar(); err != nil {
		a.escErr = err
		a.lblEsc.SetText("Escáner móvil no disponible")
		return
	}
	a.esc = s
	if u := s.URLs(); len(u) > 0 {
		a.lblEsc.SetText("Escáner móvil: " + u[0])
	} else {
		a.lblEsc.SetText("Escáner móvil: sin conexión de red")
	}
}

// onEscaner muestra la dirección de la página y un código QR para abrirla con el teléfono.
func (a *app) onEscaner() {
	if a.esc == nil {
		msg := "El escáner móvil no está disponible."
		if a.escErr != nil {
			msg += "\n\n" + a.escErr.Error()
		}
		walk.MsgBox(a.mw, "Escáner móvil", msg, walk.MsgBoxIconWarning)
		return
	}
	urls := a.esc.URLs()
	if len(urls) == 0 {
		walk.MsgBox(a.mw, "Escáner móvil", "Este PC no está conectado a ninguna red. Conéctalo a la red del policlínico y vuelve a intentarlo.", walk.MsgBoxIconWarning)
		return
	}
	var qrBmp *walk.Bitmap
	if q, err := qrcode.New(urls[0], qrcode.Medium); err == nil {
		qrBmp, _ = walk.NewBitmapFromImageForDPI(q.Image(320), 96)
	}
	texto := "1. Conecta el teléfono (Android o iPhone) al Wi-Fi del policlínico.\r\n" +
		"2. Escanea este código con la cámara o abre en el navegador:\r\n\r\n    " + strings.Join(urls, "\r\n    ") + "\r\n\r\n" +
		"3. Fotografía las hojas, escribe el nombre del paciente y pulsa \"Enviar a Unir PDF\".\r\n\r\n" +
		"El PDF se guarda en " + a.esc.Destino + "\\<paciente> y aparece en esta lista.\r\n" +
		"No se usa internet: todo queda dentro de la red del hospital.\r\n" +
		"Unir PDF debe estar abierto mientras se escanea."

	var dlg *walk.Dialog
	Dialog{
		AssignTo: &dlg,
		Title:    "Escáner móvil",
		Layout:   VBox{},
		MinSize:  Size{Width: 520, Height: 560},
		Children: []Widget{
			ImageView{Image: qrBmp, Mode: ImageViewModeIdeal, MinSize: Size{Width: 320, Height: 320}},
			TextEdit{Text: texto, ReadOnly: true, MinSize: Size{Height: 170}},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{Text: "Cerrar", OnClicked: func() { dlg.Accept() }},
				},
			},
		},
	}.Run(a.mw)
	if qrBmp != nil {
		qrBmp.Dispose()
	}
}

// ---------- Unir ----------

func (a *app) onMerge() {
	if len(a.model.paths) == 0 || (len(a.model.paths) == 1 && !conversor.EsImagen(a.model.paths[0])) {
		walk.MsgBox(a.mw, "Unir PDF", "Añade al menos dos archivos (o una imagen JPG para convertirla a PDF).", walk.MsgBoxIconInformation)
		return
	}
	if bad := a.badCount(); bad > 0 {
		if walk.MsgBox(a.mw, "Unir PDF",
			fmt.Sprintf("%d archivo(s) de la lista no se pueden leer (marcados con (!)) y probablemente harán fallar la unión.\n\n¿Intentarlo de todas formas?", bad),
			walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
			return
		}
	}
	// Nombre propuesto: la (sub)carpeta común de los archivos, o "PDF combinados" si están sueltos.
	nom, dir := nombre.Propuesto(a.model.paths, nombre.CarpetasSistema())
	if dir == "" {
		dir = filepath.Dir(a.model.paths[0])
	}
	dlg := &walk.FileDialog{
		Title:          "Guardar PDF combinado",
		Filter:         "Archivos PDF (*.pdf)|*.pdf",
		FilePath:       nom + ".pdf",
		InitialDirPath: dir,
		Flags:          win.OFN_OVERWRITEPROMPT, // preguntar antes de reemplazar un archivo existente
	}
	if ok, err := dlg.ShowSave(a.mw); err != nil || !ok {
		return
	}
	out := dlg.FilePath
	if !strings.EqualFold(filepath.Ext(out), ".pdf") {
		out += ".pdf"
	}
	for _, p := range a.model.paths {
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(out)) {
			walk.MsgBox(a.mw, "Error", "El archivo de salida no puede ser uno de los de entrada.", walk.MsgBoxIconError)
			return
		}
	}

	a.status.SetText("Uniendo...")
	a.mw.SetEnabled(false)
	inputs := append([]string(nil), a.model.paths...)

	go func() {
		err := unir(a.conv, inputs, out)
		a.mw.Synchronize(func() {
			a.mw.SetEnabled(true)
			a.updateStatus()
			if err != nil {
				os.Remove(out)
				walk.MsgBox(a.mw, "Error", "No se pudo crear el PDF:\n\n"+err.Error(), walk.MsgBoxIconError)
				return
			}
			walk.MsgBox(a.mw, "Listo", "PDF creado correctamente:\n\n"+out, walk.MsgBoxIconInformation)
		})
	}()
}

// unir convierte las imágenes a PDF (si aún no lo estaban) y une todo en out.
func unir(conv *conversor.Cache, inputs []string, out string) error {
	pdfs := make([]string, len(inputs))
	for i, p := range inputs {
		pdf, err := conv.PDF(p)
		if err != nil {
			return fmt.Errorf("%s: %v", filepath.Base(p), err)
		}
		pdfs[i] = pdf
	}
	if len(pdfs) == 1 { // una sola imagen: basta con copiar su PDF
		data, err := os.ReadFile(pdfs[0])
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	}
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return api.MergeCreateFile(context.Background(), pdfs, out, false, conf)
}
