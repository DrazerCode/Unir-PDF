# Unir PDF (Beta 3.0.1)

Aplicación de escritorio para Windows (64 bits) que combina varios PDF en uno solo.
Un único `UnirPDF.exe` portable: no requiere instalación ni dependencias.

## Uso
1. Abre `UnirPDF.exe`.
2. Añade PDF con **Añadir...**, una carpeta completa con **Agregar carpeta...**, o arrastra archivos o carpetas a la ventana.
   Los PDF de una carpeta se añaden en orden natural (`doc2.pdf` antes que `doc10.pdf`).
3. Ordénalos con **Subir ▲ / Bajar ▼** y quita los que sobren con **Quitar**.
4. Revisa a la derecha la **vista previa** del resultado: al elegir un archivo se muestra su primera página y con
   ◀ ▶ recorres página a página todo el documento final, pasando de un archivo al siguiente.
   Los archivos dañados o protegidos con contraseña se marcan con (!).
5. Pulsa **Unir PDF** y elige dónde guardar el resultado.

También puedes soltar varios PDF directamente sobre el icono del `.exe`.

## Límites
No hay límite en el número de archivos. En pruebas se unieron 30, 100 y 500 PDF (2.733 páginas) en menos de 1 segundo,
y 30 PDF con imágenes (158 MB) en 0,4 s. El único límite práctico es la memoria RAM: se necesita aproximadamente
el doble del tamaño total de los PDF.

## Compilar
Requiere Go 1.24+ (funciona desde Linux, macOS o Windows):

    go install github.com/tc-hib/go-winres@latest
    go-winres make --arch amd64          # incrusta manifiesto, icono y versión
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o UnirPDF.exe .

En Windows (PowerShell) define las variables con `$env:GOOS="windows"`, etc.

Para cambiar el icono, reemplaza `winres/icon.png` y vuelve a ejecutar `go-winres make`.

Tecnología: Go + [walk](https://github.com/lxn/walk) (interfaz nativa Win32) + [pdfcpu](https://github.com/pdfcpu/pdfcpu) (unión de PDF)
+ [PDFium](https://pdfium.googlesource.com/pdfium/) vía [go-pdfium](https://github.com/klippa-app/go-pdfium) en WebAssembly (vista previa).

La primera vez que se abre, la vista previa tarda unos segundos en estar lista; después arranca al instante
(se guarda una caché en `%LOCALAPPDATA%\UnirPDF`).

## Descarga
[Descargar UnirPDF.exe](https://github.com/DrazerCode/unir-pdf/raw/main/descarga/UnirPDF.exe)

---
APP desarrollada por TMO Martin Paves Ibaceta
