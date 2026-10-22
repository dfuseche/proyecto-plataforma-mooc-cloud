// Prueba de carga de los flujos criticos de la Plataforma MOOC.
//
// Requiere que loadtests/seed/seed_load_test_data.sql ya se haya corrido
// contra la base de datos objetivo (crea 300 estudiantes verificados,
// un curso publicado, un quiz de practica y una insignia de muestra).
//
// Uso basico:
//   k6 run -e BASE_URL=https://api.tu-dominio.com loadtests/k6/load-test.js
//
// Variables de entorno soportadas (todas opcionales salvo BASE_URL):
//   BASE_URL              URL publica de la API (obligatorio, sin / al final)
//   POOL_SIZE             cuantos usuarios del pool autenticar en el setup (default 300)
//   MAX_VUS               techo de usuarios virtuales concurrentes (default 2000)
//   COURSE_ID              curso publicado a usar (default: el sembrado por el seed SQL)
//   RESOURCE_STABLE_ID     stable_id del recurso a usar en el heartbeat
//   BADGE_CODE             verification_code de una insignia ya emitida
//   QUIZ_ID                id del quiz a presentar (default: el sembrado por el seed SQL)
//
// Ejemplo corriendo un humo rapido antes del run completo de 2000 VUs:
//   k6 run -e BASE_URL=https://api.tu-dominio.com -e MAX_VUS=20 loadtests/k6/load-test.js

import http from 'k6/http';
import { check, sleep, fail } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL;
if (!BASE_URL) {
  fail('Define BASE_URL, ej: k6 run -e BASE_URL=https://api.tu-dominio.com loadtests/k6/load-test.js');
}

const POOL_SIZE = Number(__ENV.POOL_SIZE || 300);
const MAX_VUS = Number(__ENV.MAX_VUS || 2000);
const PASSWORD = 'LoadTest123!';
const COURSE_ID = __ENV.COURSE_ID || '10000000-0000-0000-0000-000000000001';
const RESOURCE_STABLE_ID = __ENV.RESOURCE_STABLE_ID || '10000000-0000-0000-0000-000000000015';
const BADGE_CODE = __ENV.BADGE_CODE || '10000000-0000-0000-0000-000000000199';
const QUIZ_ID = __ENV.QUIZ_ID || '10000000-0000-0000-0000-000000000007';

// El rate limiter del backend permite 100 req/min por IP en las rutas de
// auth (ver internal/middleware/rate_limit.go). El pool se loguea UNA
// SOLA VEZ en el setup, espaciado para quedar bajo ese limite, y los 2000
// VUs reutilizan esos tokens durante todo el run — no se vuelve a loguear
// nadie durante la prueba en si.
const LOGIN_PACING_SECONDS = 60 / 90; // ~90 logins/min, con margen bajo el limite de 100

// Fraccion de las iteraciones de aprendizaje que ademas presentan el quiz.
// No es 100% porque cada token del pool solo tiene max_attempts=3 en el
// quiz sembrado (ver seed_load_test_data.sql): si todas las iteraciones lo
// intentaran, la mayoria de los tokens agotarian sus intentos a los pocos
// minutos y el resto del run solo mediria rechazos 409 esperados en vez de
// intentos reales.
const QUIZ_ATTEMPT_FRACTION = Number(__ENV.QUIZ_ATTEMPT_FRACTION || 0.3);

const badgeVerifyFailures = new Counter('badge_verify_failures');
const enrollDuration = new Trend('enroll_duration', true);
const heartbeatDuration = new Trend('heartbeat_duration', true);
const quizStartDuration = new Trend('quiz_start_duration', true);
const quizSubmitDuration = new Trend('quiz_submit_duration', true);
// Rechazos esperados por regla de negocio (limite de intentos ya alcanzado),
// no cuentan como fallo de la prueba, solo se reportan para contexto.
const quizMaxAttemptsReached = new Counter('quiz_max_attempts_reached');
// Si esto alguna vez es > 0, el backend recalifico un intento ya enviado:
// bug funcional critico de integridad de calificacion bajo concurrencia.
const quizDoubleGradingDetected = new Counter('quiz_double_grading_detected');

// Perfil de rampa completo (~23 min): el que sube hasta MAX_VUS en varias
// etapas y sostiene la carga, pensado para UNA corrida larga que demuestra
// la meta de 2.000 usuarios concurrentes de la seccion 1 del enunciado.
//
// Perfil de nivel discreto (LEVEL_RUN=true, ~4-5 min): sube rapido a
// MAX_VUS y sostiene lo suficiente para una medicion estable, pensado para
// ejecutarse varias veces con distintos MAX_VUS (linea base + 3 niveles
// crecientes + repeticion cerca del limite, run_escenario1_niveles.ps1) sin
// que cada corrida tome 23 minutos.
const LEVEL_RUN = (__ENV.LEVEL_RUN || 'false').toLowerCase() === 'true';

const STAGES = LEVEL_RUN
  ? [
      { duration: '30s', target: Math.round(MAX_VUS * 0.5) }, // rampa rapida
      { duration: '30s', target: MAX_VUS },
      { duration: '3m', target: MAX_VUS }, // sostenido: aqui se toma la medicion del nivel
      { duration: '30s', target: 0 }, // enfriamiento corto
    ]
  : [
      { duration: '2m', target: Math.round(MAX_VUS * 0.1) }, // calentamiento
      { duration: '3m', target: Math.round(MAX_VUS * 0.4) },
      { duration: '5m', target: MAX_VUS }, // llega a los 2.000 usuarios concurrentes del enunciado
      { duration: '10m', target: MAX_VUS }, // sostiene la carga
      { duration: '3m', target: 0 }, // enfriamiento
    ];

export const options = {
  insecureSkipTLSVerify: true,
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    trafico_mixto: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: STAGES,
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    // Condiciones de aceptacion (seccion 9/10 del enunciado): sin
    // incumplimientos criticos de latencia p95 ni de tasa de error.
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500'],
    'http_req_duration{endpoint:catalog}': ['p(95)<300'],
    'http_req_duration{endpoint:enroll}': ['p(95)<500'],
    'http_req_duration{endpoint:heartbeat}': ['p(95)<400'],
    'http_req_duration{endpoint:badge}': ['p(95)<300'],
    'http_req_duration{endpoint:quiz_start}': ['p(95)<500'],
    'http_req_duration{endpoint:quiz_submit}': ['p(95)<600'],
    // Condicion funcional (no solo de latencia): ningun envio duplicado de
    // un intento de quiz debe volver a calificarse.
    quiz_double_grading_detected: ['count==0'],
  },
  setupTimeout: '10m',
};

export function setup() {
  const tokens = [];

  for (let i = 1; i <= POOL_SIZE; i++) {
    const email = `loadtest${String(i).padStart(4, '0')}@mooc.test`;
    const res = http.post(
      `${BASE_URL}/api/v1/auth/login`,
      JSON.stringify({ email, password: PASSWORD }),
      { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'setup_login' } }
    );

    if (res.status === 200) {
      const body = res.json();
      const token = body && body.data && body.data.session && body.data.session.token;
      if (token) tokens.push(token);
    } else if (res.status === 429) {
      // nos pasamos del rate limit: esperamos el resto de la ventana y reintentamos una vez
      sleep(5);
      i--;
      continue;
    }

    sleep(LOGIN_PACING_SECONDS);
  }

  if (tokens.length === 0) {
    fail(
      'No se pudo autenticar ningun usuario del pool. Corre primero ' +
      'loadtests/seed/seed_load_test_data.sql contra la base de datos objetivo.'
    );
  }

  // Se arma una vez el mapa pregunta -> opcion a responder (la primera
  // opcion listada de cada pregunta) leyendo el snapshot publico del quiz,
  // en vez de hardcodear IDs de opciones en el script.
  const quizAnswers = {};
  const snapshotRes = http.get(`${BASE_URL}/api/v1/learning/quizzes/${QUIZ_ID}/snapshot`, {
    tags: { endpoint: 'quiz_snapshot_setup' },
  });
  if (snapshotRes.status === 200) {
    const snapshot = snapshotRes.json();
    const questions = (snapshot && snapshot.data && snapshot.data.questions) || [];
    for (const question of questions) {
      if (question.options && question.options.length > 0) {
        quizAnswers[question.id] = question.options[0].id;
      }
    }
  } else {
    console.log(
      `Advertencia: no se pudo leer el snapshot del quiz ${QUIZ_ID} (status ${snapshotRes.status}). ` +
      'El flujo de quiz de la prueba se omitira. Corre el seed actualizado si aun no lo has hecho.'
    );
  }

  console.log(`Setup listo: ${tokens.length}/${POOL_SIZE} usuarios autenticados.`);
  return { tokens, quizAnswers };
}

export default function (data) {
  const token = data.tokens[Math.floor(Math.random() * data.tokens.length)];
  const authHeaders = {
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
  };

  const roll = Math.random();

  if (roll < 0.6) {
    // 60%: navegacion publica del catalogo (sin auth) — el grueso del trafico esperado
    const list = http.get(`${BASE_URL}/api/v1/courses`, { tags: { endpoint: 'catalog' } });
    check(list, { 'catalogo responde 200': (r) => r.status === 200 });

    const detail = http.get(`${BASE_URL}/api/v1/courses/${COURSE_ID}`, {
      tags: { endpoint: 'catalog' },
    });
    check(detail, { 'detalle de curso responde 200': (r) => r.status === 200 });
  } else if (roll < 0.85) {
    // 25%: inscripcion + progreso (heartbeat) + quiz — flujo autenticado de aprendizaje
    const enroll = http.post(
      `${BASE_URL}/api/v1/learning/enrollments`,
      JSON.stringify({ course_id: COURSE_ID }),
      { ...authHeaders, tags: { endpoint: 'enroll' } }
    );
    enrollDuration.add(enroll.timings.duration);
    check(enroll, {
      // 200/201 = inscrito ahora, 409 = ya estaba inscrito (reintentos del mismo token del pool)
      'inscripcion responde 200/201/409': (r) => [200, 201, 409].includes(r.status),
    });

    const heartbeat = http.post(
      `${BASE_URL}/api/v1/learning/heartbeat`,
      JSON.stringify({
        course_id: COURSE_ID,
        resource_stable_id: RESOURCE_STABLE_ID,
        dwell_time_seconds: 30,
        last_position_seconds: 30,
      }),
      { ...authHeaders, tags: { endpoint: 'heartbeat' } }
    );
    heartbeatDuration.add(heartbeat.timings.duration);
    check(heartbeat, { 'heartbeat responde 200': (r) => r.status === 200 });

    const hasAnswers = data.quizAnswers && Object.keys(data.quizAnswers).length > 0;
    if (hasAnswers && Math.random() < QUIZ_ATTEMPT_FRACTION) {
      // 409 aqui es un rechazo esperado por regla de negocio (limite de
      // intentos ya alcanzado para ese token), no un error de la prueba.
      const startRes = http.post(
        `${BASE_URL}/api/v1/learning/quizzes/${QUIZ_ID}/start-attempt`,
        null,
        {
          ...authHeaders,
          tags: { endpoint: 'quiz_start' },
          responseCallback: http.expectedStatuses(201, 409),
        }
      );
      quizStartDuration.add(startRes.timings.duration);
      check(startRes, {
        'inicio de intento de quiz responde 201/409': (r) => [201, 409].includes(r.status),
      });

      if (startRes.status === 409) {
        quizMaxAttemptsReached.add(1);
      } else if (startRes.status === 201) {
        const startBody = startRes.json();
        const attempt = startBody && startBody.data;

        if (attempt && attempt.status === 'in_progress' && attempt.id) {
          const submitRes = http.post(
            `${BASE_URL}/api/v1/learning/attempts/${attempt.id}/submit`,
            JSON.stringify({ answers: data.quizAnswers }),
            { ...authHeaders, tags: { endpoint: 'quiz_submit' } }
          );
          quizSubmitDuration.add(submitRes.timings.duration);
          check(submitRes, {
            'envio de intento de quiz responde 200': (r) => r.status === 200,
            'el intento queda calificado (score numerico)': (r) => {
              const body = r.json();
              return !!body && body.data && typeof body.data.score === 'number';
            },
          });

          if (submitRes.status === 200) {
            // Comprobacion de envio duplicado exigida por el enunciado: reenviar
            // el mismo intento ya enviado NO debe recalificarlo. El backend
            // debe rechazarlo con 409 (ErrAttemptAlreadySubmitted), nunca
            // devolver 200 con un score nuevo.
            const duplicateRes = http.post(
              `${BASE_URL}/api/v1/learning/attempts/${attempt.id}/submit`,
              JSON.stringify({ answers: data.quizAnswers }),
              {
                ...authHeaders,
                tags: { endpoint: 'quiz_duplicate_submit' },
                responseCallback: http.expectedStatuses(409),
              }
            );
            const duplicateRejected = check(duplicateRes, {
              'envio duplicado de quiz es rechazado (409) sin doble calificacion': (r) => r.status === 409,
            });
            if (!duplicateRejected) {
              quizDoubleGradingDetected.add(1);
            }
          }
        }
      }
    }
  } else {
    // 15%: verificacion publica de insignia (sin auth)
    const badge = http.get(`${BASE_URL}/api/v1/badges/verify/${BADGE_CODE}`, {
      tags: { endpoint: 'badge' },
    });
    if (badge.status !== 200) badgeVerifyFailures.add(1);
    check(badge, { 'verificacion de insignia responde 200': (r) => r.status === 200 });
  }

  // "think time": un usuario real no dispara requests espalda con espalda
  sleep(Math.random() * 2 + 1);
}
