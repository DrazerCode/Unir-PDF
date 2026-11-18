# Unir PDF

Aplicación de escritorio para Windows (64 bits) que combina varios PDF en uno solo.
Un único `UnirPDF.exe` portable: no requiere instalación ni dependencias.

## Uso
1. Abre `UnirPDF.exe`.
2. Añade PDF con **Añadir...** o arrastrándolos (también carpetas) a la ventana.
3. Ordénalos con **Subir ▲ / Bajar ▼** y quita los que sobren con **Quitar**.
4. Pulsa **Unir PDF** y elige dónde guardar el resultado.

También puedes soltar varios PDF directamente sobre el icono del `.exe`.

## Compilar
Requiere Go 1.24+ (funciona desde Linux, macOS o Windows):

    go install github.com/tc-hib/go-winres@latest
    go-winres make --arch amd64          # incrusta manifiesto, icono y versión
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o UnirPDF.exe .

En Windows (PowerShell) define las variables con `$env:GOOS="windows"`, etc.

Para cambiar el icono, reemplaza `winres/icon.png` y vuelve a ejecutar `go-winres make`.

Tecnología: Go + [walk](https://github.com/lxn/walk) (interfaz nativa Win32) + [pdfcpu](https://github.com/pdfcpu/pdfcpu) (procesado PDF).
