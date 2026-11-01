// Escenario 3A de la Entrega 3: ciclo de autoscaling del grupo de la API
// (mooc-api-mig). Guia: docs/entrega3/guia-escenario3-autoscaling.md (C12,
// secciones 5.1 y 6).
//
// Reutiliza el trafico del Escenario 1 (load-test.js: catalogo, inscripcion,
// heartbeat, quiz con envio duplicado e insignias). k6 ignora el `options`
// del modulo importado, pero conserva los responseCallback de los 409
// esperados y la metrica quiz_double_grading_detected.
//
// La demanda se fija con un executor de llegadas (ramping-arrival-rate):
// modelo abierto, la tasa no cae cuando sube la latencia, asi que la CPU de
// la replica sube de verdad hasta provocar la creacion de otra instancia.
//
// Modos (MODO):
//   ciclo (default)  base -> subida -> sostenido -> bajada -> enfriamiento (~45 min)
//   calibracion      escalones STEP_RATES de STEP_MINUTES cada uno, para
//                    encontrar HIGH_RATE con el autoscaler en modo off (5.1)
//
// Uso (desde la VM generadora, nunca desde las VMs de la aplicacion):
//   k6 run --insecure-skip-tls-verify -e BASE_URL=https://34.49.211.71 \
//     -e LOW_RATE=3 -e HIGH_RATE=30 -e POOL_SIZE=300 -e QUIZ_ATTEMPT_FRACTION=0.05 \
//     --summary-export=summary.json --console-output=k6_console.log loadtests/k6/autoscaling-api.js
//
// Variables de entorno (ademas de las de load-test.js: POOL_SIZE, COURSE_ID,
// QUIZ_ATTEMPT_FRACTION, ...):
//   MODO               ciclo | calibracion (default ciclo)
//   LOW_RATE           iteraciones/s en base y enfriamiento (default 3)
//   HIGH_RATE          iteraciones/s en sostenido (obligatorio en MODO=ciclo; sale de la calibracion)
//   BASE_MIN, SUBIDA_MIN, SOSTENIDO_MIN, BAJADA_MIN, ENFRIAMIENTO_MIN
//                      duracion de cada fase en minutos (default 5, 2, 15, 1, 22). Solo
//                      se cambian para un humo corto; la corrida oficial usa los defaults
//   STEP_RATES         escalones de la calibracion, en iteraciones/s (default 5,10,20,30,40,50)
//   STEP_MINUTES       duracion de cada escalon (default 2)
//   SONDA_RATE         peticiones/s de la sonda de instancias (default 2)
//   PRE_VUS, MAX_VUS   VUs del executor (default 3x y 8x la tasa maxima)
//
// Salida en consola (--console-output), para la linea de tiempo del informe:
//   FASE,<iso>,<fase>                                   al empezar cada fase (marca T0 y T6)
//   SONDA,<iso>,<fase>,<status>,<X-Served-By>,<X-App-Version>,<ms>   2 por segundo

import http from 'k6/http';
import exec from 'k6/execution';
import { sleep, fail } from 'k6';
import iteracion, { setup as setupEscenario1, BASE_URL, COURSE_ID } from './load-test.js';

const MODO = (__ENV.MODO || 'ciclo').toLowerCase();
const LOW_RATE = Number(__ENV.LOW_RATE || 3);
const HIGH_RATE = Number(__ENV.HIGH_RATE || 0);
const STEP_RATES = (__ENV.STEP_RATES || '5,10,20,30,40,50')
  .split(',')
  .map((r) => Number(r.trim()))
  .filter((r) => r > 0);
const STEP_MINUTES = Number(__ENV.STEP_MINUTES || 2);
const SONDA_RATE = Number(__ENV.SONDA_RATE || 2);

if (MODO !== 'ciclo' && MODO !== 'calibracion') {
  fail(`MODO desconocido: ${MODO}. Usa ciclo o calibracion.`);
}
if (MODO === 'ciclo' && !(HIGH_RATE > LOW_RATE)) {
  fail('Define HIGH_RATE (mayor que LOW_RATE) con el valor de la calibracion, ej: -e HIGH_RATE=30');
}

function minutos(nombre, porDefecto) {
  return Number(__ENV[nombre] || porDefecto) * 60;
}

// Fases en orden, con su duracion en segundos y la tasa al terminar la fase
// (el executor hace una rampa lineal desde la tasa de la fase anterior).
const FASES =
  MODO === 'calibracion'
    ? STEP_RATES.map((rate) => ({ nombre: `escalon_${rate}`, segundos: STEP_MINUTES * 60, rate }))
    : [
        { nombre: 'base', segundos: minutos('BASE_MIN', 5), rate: LOW_RATE },
        { nombre: 'subida', segundos: minutos('SUBIDA_MIN', 2), rate: HIGH_RATE },
        { nombre: 'sostenido', segundos: minutos('SOSTENIDO_MIN', 15), rate: HIGH_RATE },
        { nombre: 'bajada', segundos: minutos('BAJADA_MIN', 1), rate: LOW_RATE },
        // Cubre la estabilizacion de 10 min del autoscaler y la reduccion 3 -> 2 -> 1.
        { nombre: 'enfriamiento', segundos: minutos('ENFRIAMIENTO_MIN', 22), rate: LOW_RATE },
      ];

const DURACION_TOTAL = FASES.reduce((acum, f) => acum + f.segundos, 0);
const TASA_MAXIMA = Math.max(...FASES.map((f) => f.rate));

// Escalones: 10 s para saltar a la tasa nueva y el resto sostenido. En el
// ciclo cada fase es un solo tramo (rampa en subida y bajada, plano en el resto).
const STAGES =
  MODO === 'calibracion'
    ? FASES.flatMap((f) => [
        { duration: '10s', target: f.rate },
        { duration: `${f.segundos - 10}s`, target: f.rate },
      ])
    : FASES.map((f) => ({ duration: `${f.segundos}s`, target: f.rate }));

// Umbrales "vacios" (siempre se cumplen) por fase: hacen que --summary-export
// traiga latencia, throughput y errores de cada fase por separado. El filtro
// scenario:trafico deja fuera la sonda.
const thresholds = {
  quiz_double_grading_detected: ['count==0'],
  dropped_iterations: ['count>=0'],
  'http_req_duration{scenario:sonda_instancias}': ['max>=0'],
};
for (const f of FASES) {
  thresholds[`http_req_duration{scenario:trafico,fase:${f.nombre}}`] = ['max>=0'];
  thresholds[`http_req_failed{scenario:trafico,fase:${f.nombre}}`] = ['rate>=0'];
  thresholds[`http_reqs{scenario:trafico,fase:${f.nombre}}`] = ['count>=0'];
  thresholds[`iterations{scenario:trafico,fase:${f.nombre}}`] = ['count>=0'];
}

export const options = {
  insecureSkipTLSVerify: true,
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  // El setup de load-test.js inicia sesion con el pool espaciado por el
  // limite de tasa: ~3,5 min con 300 usuarios.
  setupTimeout: '10m',
  scenarios: {
    trafico: {
      executor: 'ramping-arrival-rate',
      exec: 'trafico',
      startRate: FASES[0].rate,
      timeUnit: '1s',
      // Cada iteracion dura de 1 a 3 s por el think time de load-test.js.
      preAllocatedVUs: Number(__ENV.PRE_VUS || Math.ceil(TASA_MAXIMA * 3)),
      maxVUs: Number(__ENV.MAX_VUS || Math.ceil(TASA_MAXIMA * 8)),
      stages: STAGES,
    },
    // Muestra de a que replica entrega el balanceador: reparte igual todas
    // las peticiones, asi que la sonda refleja la distribucion real.
    sonda_instancias: {
      executor: 'constant-arrival-rate',
      exec: 'sonda',
      rate: SONDA_RATE,
      timeUnit: '1s',
      duration: `${DURACION_TOTAL}s`,
      preAllocatedVUs: 4,
      maxVUs: 20,
    },
    // Un solo VU que escribe FASE,<iso>,<fase> al empezar cada fase.
    marcas_fase: {
      executor: 'per-vu-iterations',
      exec: 'marcarFases',
      vus: 1,
      iterations: 1,
      maxDuration: `${DURACION_TOTAL + 120}s`,
    },
  },
  thresholds,
};

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
  if (!__ENV.QUIZ_ATTEMPT_FRACTION) {
    console.log(
      'Advertencia: QUIZ_ATTEMPT_FRACTION no esta definida y load-test.js usa 0.3. ' +
      'En 3A los intentos del pool se agotan en minutos; la guia usa -e QUIZ_ATTEMPT_FRACTION=0.05.'
    );
  }
  console.log(
    `Escenario 3A, MODO=${MODO}: ${FASES.map((f) => `${f.nombre}=${f.segundos}s@${f.rate}/s`).join(' ')}`
  );
  return setupEscenario1();
}

export function trafico(data) {
  // La etiqueta queda en todas las metricas de la iteracion (incluidas las
  // propias de load-test.js, como quiz_double_grading_detected).
  exec.vu.metrics.tags.fase = faseActual();
  iteracion(data);
}

export function sonda() {
  const fase = faseActual();
  const res = http.get(`${BASE_URL}/api/v1/courses/${COURSE_ID}`, {
    tags: { endpoint: 'sonda', fase },
  });
  console.log(
    `SONDA,${new Date().toISOString()},${fase},${res.status},` +
    `${res.headers['X-Served-By'] || ''},${res.headers['X-App-Version'] || ''},${Math.round(res.timings.duration)}`
  );
}

export function marcarFases() {
  for (const f of FASES) {
    console.log(`FASE,${new Date().toISOString()},${f.nombre}`);
    sleep(f.segundos);
  }
  console.log(`FASE,${new Date().toISOString()},fin`);
}
