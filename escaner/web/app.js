// Interfaz del escáner: captura, edición de cada página y envío a Unir PDF.
'use strict';

const $ = id => document.getElementById(id);
// Deja que el navegador pinte (p. ej. el aviso "Procesando…") antes de un cálculo pesado.
const respirar = () => new Promise(r => requestAnimationFrame(() => setTimeout(r, 20)));

// Cada página: archivo original, ajustes y resultado ya procesado.
// { file, esquinas, giros, filtro, blob, mini }
const paginas = [];
let enviado = false; // las páginas actuales ya se enviaron

// ---------- Mensajes ----------

function cubierta(texto, { icono = 'girando', botones = [], progreso = null } = {}) {
  $('cubierta').classList.remove('oculto');
  const ic = $('cub-icono');
  ic.className = icono === 'girando' ? 'girando' : '';
  ic.textContent = icono === 'girando' ? '' : icono;
  $('cub-texto').textContent = texto;
  const pr = $('cub-progreso');
  pr.classList.toggle('oculto', progreso === null);
  if (progreso !== null) pr.value = progreso;
  const bx = $('cub-botones');
  bx.innerHTML = '';
  return new Promise(ok => {
    for (const b of botones) {
      const e = document.createElement('button');
      e.type = 'button';
      e.className = 'btn ' + (b.clase || '');
      e.textContent = b.texto;
      e.onclick = () => { cerrarCubierta(); ok(b.valor); };
      bx.appendChild(e);
    }
  });
}
function cerrarCubierta() { $('cubierta').classList.add('oculto'); }
function aviso(texto, icono = '⚠️') { return cubierta(texto, { icono, botones: [{ texto: 'Aceptar', clase: 'primario' }] }); }

// ---------- Conexión con el PC ----------

async function comprobarConexion() {
  const el = $('conexion');
  try {
    const r = await fetch('api/estado', { cache: 'no-store' });
    const j = await r.json();
    el.textContent = '● ' + (j.equipo || 'Conectado');
    el.className = 'conexion ok';
  } catch (e) {
    el.textContent = 'Sin conexión con el PC';
    el.className = 'conexion mal';
  }
}

// ---------- Lista de páginas ----------

function pintarLista() {
  const cont = $('paginas');
  cont.innerHTML = '';
  paginas.forEach((p, i) => {
    const t = document.createElement('div');
    t.className = 'pagina';
    t.innerHTML = `
      <div class="mini"><img alt="Página ${i + 1}"><span class="num">${i + 1}</span></div>
      <div class="herr">
        <button type="button" data-a="subir" aria-label="Mover antes" ${i === 0 ? 'disabled' : ''}>◀</button>
        <button type="button" data-a="bajar" aria-label="Mover después" ${i === paginas.length - 1 ? 'disabled' : ''}>▶</button>
        <button type="button" data-a="editar" aria-label="Editar">✏️</button>
        <button type="button" data-a="repetir" aria-label="Repetir foto">📷</button>
        <button type="button" data-a="quitar" aria-label="Quitar">🗑️</button>
      </div>`;
    t.querySelector('img').src = p.mini;
    t.querySelector('.mini').onclick = () => editarPagina(i);
    t.querySelectorAll('.herr button').forEach(b => b.onclick = () => accionPagina(b.dataset.a, i));
    cont.appendChild(t);
  });
  $('vacio').classList.toggle('oculto', paginas.length > 0);
  const n = paginas.length;
  $('btn-enviar').disabled = n === 0;
  $('btn-enviar').textContent = n ? `Enviar a Unir PDF (${n} ${n === 1 ? 'página' : 'páginas'})` : 'Enviar a Unir PDF';
}

async function accionPagina(a, i) {
  switch (a) {
    case 'subir': [paginas[i - 1], paginas[i]] = [paginas[i], paginas[i - 1]]; break;
    case 'bajar': [paginas[i + 1], paginas[i]] = [paginas[i], paginas[i + 1]]; break;
    case 'editar': return editarPagina(i);
    case 'repetir': return capturar({ reemplazar: i });
    case 'quitar': {
      const si = await cubierta(`¿Quitar la página ${i + 1}?`, {
        icono: '🗑️', botones: [{ texto: 'Cancelar', valor: false }, { texto: 'Quitar', clase: 'peligro', valor: true }],
      });
      if (!si) return;
      paginas.splice(i, 1);
      break;
    }
  }
  enviado = false;
  pintarLista();
}

// ---------- Captura ----------

let destinoCaptura = null; // { reemplazar: índice } o null para una página nueva

function capturar(destino = null) {
  destinoCaptura = destino;
  const inp = $('in-camara');
  inp.value = '';
  inp.click();
}

$('btn-camara').onclick = () => capturar(null);
$('btn-galeria').onclick = () => { destinoCaptura = null; $('in-galeria').value = ''; $('in-galeria').click(); };

$('in-camara').onchange = e => {
  const f = e.target.files[0];
  if (f) abrirEditor(f, destinoCaptura);
};

$('in-galeria').onchange = async e => {
  const files = [...e.target.files];
  if (files.length === 1) return abrirEditor(files[0], null);
  // Varias fotos: se agregan todas con recorte automático; luego se puede
  // tocar cada una para corregirla.
  for (let k = 0; k < files.length; k++) {
    cubierta(`Procesando foto ${k + 1} de ${files.length}…`, { progreso: 100 * k / files.length });
    await respirar();
    try {
      const src = await Imagen.cargar(files[k]);
      const esquinas = Imagen.detectar(src) || Imagen.esquinasPorDefecto();
      const pag = { file: files[k], esquinas, giros: 0, filtro: 'documento' };
      await renderizarPagina(pag, src);
      Imagen.liberar(src);
      paginas.push(pag);
    } catch (err) {
      await aviso(`No se pudo abrir la foto ${k + 1}: ${err.message}`);
    }
  }
  cerrarCubierta();
  enviado = false;
  pintarLista();
};

// Genera el JPEG final y la miniatura de una página.
async function renderizarPagina(pag, src) {
  const c = Imagen.procesar(src, pag.esquinas, pag.giros, pag.filtro);
  pag.blob = await Imagen.aJPEG(c, pag.filtro === 'byn' ? 0.8 : 0.85);
  pag.mini = Imagen.miniatura(c);
  Imagen.liberar(c);
}

// ---------- Editor ----------

const ed = {
  file: null, src: null, disp: null, esquinas: null, giros: 0, filtro: 'documento',
  destino: null,       // { reemplazar: i } | { editar: i } | null
  previaBase: null,    // recorte enderezado en baja resolución (para la vista previa rápida)
  previaUrl: null,
};

async function abrirEditor(file, destino, ajustes = null) {
  cubierta('Abriendo foto…');
  await respirar();
  try {
    liberarEditor();
    ed.file = file;
    ed.destino = destino;
    ed.src = await Imagen.cargar(file);
    ed.disp = Imagen.reducido(ed.src, 1400);
    if (ajustes) {
      ed.esquinas = ajustes.esquinas.map(p => ({ ...p }));
      ed.giros = ajustes.giros;
      ed.filtro = ajustes.filtro;
    } else {
      ed.esquinas = Imagen.detectar(ed.src) || Imagen.esquinasPorDefecto();
      ed.giros = 0;
      // Al repetir una foto se conserva el filtro que tenía la página.
      ed.filtro = destino && destino.reemplazar !== undefined ? paginas[destino.reemplazar].filtro : 'documento';
    }
  } catch (e) {
    cerrarCubierta();
    return aviso('No se pudo abrir la imagen. Prueba a tomar la foto de nuevo.');
  }
  cerrarCubierta();
  $('vista-lista').classList.add('oculto');
  $('vista-editor').classList.remove('oculto');
  mostrarPaso('recorte');
}

function editarPagina(i) {
  const p = paginas[i];
  abrirEditor(p.file, { editar: i }, p);
}

function liberarEditor() {
  Imagen.liberar(ed.src); Imagen.liberar(ed.disp); Imagen.liberar(ed.previaBase);
  ed.src = ed.disp = ed.previaBase = null;
  if (ed.previaUrl) URL.revokeObjectURL(ed.previaUrl);
  ed.previaUrl = null;
}

function cerrarEditor() {
  liberarEditor();
  $('vista-editor').classList.add('oculto');
  $('vista-lista').classList.remove('oculto');
}

function mostrarPaso(p) {
  $('paso-recorte').classList.toggle('oculto', p !== 'recorte');
  $('paso-ajuste').classList.toggle('oculto', p !== 'ajuste');
  $('ed-titulo').textContent = p === 'recorte' ? 'Ajusta los bordes' : 'Gira y mejora';
  if (p === 'recorte') requestAnimationFrame(dibujarRecorte);
  else actualizarPrevia();
}

$('ed-cancelar').onclick = cerrarEditor;
$('ed-auto').onclick = () => {
  const q = Imagen.detectar(ed.src);
  if (!q) aviso('No se encontró la hoja automáticamente. Ajusta los puntos a mano.', 'ℹ️');
  ed.esquinas = q || Imagen.esquinasPorDefecto();
  dibujarRecorte();
};
$('ed-todo').onclick = () => {
  ed.esquinas = [{ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 1, y: 1 }, { x: 0, y: 1 }];
  dibujarRecorte();
};
$('ed-siguiente').onclick = async () => {
  if (!cuadValido(ed.esquinas)) return aviso('Los puntos no forman un recuadro válido. Revisa que estén en las cuatro esquinas.');
  cubierta('Enderezando…');
  await respirar();
  Imagen.liberar(ed.previaBase);
  ed.previaBase = Imagen.enderezar(Imagen.reducido(ed.src, 1300), ed.esquinas);
  cerrarCubierta();
  mostrarPaso('ajuste');
};
$('ed-atras').onclick = () => mostrarPaso('recorte');
$('ed-izq').onclick = () => { ed.giros = (ed.giros + 3) % 4; actualizarPrevia(); };
$('ed-der').onclick = () => { ed.giros = (ed.giros + 1) % 4; actualizarPrevia(); };
$('ed-guardar').onclick = () => guardarPagina(false);
$('ed-otra').onclick = () => guardarPagina(true);

// Botones de filtro.
for (const f of Imagen.FILTROS) {
  const b = document.createElement('button');
  b.type = 'button';
  b.setAttribute('role', 'radio');
  b.dataset.f = f.id;
  b.textContent = f.nombre;
  b.onclick = () => { ed.filtro = f.id; actualizarPrevia(); };
  $('filtros').appendChild(b);
}

let previaPendiente = 0;
async function actualizarPrevia() {
  for (const b of $('filtros').children) b.setAttribute('aria-checked', String(b.dataset.f === ed.filtro));
  const turno = ++previaPendiente;
  await respirar();
  if (turno !== previaPendiente || !ed.previaBase) return;
  let c = Imagen.girar(Imagen.copiar(ed.previaBase), ed.giros);
  c = Imagen.aplicarFiltro(c, ed.filtro);
  const blob = await Imagen.aJPEG(c, 0.8);
  Imagen.liberar(c);
  if (turno !== previaPendiente) return;
  if (ed.previaUrl) URL.revokeObjectURL(ed.previaUrl);
  ed.previaUrl = URL.createObjectURL(blob);
  $('ed-previa').src = ed.previaUrl;
}

async function guardarPagina(otra) {
  cubierta('Guardando página…');
  await respirar();
  const pag = { file: ed.file, esquinas: ed.esquinas, giros: ed.giros, filtro: ed.filtro };
  try {
    await renderizarPagina(pag, ed.src);
  } catch (e) {
    cerrarCubierta();
    return aviso('No se pudo procesar la página: ' + e.message);
  }
  const d = ed.destino;
  if (d && d.reemplazar !== undefined) paginas[d.reemplazar] = pag;
  else if (d && d.editar !== undefined) paginas[d.editar] = pag;
  else paginas.push(pag);
  enviado = false;
  cerrarCubierta();
  cerrarEditor();
  pintarLista();
  if (otra) capturar(null);
}

function cuadValido(q) {
  // Convexo y con un área mínima.
  let signo = 0;
  for (let i = 0; i < 4; i++) {
    const a = q[i], b = q[(i + 1) % 4], c = q[(i + 2) % 4];
    const z = (b.x - a.x) * (c.y - b.y) - (b.y - a.y) * (c.x - b.x);
    if (Math.abs(z) < 1e-6) return false;
    const s = Math.sign(z);
    if (signo && s !== signo) return false;
    signo = s;
  }
  let area = 0;
  for (let i = 0; i < 4; i++) area += q[i].x * q[(i + 1) % 4].y - q[(i + 1) % 4].x * q[i].y;
  return Math.abs(area) / 2 > 0.02;
}

// ---------- Lienzo de recorte (arrastrar esquinas) ----------

const lien = $('lienzo-recorte');
const vistaR = { s: 1, ox: 0, oy: 0, dpr: 1 }; // escala y desplazamiento de la imagen en el lienzo
let arrastre = null; // { i, dx, dy }

function dibujarRecorte() {
  if (!ed.disp) return;
  const caja = $('lienzo-caja').getBoundingClientRect();
  const dpr = Math.min(2, window.devicePixelRatio || 1);
  lien.width = Math.round(caja.width * dpr);
  lien.height = Math.round(caja.height * dpr);
  const m = 24 * dpr;
  const s = Math.min((lien.width - 2 * m) / ed.disp.width, (lien.height - 2 * m) / ed.disp.height);
  Object.assign(vistaR, {
    s, dpr,
    ox: (lien.width - ed.disp.width * s) / 2,
    oy: (lien.height - ed.disp.height * s) / 2,
  });
  const ctx = lien.getContext('2d');
  ctx.clearRect(0, 0, lien.width, lien.height);
  ctx.drawImage(ed.disp, vistaR.ox, vistaR.oy, ed.disp.width * s, ed.disp.height * s);

  const P = ed.esquinas.map(aLienzo);
  // Oscurece lo que queda fuera de la hoja.
  ctx.save();
  ctx.beginPath();
  ctx.rect(vistaR.ox, vistaR.oy, ed.disp.width * s, ed.disp.height * s);
  ctx.moveTo(P[0].x, P[0].y);
  for (let i = 3; i >= 0; i--) ctx.lineTo(P[i].x, P[i].y);
  ctx.closePath();
  ctx.fillStyle = 'rgba(0,0,0,0.5)';
  ctx.fill('evenodd');
  ctx.restore();

  ctx.beginPath();
  P.forEach((p, i) => i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y));
  ctx.closePath();
  ctx.lineWidth = 2.5 * dpr;
  ctx.strokeStyle = '#4da3ff';
  ctx.stroke();
  for (let i = 0; i < 4; i++) {
    ctx.beginPath();
    ctx.arc(P[i].x, P[i].y, (arrastre && arrastre.i === i ? 16 : 12) * dpr, 0, 2 * Math.PI);
    ctx.fillStyle = 'rgba(31,95,168,0.85)';
    ctx.fill();
    ctx.lineWidth = 3 * dpr;
    ctx.strokeStyle = '#fff';
    ctx.stroke();
  }
}

function aLienzo(p) {
  return { x: vistaR.ox + p.x * ed.disp.width * vistaR.s, y: vistaR.oy + p.y * ed.disp.height * vistaR.s };
}
function deLienzo(x, y) {
  return {
    x: Math.min(1, Math.max(0, (x - vistaR.ox) / (ed.disp.width * vistaR.s))),
    y: Math.min(1, Math.max(0, (y - vistaR.oy) / (ed.disp.height * vistaR.s))),
  };
}
function punteroEnLienzo(e) {
  const r = lien.getBoundingClientRect();
  return { x: (e.clientX - r.left) * vistaR.dpr, y: (e.clientY - r.top) * vistaR.dpr };
}

lien.addEventListener('pointerdown', e => {
  if (!ed.disp) return;
  const p = punteroEnLienzo(e);
  const P = ed.esquinas.map(aLienzo);
  let mejor = -1, d = Infinity;
  P.forEach((q, i) => { const k = Math.hypot(q.x - p.x, q.y - p.y); if (k < d) { d = k; mejor = i; } });
  if (d > 70 * vistaR.dpr) return;
  arrastre = { i: mejor, dx: P[mejor].x - p.x, dy: P[mejor].y - p.y };
  lien.setPointerCapture(e.pointerId);
  e.preventDefault();
  dibujarRecorte();
  dibujarLupa();
});
lien.addEventListener('pointermove', e => {
  if (!arrastre) return;
  const p = punteroEnLienzo(e);
  ed.esquinas[arrastre.i] = deLienzo(p.x + arrastre.dx, p.y + arrastre.dy);
  dibujarRecorte();
  dibujarLupa();
  e.preventDefault();
});
const soltar = () => { arrastre = null; $('lupa').classList.add('oculto'); dibujarRecorte(); };
lien.addEventListener('pointerup', soltar);
lien.addEventListener('pointercancel', soltar);
window.addEventListener('resize', () => { if (!$('paso-recorte').classList.contains('oculto')) dibujarRecorte(); });

// Lupa: muestra ampliada la zona de la esquina que se arrastra (el dedo la tapa).
function dibujarLupa() {
  const lupa = $('lupa');
  const q = ed.esquinas[arrastre.i];
  lupa.classList.remove('oculto');
  lupa.classList.toggle('derecha', q.x < 0.5);
  const ctx = lupa.getContext('2d');
  const L = lupa.width;
  const zoom = 3 * vistaR.s * vistaR.dpr; // píxeles de lupa por píxel de 'disp'
  const cx = q.x * ed.disp.width, cy = q.y * ed.disp.height;
  const r = L / 2 / zoom;
  ctx.fillStyle = '#000';
  ctx.fillRect(0, 0, L, L);
  ctx.imageSmoothingEnabled = true;
  ctx.drawImage(ed.disp, cx - r, cy - r, 2 * r, 2 * r, 0, 0, L, L);
  ctx.strokeStyle = '#4da3ff';
  ctx.lineWidth = 3;
  ctx.beginPath();
  ctx.moveTo(L / 2, L / 2 - 26); ctx.lineTo(L / 2, L / 2 + 26);
  ctx.moveTo(L / 2 - 26, L / 2); ctx.lineTo(L / 2 + 26, L / 2);
  ctx.stroke();
}

// ---------- Envío ----------

$('btn-enviar').onclick = enviar;

async function enviar() {
  const inp = $('paciente');
  const paciente = inp.value.trim().replace(/\s+/g, ' ');
  if (!paciente) {
    inp.classList.add('falta');
    inp.focus();
    return aviso('Escribe el nombre del paciente antes de enviar.');
  }
  inp.classList.remove('falta');
  if (!paginas.length) return;

  const fd = new FormData();
  fd.append('paciente', paciente);
  paginas.forEach((p, i) => fd.append('paginas', p.blob, `pagina${String(i + 1).padStart(3, '0')}.jpg`));

  cubierta('Enviando al PC…', { progreso: 0 });
  let res;
  try {
    res = await new Promise((ok, mal) => {
      const x = new XMLHttpRequest();
      x.open('POST', 'api/escaneo');
      x.upload.onprogress = e => { if (e.lengthComputable) $('cub-progreso').value = 100 * e.loaded / e.total; };
      x.upload.onload = () => { $('cub-texto').textContent = 'Creando el PDF en el PC…'; };
      x.onload = () => {
        let j = null;
        try { j = JSON.parse(x.responseText); } catch (e) { /* respuesta no JSON */ }
        if (x.status === 200 && j && j.ok) ok(j);
        else mal(new Error((j && j.error) || `El PC respondió con un error (${x.status}).`));
      };
      x.onerror = () => mal(new Error('No se pudo conectar con el PC. Comprueba que Unir PDF esté abierto y que el teléfono esté en la red del policlínico.'));
      x.send(fd);
    });
  } catch (e) {
    const otra = await cubierta(e.message, {
      icono: '❌', botones: [{ texto: 'Cerrar', valor: false }, { texto: 'Reintentar', clase: 'primario', valor: true }],
    });
    if (otra) enviar();
    return;
  }
  enviado = true;
  const nuevo = await cubierta(
    `Enviado correctamente.\n\nEn el PC se guardó «${res.archivo}» (${res.paginas} ${res.paginas === 1 ? 'página' : 'páginas'}) y ya aparece en la lista de Unir PDF.`,
    { icono: '✅', botones: [{ texto: 'Seguir aquí', valor: false }, { texto: 'Nuevo paciente', clase: 'primario', valor: true }] });
  if (nuevo) {
    paginas.length = 0;
    $('paciente').value = '';
    enviado = false;
    pintarLista();
    window.scrollTo(0, 0);
  }
}

window.addEventListener('beforeunload', e => {
  if (paginas.length && !enviado) { e.preventDefault(); e.returnValue = ''; }
});

// ---------- Inicio ----------

pintarLista();
comprobarConexion();
setInterval(comprobarConexion, 15000);
