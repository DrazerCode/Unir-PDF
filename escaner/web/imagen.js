// Procesamiento de imágenes del escáner, en JavaScript puro (sin librerías ni
// internet): detección de los bordes de la hoja, corrección de perspectiva,
// giro y filtros para que el texto (impreso o manuscrito) quede nítido.
'use strict';

const Imagen = (() => {
  // Lado mayor con el que se trabaja la foto. Las fotos de móvil (12-50 Mpx)
  // se reducen para no agotar la memoria del teléfono (sobre todo en iPhone).
  const LADO_TRABAJO = 2600;
  // Lado mayor de la página final (≈ 230 ppp en una hoja A4).
  const LADO_SALIDA = 2400;

  // ---------- Carga ----------

  // cargar lee el archivo de la cámara/galería y devuelve un canvas ya
  // orientado (los navegadores aplican la orientación EXIF al dibujar).
  async function cargar(file) {
    let fuente;
    let w, h;
    try {
      fuente = await createImageBitmap(file, { imageOrientation: 'from-image' });
      w = fuente.width; h = fuente.height;
    } catch (e) {
      fuente = await new Promise((ok, mal) => {
        const img = new Image();
        const url = URL.createObjectURL(file);
        img.onload = () => { URL.revokeObjectURL(url); ok(img); };
        img.onerror = () => { URL.revokeObjectURL(url); mal(new Error('No se pudo abrir la imagen.')); };
        img.src = url;
      });
      w = fuente.naturalWidth; h = fuente.naturalHeight;
    }
    const k = Math.min(1, LADO_TRABAJO / Math.max(w, h));
    const c = lienzo(Math.round(w * k), Math.round(h * k));
    const ctx = c.getContext('2d');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(fuente, 0, 0, c.width, c.height);
    if (fuente.close) fuente.close();
    return c;
  }

  function lienzo(w, h) {
    const c = document.createElement('canvas');
    c.width = Math.max(1, w); c.height = Math.max(1, h);
    return c;
  }

  function copiar(src) {
    const c = lienzo(src.width, src.height);
    c.getContext('2d').drawImage(src, 0, 0);
    return c;
  }

  // liberar ayuda a iOS a recuperar la memoria de un canvas.
  function liberar(c) { if (c) { c.width = 1; c.height = 1; } }

  function reducido(src, lado) {
    const k = Math.min(1, lado / Math.max(src.width, src.height));
    const c = lienzo(Math.round(src.width * k), Math.round(src.height * k));
    const ctx = c.getContext('2d');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(src, 0, 0, c.width, c.height);
    return c;
  }

  // ---------- Detección de la hoja ----------

  // Esquinas por defecto: un pequeño margen dentro de la foto.
  function esquinasPorDefecto() {
    const m = 0.04;
    return [{ x: m, y: m }, { x: 1 - m, y: m }, { x: 1 - m, y: 1 - m }, { x: m, y: 1 - m }];
  }

  // detectar busca la hoja en la foto y devuelve sus 4 esquinas normalizadas
  // (0..1) en orden sup-izq, sup-der, inf-der, inf-izq, o null si no la encuentra.
  // Se prueban dos métodos (bordes y brillo) y se queda con el que dé una
  // forma más parecida a un cuadrilátero.
  function detectar(src) {
    const c = reducido(src, 400);
    const w = c.width, h = c.height;
    const rgba = c.getContext('2d').getImageData(0, 0, w, h).data;
    liberar(c);
    let gris = new Float32Array(w * h);
    for (let i = 0, j = 0; i < gris.length; i++, j += 4) {
      gris[i] = 0.299 * rgba[j] + 0.587 * rgba[j + 1] + 0.114 * rgba[j + 2];
    }
    gris = desenfocar(desenfocar(gris, w, h, 2), w, h, 2);

    const candidatos = [];
    const porBordes = regionPorBordes(gris, w, h);
    if (porBordes) candidatos.push(porBordes);
    const porBrillo = regionPorBrillo(gris, w, h);
    if (porBrillo) candidatos.push(porBrillo);

    let mejor = null;
    for (const mask of candidatos) {
      const r = cuadDeRegion(mask, w, h);
      if (r && (!mejor || r.puntaje > mejor.puntaje)) mejor = r;
    }
    if (!mejor || mejor.puntaje < 0.8) return null;
    // Se acercan un poco las esquinas al centro para no dejar un filo de mesa.
    const q = ordenarEsquinas(mejor.quad);
    const cx = q.reduce((s, p) => s + p.x, 0) / 4, cy = q.reduce((s, p) => s + p.y, 0) / 4;
    return q.map(p => ({
      x: Math.min(1, Math.max(0, (p.x + (cx - p.x) * 0.012) / (w - 1))),
      y: Math.min(1, Math.max(0, (p.y + (cy - p.y) * 0.012) / (h - 1))),
    }));
  }

  // Desenfoque de caja separable (radio r) sobre un arreglo de grises.
  function desenfocar(a, w, h, r) {
    const t = new Float32Array(a.length), o = new Float32Array(a.length);
    const n = 2 * r + 1;
    for (let y = 0; y < h; y++) {
      let s = 0;
      const f = y * w;
      for (let x = -r; x <= r; x++) s += a[f + Math.min(w - 1, Math.max(0, x))];
      for (let x = 0; x < w; x++) {
        t[f + x] = s / n;
        s += a[f + Math.min(w - 1, x + r + 1)] - a[f + Math.max(0, x - r)];
      }
    }
    for (let x = 0; x < w; x++) {
      let s = 0;
      for (let y = -r; y <= r; y++) s += t[Math.min(h - 1, Math.max(0, y)) * w + x];
      for (let y = 0; y < h; y++) {
        o[y * w + x] = s / n;
        s += t[Math.min(h - 1, y + r + 1) * w + x] - t[Math.max(0, y - r) * w + x];
      }
    }
    return o;
  }

  // Método 1: bordes (Sobel). Se rellena el fondo desde el contorno de la foto
  // sin cruzar bordes; lo que queda sin rellenar es la hoja.
  function regionPorBordes(g, w, h) {
    const mag = new Float32Array(w * h);
    let suma = 0;
    for (let y = 1; y < h - 1; y++) {
      for (let x = 1; x < w - 1; x++) {
        const i = y * w + x;
        const gx = -g[i - w - 1] - 2 * g[i - 1] - g[i + w - 1] + g[i - w + 1] + 2 * g[i + 1] + g[i + w + 1];
        const gy = -g[i - w - 1] - 2 * g[i - w] - g[i - w + 1] + g[i + w - 1] + 2 * g[i + w] + g[i + w + 1];
        const m = Math.hypot(gx, gy);
        mag[i] = m; suma += m;
      }
    }
    const umbral = Math.max(24, 2.2 * suma / (w * h));
    let borde = new Uint8Array(w * h);
    for (let i = 0; i < mag.length; i++) borde[i] = mag[i] > umbral ? 1 : 0;
    borde = dilatar(borde, w, h);

    const fondo = new Uint8Array(w * h);
    const pila = [];
    const sembrar = i => { if (!borde[i] && !fondo[i]) { fondo[i] = 1; pila.push(i); } };
    for (let x = 0; x < w; x++) { sembrar(x); sembrar((h - 1) * w + x); }
    for (let y = 0; y < h; y++) { sembrar(y * w); sembrar(y * w + w - 1); }
    while (pila.length) {
      const i = pila.pop();
      const x = i % w;
      if (x > 0) sembrar(i - 1);
      if (x < w - 1) sembrar(i + 1);
      if (i >= w) sembrar(i - w);
      if (i < w * (h - 1)) sembrar(i + w);
    }
    const mask = new Uint8Array(w * h);
    for (let i = 0; i < mask.length; i++) mask[i] = fondo[i] ? 0 : 1;
    return mayorComponente(mask, w, h);
  }

  function dilatar(m, w, h) {
    const o = new Uint8Array(w * h);
    for (let y = 0; y < h; y++) {
      for (let x = 0; x < w; x++) {
        if (!m[y * w + x]) continue;
        for (let dy = -1; dy <= 1; dy++) {
          const yy = y + dy;
          if (yy < 0 || yy >= h) continue;
          for (let dx = -1; dx <= 1; dx++) {
            const xx = x + dx;
            if (xx >= 0 && xx < w) o[yy * w + xx] = 1;
          }
        }
      }
    }
    return o;
  }

  // Método 2: brillo (Otsu). La hoja suele ser más clara que la mesa.
  function regionPorBrillo(g, w, h) {
    const t = otsu(g);
    const mask = new Uint8Array(w * h);
    for (let i = 0; i < g.length; i++) mask[i] = g[i] > t ? 1 : 0;
    return mayorComponente(mask, w, h);
  }

  function otsu(g) {
    const hist = new Float64Array(256);
    for (let i = 0; i < g.length; i++) hist[Math.max(0, Math.min(255, g[i] | 0))]++;
    const total = g.length;
    let sumT = 0;
    for (let i = 0; i < 256; i++) sumT += i * hist[i];
    let sumB = 0, wB = 0, max = -1, t = 128;
    for (let i = 0; i < 256; i++) {
      wB += hist[i];
      if (!wB) continue;
      const wF = total - wB;
      if (!wF) break;
      sumB += i * hist[i];
      const mB = sumB / wB, mF = (sumT - sumB) / wF;
      const v = wB * wF * (mB - mF) * (mB - mF);
      if (v > max) { max = v; t = i; }
    }
    return t;
  }

  // Deja solo la región conectada más grande de la máscara.
  function mayorComponente(mask, w, h) {
    const etiqueta = new Int32Array(w * h);
    let mejor = 0, mejorArea = 0, n = 0;
    const pila = [];
    for (let s = 0; s < mask.length; s++) {
      if (!mask[s] || etiqueta[s]) continue;
      n++;
      let area = 0;
      etiqueta[s] = n; pila.push(s);
      while (pila.length) {
        const i = pila.pop();
        area++;
        const x = i % w;
        const vec = [x > 0 ? i - 1 : -1, x < w - 1 ? i + 1 : -1, i - w, i + w];
        for (const j of vec) {
          if (j >= 0 && j < mask.length && mask[j] && !etiqueta[j]) { etiqueta[j] = n; pila.push(j); }
        }
      }
      if (area > mejorArea) { mejorArea = area; mejor = n; }
    }
    if (!mejor) return null;
    const o = new Uint8Array(w * h);
    for (let i = 0; i < o.length; i++) o[i] = etiqueta[i] === mejor ? 1 : 0;
    return o;
  }

  // Ajusta un cuadrilátero a la región: envolvente convexa simplificada a 4
  // vértices. El puntaje indica cuánto se parece la región a ese cuadrilátero.
  function cuadDeRegion(mask, w, h) {
    const pts = [];
    let area = 0;
    for (let y = 0; y < h; y++) {
      let a = -1, b = -1;
      for (let x = 0; x < w; x++) {
        if (mask[y * w + x]) { if (a < 0) a = x; b = x; area++; }
      }
      if (a >= 0) { pts.push({ x: a, y }); if (b !== a) pts.push({ x: b, y }); }
    }
    const frac = area / (w * h);
    if (frac < 0.12 || frac > 0.985) return null;
    let casco = envolvente(pts);
    if (casco.length < 4) return null;
    while (casco.length > 4) {
      let menor = Infinity, k = 0;
      for (let i = 0; i < casco.length; i++) {
        const a = casco[(i - 1 + casco.length) % casco.length], b = casco[i], c = casco[(i + 1) % casco.length];
        const t = Math.abs((b.x - a.x) * (c.y - a.y) - (c.x - a.x) * (b.y - a.y));
        if (t < menor) { menor = t; k = i; }
      }
      casco.splice(k, 1);
    }
    const aq = areaPoligono(casco);
    if (aq <= 0) return null;
    // Igual a 1 si la región coincide con el cuadrilátero; baja si sobra o falta.
    const puntaje = Math.min(area, aq) / Math.max(area, aq);
    return { quad: casco, puntaje };
  }

  function envolvente(p) {
    p = p.slice().sort((a, b) => a.x - b.x || a.y - b.y);
    const cruz = (o, a, b) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);
    const inf = [], sup = [];
    for (const q of p) {
      while (inf.length >= 2 && cruz(inf[inf.length - 2], inf[inf.length - 1], q) <= 0) inf.pop();
      inf.push(q);
    }
    for (let i = p.length - 1; i >= 0; i--) {
      const q = p[i];
      while (sup.length >= 2 && cruz(sup[sup.length - 2], sup[sup.length - 1], q) <= 0) sup.pop();
      sup.push(q);
    }
    sup.pop(); inf.pop();
    return inf.concat(sup);
  }

  function areaPoligono(p) {
    let a = 0;
    for (let i = 0; i < p.length; i++) {
      const q = p[i], r = p[(i + 1) % p.length];
      a += q.x * r.y - r.x * q.y;
    }
    return Math.abs(a) / 2;
  }

  // Ordena 4 puntos como sup-izq, sup-der, inf-der, inf-izq.
  function ordenarEsquinas(q) {
    const cx = q.reduce((s, p) => s + p.x, 0) / 4, cy = q.reduce((s, p) => s + p.y, 0) / 4;
    const r = q.slice().sort((a, b) => Math.atan2(a.y - cy, a.x - cx) - Math.atan2(b.y - cy, b.x - cx));
    // Ahora van en sentido horario empezando por el de ángulo menor; el
    // sup-izq es el de menor x+y.
    let k = 0;
    for (let i = 1; i < 4; i++) if (r[i].x + r[i].y < r[k].x + r[k].y) k = i;
    return [r[k], r[(k + 1) % 4], r[(k + 2) % 4], r[(k + 3) % 4]];
  }

  // ---------- Perspectiva ----------

  // Resuelve la homografía que lleva los puntos de 'de' a 'a' (4 pares).
  function homografia(de, a) {
    const M = [], v = [];
    for (let i = 0; i < 4; i++) {
      const { x, y } = de[i], X = a[i].x, Y = a[i].y;
      M.push([x, y, 1, 0, 0, 0, -x * X, -y * X]); v.push(X);
      M.push([0, 0, 0, x, y, 1, -x * Y, -y * Y]); v.push(Y);
    }
    // Eliminación gaussiana con pivoteo parcial.
    for (let c = 0; c < 8; c++) {
      let p = c;
      for (let r = c + 1; r < 8; r++) if (Math.abs(M[r][c]) > Math.abs(M[p][c])) p = r;
      [M[c], M[p]] = [M[p], M[c]]; [v[c], v[p]] = [v[p], v[c]];
      for (let r = c + 1; r < 8; r++) {
        const f = M[r][c] / M[c][c];
        for (let k = c; k < 8; k++) M[r][k] -= f * M[c][k];
        v[r] -= f * v[c];
      }
    }
    const s = new Array(8);
    for (let r = 7; r >= 0; r--) {
      let t = v[r];
      for (let k = r + 1; k < 8; k++) t -= M[r][k] * s[k];
      s[r] = t / M[r][r];
    }
    return [...s, 1];
  }

  const dist = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);

  // enderezar recorta la hoja definida por las esquinas (normalizadas) y la
  // deja rectangular.
  function enderezar(src, esquinas) {
    const W = src.width, H = src.height;
    const q = esquinas.map(p => ({ x: p.x * (W - 1), y: p.y * (H - 1) }));
    // Promedio de lados opuestos: aproxima la proporción real de la hoja.
    let ow = (dist(q[0], q[1]) + dist(q[3], q[2])) / 2;
    let oh = (dist(q[0], q[3]) + dist(q[1], q[2])) / 2;
    const k = Math.min(1, LADO_SALIDA / Math.max(ow, oh));
    ow = Math.max(8, Math.round(ow * k)); oh = Math.max(8, Math.round(oh * k));

    const sd = src.getContext('2d').getImageData(0, 0, W, H).data;
    const out = lienzo(ow, oh);
    const octx = out.getContext('2d');
    const od = octx.createImageData(ow, oh);
    const d = od.data;
    const T = homografia(
      [{ x: 0, y: 0 }, { x: ow - 1, y: 0 }, { x: ow - 1, y: oh - 1 }, { x: 0, y: oh - 1 }], q);
    let o = 0;
    for (let y = 0; y < oh; y++) {
      for (let x = 0; x < ow; x++, o += 4) {
        const z = T[6] * x + T[7] * y + 1;
        let sx = (T[0] * x + T[1] * y + T[2]) / z;
        let sy = (T[3] * x + T[4] * y + T[5]) / z;
        if (sx < 0) sx = 0; else if (sx > W - 1.001) sx = W - 1.001;
        if (sy < 0) sy = 0; else if (sy > H - 1.001) sy = H - 1.001;
        const x0 = sx | 0, y0 = sy | 0, fx = sx - x0, fy = sy - y0;
        const i00 = (y0 * W + x0) * 4, i10 = i00 + 4, i01 = i00 + W * 4, i11 = i01 + 4;
        for (let c = 0; c < 3; c++) {
          const a = sd[i00 + c] + (sd[i10 + c] - sd[i00 + c]) * fx;
          const b = sd[i01 + c] + (sd[i11 + c] - sd[i01 + c]) * fx;
          d[o + c] = a + (b - a) * fy;
        }
        d[o + 3] = 255;
      }
    }
    octx.putImageData(od, 0, 0);
    return out;
  }

  // girar rota el canvas en múltiplos de 90° (giros: 0..3, sentido horario).
  function girar(src, giros) {
    giros = ((giros % 4) + 4) % 4;
    if (!giros) return src;
    const r = giros % 2 === 1;
    const c = lienzo(r ? src.height : src.width, r ? src.width : src.height);
    const ctx = c.getContext('2d');
    ctx.translate(c.width / 2, c.height / 2);
    ctx.rotate(giros * Math.PI / 2);
    ctx.drawImage(src, -src.width / 2, -src.height / 2);
    liberar(src);
    return c;
  }

  // ---------- Filtros ----------

  const FILTROS = [
    { id: 'documento', nombre: 'Documento' },   // fondo blanco, conserva colores (timbres, firmas)
    { id: 'gris', nombre: 'Gris nítido' },      // escala de grises con trazos reforzados
    { id: 'byn', nombre: 'Blanco y negro' },    // máximo contraste para texto
    { id: 'original', nombre: 'Original' },
  ];

  // Estima la iluminación del papel (sin el texto) a baja resolución: se
  // toma el máximo de cada bloque (borra los trazos oscuros) y se suaviza.
  function fondoPapel(lum, w, h) {
    const B = Math.max(8, Math.round(Math.max(w, h) / 120));
    const bw = Math.ceil(w / B), bh = Math.ceil(h / B);
    let f = new Float32Array(bw * bh);
    for (let by = 0; by < bh; by++) {
      for (let bx = 0; bx < bw; bx++) {
        let m = 0;
        const y1 = Math.min(h, (by + 1) * B), x1 = Math.min(w, (bx + 1) * B);
        for (let y = by * B; y < y1; y++) {
          for (let x = bx * B; x < x1; x++) { const v = lum[y * w + x]; if (v > m) m = v; }
        }
        f[by * bw + bx] = m;
      }
    }
    f = desenfocar(desenfocar(f, bw, bh, 2), bw, bh, 2);
    return { f, bw, bh, B };
  }

  function valorFondo(F, x, y) {
    // Interpolación bilineal entre centros de bloque.
    const gx = Math.min(F.bw - 1, Math.max(0, x / F.B - 0.5));
    const gy = Math.min(F.bh - 1, Math.max(0, y / F.B - 0.5));
    const x0 = gx | 0, y0 = gy | 0;
    const x1 = Math.min(F.bw - 1, x0 + 1), y1 = Math.min(F.bh - 1, y0 + 1);
    const fx = gx - x0, fy = gy - y0;
    const a = F.f[y0 * F.bw + x0] + (F.f[y0 * F.bw + x1] - F.f[y0 * F.bw + x0]) * fx;
    const b = F.f[y1 * F.bw + x0] + (F.f[y1 * F.bw + x1] - F.f[y1 * F.bw + x0]) * fx;
    return Math.max(40, a + (b - a) * fy);
  }

  // Curva que oscurece los trazos y deja blanco el papel.
  function curva(v, negro) {
    // v en 0..1 (1 = papel). Por debajo de 'negro' todo es negro.
    let t = (v - negro) / (0.94 - negro);
    if (t <= 0) return 0;
    if (t >= 1) return 255;
    return 255 * Math.pow(t, 1.5);
  }

  // aplicarFiltro modifica el canvas en el sitio.
  function aplicarFiltro(c, filtro) {
    if (filtro === 'original') return c;
    const w = c.width, h = c.height;
    const ctx = c.getContext('2d');
    const im = ctx.getImageData(0, 0, w, h);
    const d = im.data;
    const n = w * h;
    const lum = new Float32Array(n);
    for (let i = 0, j = 0; i < n; i++, j += 4) lum[i] = 0.299 * d[j] + 0.587 * d[j + 1] + 0.114 * d[j + 2];
    const F = fondoPapel(lum, w, h);

    if (filtro === 'documento') {
      for (let y = 0, j = 0; y < h; y++) {
        for (let x = 0; x < w; x++, j += 4) {
          const bg = valorFondo(F, x, y);
          for (let k = 0; k < 3; k++) d[j + k] = curva(d[j + k] / bg, 0.25);
        }
      }
      ctx.putImageData(im, 0, 0);
      return enfocar(c, 0.6);
    }

    // Gris normalizado (0..1, 1 = papel).
    const norm = new Float32Array(n);
    for (let y = 0, i = 0; y < h; y++) {
      for (let x = 0; x < w; x++, i++) norm[i] = Math.min(1.2, lum[i] / valorFondo(F, x, y));
    }

    if (filtro === 'gris') {
      for (let i = 0, j = 0; i < n; i++, j += 4) {
        const v = curva(norm[i], 0.3);
        d[j] = d[j + 1] = d[j + 2] = v;
      }
      ctx.putImageData(im, 0, 0);
      return enfocar(c, 0.8);
    }

    // Blanco y negro: umbral sobre la imagen normalizada (equivale a un umbral
    // adaptativo: tolera sombras y luz desigual). Se suaviza un poco antes para
    // que los trazos finos del manuscrito no se corten.
    const s = desenfocar(norm, w, h, 1);
    const hist = new Float32Array(n);
    for (let i = 0; i < n; i++) hist[i] = Math.min(255, s[i] * 255);
    let t = otsu(hist) / 255;
    t = Math.min(0.86, Math.max(0.62, t));
    for (let i = 0, j = 0; i < n; i++, j += 4) {
      // Pequeña transición para bordes suaves (sin dientes de sierra).
      const v = Math.max(0, Math.min(255, (s[i] - t) * 255 / 0.06 + 128));
      d[j] = d[j + 1] = d[j + 2] = v;
    }
    ctx.putImageData(im, 0, 0);
    return c;
  }

  // Máscara de enfoque (unsharp mask) con radio 1.
  function enfocar(c, fuerza) {
    const w = c.width, h = c.height;
    const ctx = c.getContext('2d');
    const im = ctx.getImageData(0, 0, w, h);
    const d = im.data;
    const canal = new Float32Array(w * h);
    for (let k = 0; k < 3; k++) {
      for (let i = 0, j = k; i < canal.length; i++, j += 4) canal[i] = d[j];
      const b = desenfocar(canal, w, h, 1);
      for (let i = 0, j = k; i < canal.length; i++, j += 4) d[j] = canal[i] + fuerza * (canal[i] - b[i]);
    }
    ctx.putImageData(im, 0, 0);
    return c;
  }

  // procesar genera la página final: recorte + giro + filtro.
  function procesar(src, esquinas, giros, filtro) {
    let c = enderezar(src, esquinas);
    c = girar(c, giros);
    return aplicarFiltro(c, filtro);
  }

  function aJPEG(c, calidad = 0.85) {
    return new Promise((ok, mal) => c.toBlob(b => b ? ok(b) : mal(new Error('No se pudo generar la imagen.')), 'image/jpeg', calidad));
  }

  function miniatura(c, lado = 360) {
    const m = reducido(c, lado);
    const url = m.toDataURL('image/jpeg', 0.7);
    liberar(m);
    return url;
  }

  return { cargar, detectar, esquinasPorDefecto, enderezar, girar, aplicarFiltro, procesar, aJPEG, miniatura, liberar, reducido, copiar, FILTROS };
})();
