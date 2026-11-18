// UnirPDF: aplicación de escritorio para Windows que combina varios PDF en uno.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// fileModel alimenta la lista visible con nombres de archivo; guarda las rutas completas.
type fileModel struct {
	walk.ListModelBase
	paths []string
}

func (m *fileModel) ItemCount() int { return len(m.paths) }
func (m *fileModel) Value(i int) interface{} {
	return fmt.Sprintf("%d.  %s   (%s)", i+1, filepath.Base(m.paths[i]), filepath.Dir(m.paths[i]))
}

type app struct {
	mw     *walk.MainWindow
	lb     *walk.ListBox
	status *walk.Label
	model  *fileModel
}

func main() {
	a := &app{model: &fileModel{}}

	err := MainWindow{
		AssignTo: &a.mw,
		Title:    "Unir PDF - Beta 2.0",
		MinSize:  Size{Width: 560, Height: 420},
		Size:     Size{Width: 720, Height: 500},
		Layout:   VBox{},
		OnDropFiles: func(files []string) {
			a.addFiles(files)
		},
		Children: []Widget{
			Label{Text: "Añade PDF o una carpeta (o arrástralos aquí), ordénalos y pulsa \"Unir PDF\"."},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					ListBox{
						AssignTo:       &a.lb,
						Model:          a.model,
						MultiSelection: false,
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

	// Archivos pasados como argumentos (p. ej. al soltarlos sobre el .exe).
	if len(os.Args) > 1 {
		a.addFiles(os.Args[1:])
	}
	a.mw.Run()
}

func (a *app) refresh(sel int) {
	a.model.PublishItemsReset()
	if sel >= 0 && sel < len(a.model.paths) {
		a.lb.SetCurrentIndex(sel)
	}
	n := len(a.model.paths)
	if n == 1 {
		a.status.SetText("1 archivo")
	} else {
		a.status.SetText(fmt.Sprintf("%d archivos", n))
	}
}

func (a *app) addFiles(files []string) {
	skipped := 0
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.IsDir() {
			a.model.paths = append(a.model.paths, pdfsInFolder(f)...)
			continue
		}
		if strings.EqualFold(filepath.Ext(f), ".pdf") {
			a.model.paths = append(a.model.paths, f)
		} else {
			skipped++
		}
	}
	a.refresh(len(a.model.paths) - 1)
	if skipped > 0 {
		walk.MsgBox(a.mw, "Aviso", fmt.Sprintf("Se ignoraron %d archivo(s) que no son PDF.", skipped), walk.MsgBoxIconWarning)
	}
}

func (a *app) onAdd() {
	dlg := &walk.FileDialog{Title: "Selecciona archivos PDF", Filter: "Archivos PDF (*.pdf)|*.pdf"}
	if ok, err := dlg.ShowOpenMultiple(a.mw); err != nil || !ok {
		return
	}
	a.addFiles(dlg.FilePaths)
}

func (a *app) onAddFolder() {
	dlg := &walk.FileDialog{Title: "Selecciona una carpeta con archivos PDF"}
	if ok, err := dlg.ShowBrowseFolder(a.mw); err != nil || !ok {
		return
	}
	found := pdfsInFolder(dlg.FilePath)
	if len(found) == 0 {
		walk.MsgBox(a.mw, "Agregar carpeta", "La carpeta seleccionada no contiene archivos PDF.", walk.MsgBoxIconInformation)
		return
	}
	a.model.paths = append(a.model.paths, found...)
	a.refresh(len(a.model.paths) - 1)
}

// pdfsInFolder devuelve los PDF de una carpeta (sin subcarpetas) en orden
// natural: "doc2.pdf" va antes que "doc10.pdf".
func pdfsInFolder(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
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

func (a *app) onMerge() {
	if len(a.model.paths) < 2 {
		walk.MsgBox(a.mw, "Unir PDF", "Añade al menos dos archivos PDF.", walk.MsgBoxIconInformation)
		return
	}
	dlg := &walk.FileDialog{
		Title:          "Guardar PDF combinado",
		Filter:         "Archivos PDF (*.pdf)|*.pdf",
		FilePath:       "combinado.pdf",
		InitialDirPath: filepath.Dir(a.model.paths[0]),
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
		conf := model.NewDefaultConfiguration()
		conf.ValidationMode = model.ValidationRelaxed
		err := api.MergeCreateFile(context.Background(), inputs, out, false, conf)
		a.mw.Synchronize(func() {
			a.mw.SetEnabled(true)
			a.refresh(a.lb.CurrentIndex())
			if err != nil {
				os.Remove(out)
				walk.MsgBox(a.mw, "Error", "No se pudieron unir los PDF:\n\n"+err.Error(), walk.MsgBoxIconError)
				return
			}
			walk.MsgBox(a.mw, "Listo", "PDF creado correctamente:\n\n"+out, walk.MsgBoxIconInformation)
		})
	}()
}
