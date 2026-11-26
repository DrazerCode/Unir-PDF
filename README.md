# Unir PDF (Beta 4.0.3)

Aplicación de escritorio para Windows (64 bits) que combina varios PDF e imágenes JPG en un solo PDF.
Un único `UnirPDF.exe` portable: no requiere instalación ni dependencias.

## Uso
1. Abre `UnirPDF.exe`.
2. Añade PDF o imágenes JPG/JPEG con **Añadir...**, una carpeta completa con **Agregar carpeta...** (se abre en el Escritorio), o arrastra archivos o carpetas a la ventana.
   Los PDF de una carpeta se añaden en orden natural (`doc2.pdf` antes que `doc10.pdf`).
3. Ordénalos con **Subir ▲ / Bajar ▼** y quita los que sobren con **Quitar**.
   Las imágenes JPG/JPEG se convierten a PDF automáticamente, sin pasos extra: cada foto ocupa una hoja A4
   (vertical u horizontal según la foto), centrada, y las fotos de móvil se enderezan según su orientación EXIF.
4. Revisa a la derecha la **vista previa** del resultado: al elegir un archivo se muestra su primera página y con
   ◀ ▶ recorres página a página todo el documento final, pasando de un archivo al siguiente.
   Los archivos dañados o protegidos con contraseña se marcan con (!).
5. Pulsa **Unir PDF** y elige dónde guardar el resultado. El nombre propuesto depende de dónde están los archivos:
   - Si todos están dentro de una misma carpeta, se usa el nombre de la carpeta más específica que los contiene.
     Ej.: `Hospital Castro\Martin Paves Ibaceta\*.pdf` → `Martin Paves Ibaceta.pdf`
     (si vienen de varias subcarpetas de `Hospital Castro`, se usa `Hospital Castro.pdf`).
   - Si están sueltos (Escritorio, Descargas, Documentos, la raíz de un disco...) o en discos distintos → `PDF combinados.pdf`.
   - Si ya existe un archivo con ese nombre, se pregunta antes de reemplazarlo.

También puedes soltar varios PDF directamente sobre el icono del `.exe`.

## Escáner móvil (Android y iPhone, sin internet)
Mientras Unir PDF está abierto, sirve en la red local una página para escanear documentos con el teléfono.
Abajo a la izquierda de la ventana se ve su dirección (p. ej. `http://192.168.1.20:8080`) y el botón
**📱 Escáner móvil...** muestra un código QR para abrirla con la cámara del teléfono.

1. Conecta el teléfono al Wi-Fi del policlínico y abre la dirección (o escanea el QR).
2. **Tomar foto** (o **Galería**) por cada hoja. La hoja se detecta sola; se pueden ajustar las 4 esquinas
   arrastrándolas (con lupa) y luego girar y elegir filtro: *Documento* (fondo blanco, conserva colores),
   *Gris nítido*, *Blanco y negro* (máximo contraste para texto manuscrito o impreso) u *Original*.
3. En la lista se reordenan las páginas (◀ ▶), se editan (✏️), se repiten (📷) o se quitan (🗑️).
4. Escribe el nombre del paciente y pulsa **Enviar a Unir PDF**.

El PDF se guarda en `Escritorio\Escaneos\<paciente>\<paciente>.pdf` (si ya existe, `<paciente> (2).pdf`)
y se añade automáticamente a la lista de Unir PDF. Todo el procesamiento de las fotos ocurre en el teléfono
y la página va incrustada en el `.exe`: no se usa internet y los datos no salen de la red del hospital.

- Puerto 8080 (si está ocupado, el primero libre entre 8081 y 8089).
- La primera vez Windows puede preguntar si permite a Unir PDF comunicarse en redes privadas: hay que aceptar.
- Para otras apps (como la app de Android) hay una API: `GET /api/ping` y `POST /api/subir`
  (multipart: `paciente` + `pdf`), además de `POST /api/escaneo` (multipart: `paciente` + varias `paginas` JPEG).

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
