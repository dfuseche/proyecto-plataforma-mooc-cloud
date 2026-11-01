// Escenario 3B de la Entrega 3: ciclo de autoscaling del grupo de workers
// (mooc-worker-mig), que escala por la cola (backlog = pending + active, 2
// trabajos por VM). Guia: docs/entrega3/guia-escenario3-autoscaling.md (C13,
// secciones 5.2, 7 y 7.1).
//
// Reutiliza de media-load-test.js la subida de un video (crear recurso, PUT
// directo al bucket con la URL prefirmada, complete-upload) y la espera del
// manifiesto HLS. Cada iteracion es UN trabajo de transcodificacion: el
// executor de llegadas fija cuantos trabajos entran por unidad de tiempo, sin
// importar cuanto tarden los workers en procesarlos, asi que la cola crece
// cuando la llegada supera la capacidad de los workers activos.
//
// Modos (MODO):
//   ciclo (default)  base -> subida -> sostenido -> corte (0 llegadas, la cola drena) (~43 min)
//   rafaga           sube N trabajos de una vez y espera a que terminen: calibracion de la
//                    capacidad de 1 worker (5.2, N=6) y retiro controlado 3B-R (7.1, N=1)
//
// Uso (desde la VM generadora):
//   k6 run --insecure-skip-tls-verify -e BASE_URL=https://34.49.211.71 \
//     -e PROFILE=pesado -e TIME_UNIT=10m -e LOW_RATE=5 -e HIGH_RATE=25 \
//     -e POLL_INTERVAL_SECONDS=10 -e MAX_WAIT_FOR_MANIFEST_SECONDS=1800 \
//     --summary-export=summary.json --console-output=k6_console.log loadtests/k6/autoscaling-workers.js
//
// Variables de entorno (ademas de las de media-load-test.js: UNIT_ID, TEACHER_EMAIL, ...):
//   MODO               ciclo | rafaga (default ciclo)
//   PROFILE            ligero | medio | pesado (default pesado: 1080p, 4 renditions, costo estable)
//   TIME_UNIT          unidad de LOW_RATE y HIGH_RATE (default 1m; 10m para tasas fraccionarias)
//   LOW_RATE           trabajos por TIME_UNIT en base (default 1)
//   HIGH_RATE          trabajos por TIME_UNIT en sostenido (obligatorio en MODO=ciclo)
//   BASE_MIN, SUBIDA_MIN, SOSTENIDO_MIN, DRENAJE_MIN
//                      duracion de cada fase en minutos (default 5, 1, 12, 25)
//   N                  trabajos de la rafaga (default 6)
//   POLL_INTERVAL_SECONDS, MAX_WAIT_FOR_MANIFEST_SECONDS
//                      obligatorias (la guia usa 10 y 1800). Con los defaults de
//                      media-load-test.js (3 s y 180 s) el polling de ~100 VUs sube la CPU
//                      de la API y los trabajos en cola se darian por perdidos a los 3 min
//   PRE_VUS, MAX_VUS   VUs del executor (default: calculados con HIGH_RATE y la espera maxima)
//
// Salida en consola (--console-output), para la reconciliacion de trabajos:
//   FASE,<iso>,<fase>                              al empezar cada fase (corte = T6)
//   JOB,<iso>,<resource_id>,aceptado               complete-upload respondio 200 (trabajo encolado)
//   JOB,<iso>,<resource_id>,disponible|timeout     el manifiesto quedo listo o se agoto la espera
//   JOB,<iso>,,rechazado                           no se pudo crear, subir o encolar el trabajo

import exec from 'k6/execution';
import { sleep, fail } from 'k6';
import { Counter } from 'k6/metrics';
import {
  crearYSubirVideo,
  esperarManifiesto,
  getTeacherToken,
  profileByName,
  MAX_WAIT_FOR_MANIFEST_SECONDS,
} from './media-load-test.js';

const MODO = (__ENV.MODO || 'ciclo').toLowerCase();
const PERFIL = profileByName(__ENV.PROFILE || 'pesado');
const TIME_UNIT = __ENV.TIME_UNIT || '1m';
const LOW_RATE = Number(__ENV.LOW_RATE || 1);
const HIGH_RATE = Number(__ENV.HIGH_RATE || 0);
const N = Number(__ENV.N || 6);
// La reconciliacion de 3B filtra los recursos por este prefijo de titulo
// (`Autoscaling 3B %`); las rafagas usan otro para no mezclarse con el ciclo.
const PREFIJO = MODO === 'rafaga' ? 'Autoscaling rafaga' : 'Autoscaling 3B';

if (MODO !== 'ciclo' && MODO !== 'rafaga') {
  fail(`MODO desconocido: ${MODO}. Usa ciclo o rafaga.`);
}
if (!PERFIL) {
  fail(`PROFILE desconocido: ${__ENV.PROFILE}. Usa ligero, medio o pesado.`);
}
if (!__ENV.POLL_INTERVAL_SECONDS || !__ENV.MAX_WAIT_FOR_MANIFEST_SECONDS) {
  fail('Define -e POLL_INTERVAL_SECONDS=10 -e MAX_WAIT_FOR_MANIFEST_SECONDS=1800 (ver la cabecera del script).');
}
if (MODO === 'ciclo' && !(HIGH_RATE > LOW_RATE)) {
  fail('Define HIGH_RATE (mayor que LOW_RATE) con el valor de la calibracion, ej: -e TIME_UNIT=10m -e HIGH_RATE=25');
}

// TIME_UNIT en minutos ("30s", "1m", "10m", "1h"), para dimensionar los VUs.
function unidadEnMinutos(unidad) {
  const m = /^(\d+(?:\.\d+)?)(s|m|h)$/.exec(unidad);
  if (!m) fail(`TIME_UNIT invalido: ${unidad}. Usa, por ejemplo, 1m o 10m.`);
  const valor = Number(m[1]);
  return m[2] === 's' ? valor / 60 : m[2] === 'h' ? valor * 60 : valor;
}

function minutos(nombre, porDefecto) {
  return Number(__ENV[nombre] || porDefecto) * 60;
}

// Fases del ciclo, con su duracion en segundos. "corte" es la ventana de
// drenaje: la llegada cae a 0 de golpe y los VUs siguen esperando sus trabajos.
const FASES =
  MODO === 'rafaga'
    ? [{ nombre: 'rafaga', segundos: MAX_WAIT_FOR_MANIFEST_SECONDS + 600 }]
    : [
        { nombre: 'base', segundos: minutos('BASE_MIN', 5) },
        { nombre: 'subida', segundos: minutos('SUBIDA_MIN', 1) },
        { nombre: 'sostenido', segundos: minutos('SOSTENIDO_MIN', 12) },
        { nombre: 'corte', segundos: minutos('DRENAJE_MIN', 25) },
      ];
const DURACION_TOTAL = FASES.reduce((acum, f) => acum + f.segundos, 0);

// Trabajos simultaneos en espera = llegadas por minuto x minutos de espera.
// Se dimensiona con la espera maxima; cada VU carga los 3 videos en memoria
// (~20 MB), por eso no se preasignan todos.
const LLEGADAS_POR_MINUTO = (MODO === 'rafaga' ? N : HIGH_RATE) / unidadEnMinutos(TIME_UNIT);
const VUS_MAXIMOS =
  MODO === 'rafaga' ? N : Math.ceil(LLEGADAS_POR_MINUTO * (MAX_WAIT_FOR_MANIFEST_SECONDS / 60)) + 5;

const trabajosAceptados = new Counter('trabajos_aceptados');
const trabajosDisponibles = new Counter('trabajos_disponibles');
const trabajosTimeout = new Counter('trabajos_timeout');
const trabajosRechazados = new Counter('trabajos_rechazados');

// Umbrales "vacios" por fase: --summary-export trae el tiempo hasta available
// (media_processing_duration) de los trabajos que llegaron en cada fase.
const thresholds = {
  trabajos_aceptados: ['count>=0'],
  trabajos_disponibles: ['count>=0'],
  trabajos_timeout: ['count>=0'],
  trabajos_rechazados: ['count>=0'],
  media_processing_timeouts: ['count>=0'],
  dropped_iterations: ['count>=0'],
  'http_req_duration{endpoint:create_resource}': ['max>=0'],
  'http_req_duration{endpoint:upload_put}': ['max>=0'],
  'http_req_duration{endpoint:complete_upload}': ['max>=0'],
  'http_req_duration{endpoint:stream_url}': ['max>=0'],
  'http_req_failed{endpoint:stream_url}': ['rate>=0'],
};
for (const f of FASES.filter((fase) => fase.nombre !== 'corte')) {
  thresholds[`media_processing_duration{fase:${f.nombre}}`] = ['max>=0'];
}

const scenarios =
  MODO === 'rafaga'
    ? {
        trabajos: {
          executor: 'shared-iterations',
          exec: 'trabajo',
          vus: N,
          iterations: N,
          maxDuration: `${DURACION_TOTAL}s`,
        },
      }
    : {
        trabajos: {
          executor: 'ramping-arrival-rate',
          exec: 'trabajo',
          startRate: LOW_RATE,
          timeUnit: TIME_UNIT,
          preAllocatedVUs: Number(__ENV.PRE_VUS || Math.ceil(VUS_MAXIMOS / 2)),
          maxVUs: Number(__ENV.MAX_VUS || VUS_MAXIMOS),
          stages: [
            { duration: `${FASES[0].segundos}s`, target: LOW_RATE },
            { duration: `${FASES[1].segundos}s`, target: HIGH_RATE },
            { duration: `${FASES[2].segundos}s`, target: HIGH_RATE },
            { duration: '1s', target: 0 },
            { duration: `${FASES[3].segundos - 1}s`, target: 0 },
          ],
          // No cortar los trabajos que siguen esperando al terminar el drenaje.
          gracefulStop: '30m',
        },
        marcas_fase: {
          executor: 'per-vu-iterations',
          exec: 'marcarFases',
          vus: 1,
          iterations: 1,
          maxDuration: `${DURACION_TOTAL + 120}s`,
        },
      };

export const options = {
  insecureSkipTLSVerify: true,
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios,
  thresholds,
};

function iso() {
  return new Date().toISOString();
}

function faseActual() {
  const transcurrido = (Date.now() - exec.scenario.startTime) / 1000;
  let acumulado = 0;
  for (const f of FASES) {
    acumulado += f.segundos;
    if (transcurrido < acumulado) return f.nombre;
  }
  return FASES[FASES.length - 1].nombre;
}

export function setup() {
  const token = getTeacherToken();
  if (!token) {
    fail('No se pudo autenticar al profesor de carga. Corre loadtests/seed/seed_load_test_data.sql.');
  }
  console.log(
    `Escenario 3B, MODO=${MODO}, perfil ${PERFIL.name}: ` +
    (MODO === 'rafaga'
      ? `${N} trabajos de una vez`
      : `${FASES.map((f) => `${f.nombre}=${f.segundos}s`).join(' ')}, ${LOW_RATE} -> ${HIGH_RATE} por ${TIME_UNIT}`) +
    `, hasta ${VUS_MAXIMOS} VUs`
  );
  if (MODO === 'rafaga') console.log(`FASE,${iso()},rafaga`);
  return { token };
}

export function trabajo(data) {
  // La fase de llegada queda en todas las metricas del trabajo, incluido
  // media_processing_duration aunque termine en una fase posterior.
  exec.vu.metrics.tags.fase = MODO === 'rafaga' ? 'rafaga' : faseActual();

  const resourceId = crearYSubirVideo(PREFIJO, PERFIL, data.token);
  if (!resourceId) {
    trabajosRechazados.add(1);
    console.log(`JOB,${iso()},,rechazado`);
    return;
  }
  trabajosAceptados.add(1);
  console.log(`JOB,${iso()},${resourceId},aceptado`);

  const manifestUrl = esperarManifiesto(resourceId, PERFIL, false);
  if (manifestUrl) {
    trabajosDisponibles.add(1);
    console.log(`JOB,${iso()},${resourceId},disponible`);
  } else {
    trabajosTimeout.add(1);
    console.log(`JOB,${iso()},${resourceId},timeout`);
  }
}

export function marcarFases() {
  for (const f of FASES) {
    console.log(`FASE,${iso()},${f.nombre}`);
    sleep(f.segundos);
  }
  console.log(`FASE,${iso()},fin`);
}
