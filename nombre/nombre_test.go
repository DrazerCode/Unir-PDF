package nombre

import "testing"

func TestPropuesto(t *testing.T) {
	sis := []string{"/home/martin", "/home/martin/Escritorio", "/home/martin/Descargas"}
	casos := []struct {
		paths []string
		want  string
	}{
		// I) sueltos
		{[]string{"/home/martin/Escritorio/a.pdf", "/home/martin/Escritorio/b.jpg"}, PorDefecto},
		{[]string{"/home/martin/Descargas/a.pdf", "/home/martin/Escritorio/b.pdf"}, PorDefecto},
		{[]string{"/a.pdf", "/b.pdf"}, PorDefecto},
		{[]string{"/home/OtraPersona/OneDrive/Escritorio/a.pdf"}, PorDefecto},
		// II) dentro de una carpeta
		{[]string{"/home/martin/Escritorio/Hospital Castro/a.pdf", "/home/martin/Escritorio/Hospital Castro/b.pdf"}, "Hospital Castro"},
		// III) subcarpeta > carpeta
		{[]string{"/home/martin/Escritorio/Hospital Castro/Martin Paves Ibaceta/a.pdf", "/home/martin/Escritorio/Hospital Castro/Martin Paves Ibaceta/foto.jpg"}, "Martin Paves Ibaceta"},
		// subcarpetas distintas de la misma carpeta -> la carpeta
		{[]string{"/d/Hospital Castro/Martin Paves Ibaceta/a.pdf", "/d/Hospital Castro/Ana Soto/b.pdf"}, "Hospital Castro"},
		// archivos en carpeta y en su subcarpeta -> la carpeta
		{[]string{"/d/Hospital Castro/a.pdf", "/d/Hospital Castro/Martin Paves Ibaceta/b.pdf"}, "Hospital Castro"},
		// mayúsculas distintas (Windows no las distingue)
		{[]string{"/d/Hospital Castro/x/a.pdf", "/d/hospital castro/x/b.pdf"}, "x"},
		// prefijo engañoso: "Castro" vs "Castro2" no comparten carpeta
		{[]string{"/d/H/Castro/a.pdf", "/d/H/Castro2/b.pdf"}, "H"},
	}
	for _, c := range casos {
		if got, _ := Propuesto(c.paths, sis); got != c.want {
			t.Errorf("%v: obtenido %q, esperado %q", c.paths, got, c.want)
		}
	}
}
