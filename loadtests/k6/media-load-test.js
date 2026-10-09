// Prueba de carga del Escenario 2 (carga, procesamiento y consumo
// multimedia) de la Plataforma MOOC.
//
// A diferencia de load-test.js (Escenario 1: catalogo/inscripcion/quiz),
// el patron de trafico aca es muy distinto y se modela con DOS escenarios
// de k6 corriendo en paralelo, en la misma proporcion que tendria una
// plataforma real:
//
//   - subida_multimedia: pocos VUs (subir + esperar la transcodificacion
//     HLS es trabajo pesado y secuencial: PUT del archivo + ffmpeg en el
//     worker). Simula profesores subiendo contenido.
//   - consumo_hls: muchos VUs (pedir stream-url, bajar el manifiesto
//     firmado y sus segmentos). Simula estudiantes reproduciendo video ya
//     procesado — es el trafico de lectura real a escala, analogo a lo
//     que en produccion serviria una CDN.
//
// Que cubre (alineado con el punto 6/7 de la seccion 2 del enunciado:
// "Procesamiento asincrono de video y audio a HLS, conservacion del
// original, idempotencia... reproduccion adaptativa... desde la ultima
// posicion reportada"):
//   - Carga directa a almacenamiento via presigned URL + complete-upload.
//   - Tiempo real de la transcodificacion HLS asincrona (metrica de
//     negocio, no solo de latencia HTTP).
//   - Que un fallo al encolar la transcodificacion ya NO quede en
//     silencio: complete-upload debe responder 200 o 502, nunca un 200
//     enganoso con el recurso atascado (bug real que encontramos y
//     arreglamos durante el smoke test manual de este mismo escenario).
//   - Consumo del manifiesto HLS firmado y descarga real de sus
//     segmentos (el bug original de este escenario: el manifiesto
//     firmaba solo el .m3u8 y los .ts devolvian 403).
//   - El endpoint de "resume" (ultima posicion reportada).
//
// Que NO cubre todavia: el enunciado pide carga "multipart directa y
// reanudable"; la API actual (GeneratePresignedUpload) solo ofrece un PUT
// firmado de un solo tiro, sin soporte de multipart/resumable upload. Este
// script prueba lo que existe hoy, no lo simula.
//
// Requiere:
//   - loadtests/seed/seed_load_test_data.sql ya corrido (usa la unidad
//     sembrada como destino de los recursos de video de prueba).
//   - Los 3 perfiles de video de loadtests/assets/ (incluidos en el repo,
//     ver generate_assets.sh): ligero 640x360/10s/~1MB, medio 1280x720/20s/
//     ~6.6MB y pesado 1920x1080/20s/~12.9MB. Cada subida usa uno de los tres
//     (rotando), asi el worker transcodifica archivos con costo realmente
//     distinto y genera escaleras HLS de 1, 3 y 4 renditions respectivamente
//     (sin upscaling: solo escalones <= la altura del original).
//
// Uso basico (correr desde la raiz del repo, FUERA de las VMs, igual que
// load-test.js):
//   k6 run -e BASE_URL=http://35.209.105.199 loadtests/k6/media-load-test.js
//
// Humo rapido antes del run completo:
//   k6 run -e BASE_URL=http://35.209.105.199 -e UPLOAD_VUS=1 -e PLAYBACK_VUS=5 \
//     -e UPLOAD_DURATION=40s -e PLAYBACK_DURATION=40s loadtests/k6/media-load-test.js
//
// Variables de entorno soportadas (todas opcionales salvo BASE_URL):
//   BASE_URL                     URL publica de la API (obligatorio, sin / al final)
//   UNIT_ID                      unidad donde crear los recursos de video (default: la sembrada)
//   UPLOAD_VUS                   VUs concurrentes subiendo video (default 5)
//   UPLOAD_DURATION               duracion del escenario de subida (default 3m)
//   PLAYBACK_VUS                 VUs concurrentes reproduciendo HLS (default 150)
//   PLAYBACK_DURATION             duracion del escenario de reproduccion (default 3m)
//   PLAYBACK_POOL_SIZE           cuantos videos se pre-transcodifican en el setup para el escenario de reproduccion (default 3: uno por perfil, rotando)
//   PRETRANSCODED_RESOURCE_IDS   CSV de resource_id ya transcodificados; si se define, el setup NO sube nada nuevo y reutiliza estos para consumo_hls
//   SETUP_MAX_WAIT_SECONDS       cuanto esperar, POR VIDEO, en el setup (default 1200). Si el worker aun tiene cola de un nivel anterior, el video del pool queda detras de ella: esperar aqui es esperar el drenado de la cola, y no cuenta como medicion
//   MAX_WAIT_FOR_MANIFEST_SECONDS cuanto esperar a que un video termine de transcodificarse antes de darlo por fallido (default 180: el perfil pesado con 4 renditions tarda mucho mas en 2 vCPU)
//   POLL_INTERVAL_SECONDS        cada cuanto reconsultar stream-url mientras se espera la transcodificacion (default 3)
//   MAX_SEGMENTS_PER_ITERACION    tope de segmentos .ts a descargar por reproduccion, para no bajar videos larguisimos completos en cada iteracion (default 5)

import http from 'k6/http';
import { check, sleep, fail } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL;
if (!BASE_URL) {
  fail('Define BASE_URL, ej: k6 run -e BASE_URL=http://35.209.105.199 loadtests/k6/media-load-test.js');
}

const UNIT_ID = __ENV.UNIT_ID || '10000000-0000-0000-0000-000000000004';

const UPLOAD_VUS = Number(__ENV.UPLOAD_VUS || 5);
const UPLOAD_DURATION = __ENV.UPLOAD_DURATION || '3m';
const PLAYBACK_VUS = Number(__ENV.PLAYBACK_VUS || 150);
const PLAYBACK_DURATION = __ENV.PLAYBACK_DURATION || '3m';
const PLAYBACK_POOL_SIZE = Number(__ENV.PLAYBACK_POOL_SIZE || 3);
const PRETRANSCODED_RESOURCE_IDS = (__ENV.PRETRANSCODED_RESOURCE_IDS || '')
  .split(',')
  .map((id) => id.trim())
  .filter((id) => id.length > 0);

const MAX_WAIT_FOR_MANIFEST_SECONDS = Number(__ENV.MAX_WAIT_FOR_MANIFEST_SECONDS || 180);
const SETUP_MAX_WAIT_SECONDS = Number(__ENV.SETUP_MAX_WAIT_SECONDS || 1200);
const POLL_INTERVAL_SECONDS = Number(__ENV.POLL_INTERVAL_SECONDS || 3);
const MAX_SEGMENTS_PER_ITERACION = Number(__ENV.MAX_SEGMENTS_PER_ITERACION || 5);

// Se leen una sola vez en el contexto de inicializacion de k6 (obligatorio:
// open() no puede llamarse dentro de una funcion de VU). open() exige
// literales de ruta, por eso no se arma en un bucle.
// expectedRenditions = escalones de la escalera HLS que el worker debe
// generar (1080/720/480/360 con altura <= la del original).
const PROFILES = [
  { name: 'ligero', bytes: open('../assets/perfil_ligero_360p_10s.mp4', 'b'), expectedRenditions: 1 },
  { name: 'medio', bytes: open('../assets/perfil_medio_720p_20s.mp4', 'b'), expectedRenditions: 3 },
  { name: 'pesado', bytes: open('../assets/perfil_pesado_1080p_20s.mp4', 'b'), expectedRenditions: 4 },
];

function profileByName(name) {
  return PROFILES.find((p) => p.name === name);
}

// --- Metricas propias -------------------------------------------------

const uploadPutDuration = new Trend('media_upload_put_duration', true);
// Tiempo real de negocio: desde que complete-upload confirma el encolado
// hasta que stream-url empieza a devolver el manifiesto HLS. No es un
// http_req_duration porque involucra varias requests de polling.
const processingDuration = new Trend('media_processing_duration', true);
// Un video que nunca termina de procesarse en el tiempo maximo permitido.
const processingTimeouts = new Counter('media_processing_timeouts');
// complete-upload respondiendo 502 (el encolado a asynq fallo de verdad).
// Antes este caso era completamente invisible: el handler descartaba el
// error de Enqueue en silencio y siempre respondia 200. Que esta metrica
// suba de vez en cuando bajo carga real es esperable (Redis bajo presion);
// que NUNCA suba es lo que habria que sospechar, no celebrar.
const enqueueRejected = new Counter('media_enqueue_rejected');
const manifestDuration = new Trend('media_manifest_duration', true);
const segmentDuration = new Trend('media_segment_duration', true);
const segmentBytes = new Counter('media_segment_bytes_downloaded');
// Si esto sube, el bug original del escenario (manifiesto firmado sin
// firmar los segmentos) volvio.
const segmentDownloadFailures = new Counter('media_segment_download_failures');
// La escalera multi-calidad no coincide con la esperada para el perfil
// (p. ej. un 1080p que solo trae 1 rendition: el bug que motivo este cambio).
const unexpectedLadder = new Counter('media_unexpected_ladder');
const variantPlaylistDuration = new Trend('media_variant_playlist_duration', true);

export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    subida_multimedia: {
      executor: 'constant-vus',
      exec: 'subirVideo',
      vus: UPLOAD_VUS,
      duration: UPLOAD_DURATION,
    },
    consumo_hls: {
      executor: 'constant-vus',
      exec: 'reproducirHLS',
      vus: PLAYBACK_VUS,
      duration: PLAYBACK_DURATION,
      // Le da tiempo al setup (y a los primeros videos del pool) antes de
      // que arranquen los VUs de reproduccion.
      startTime: '5s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.02'],
    'http_req_duration{endpoint:create_resource}': ['p(95)<800'],
    // Los perfiles pesan hasta ~13 MB (antes ~90 KB): la subida directa ya
    // no es casi instantanea.
    'http_req_duration{endpoint:upload_put}': ['p(95)<15000'],
    'http_req_duration{endpoint:complete_upload}': ['p(95)<800'],
    'http_req_duration{endpoint:stream_url}': ['p(95)<500'],
    'http_req_duration{endpoint:manifest}': ['p(95)<600'],
    'http_req_duration{endpoint:variant_playlist}': ['p(95)<600'],
    // Los segmentos pesan segun la calidad (6s a 0.8-5 Mbps = ~0.6-3.7 MB),
    // asi que un umbral unico no tiene sentido: se fija uno por rendition.
    // Incluyen el ancho de banda de la laptop generadora (no medido).
    'http_req_duration{endpoint:segment,rendition:360p}': ['p(95)<1000'],
    'http_req_duration{endpoint:segment,rendition:480p}': ['p(95)<1000'],
    'http_req_duration{endpoint:segment,rendition:720p}': ['p(95)<2000'],
    'http_req_duration{endpoint:segment,rendition:1080p}': ['p(95)<3000'],
    // Sin umbral real: mantiene el agregado en el summary-export.
    'http_req_duration{endpoint:segment}': ['max>=0'],
    'http_req_duration{endpoint:resume}': ['p(95)<400'],
    // El pipeline completo (ffmpeg incluido) tardando mas de ~45s con
    // carga concurrente en un worker de 2 vCPU es senal de saturacion.
    media_processing_duration: ['p(95)<45000'],
    media_processing_timeouts: ['count==0'],
    // Igual que en load-test.js: condicion funcional, no solo de latencia.
    media_segment_download_failures: ['count==0'],
    media_unexpected_ladder: ['count==0'],
    // Sin umbral real: declarar los sub-metricos por perfil hace que k6 los
    // incluya en --summary-export, para separar el costo de cada perfil.
    'media_processing_duration{profile:ligero}': ['max>=0'],
    'media_processing_duration{profile:medio}': ['max>=0'],
    'media_processing_duration{profile:pesado}': ['max>=0'],
    'http_req_duration{endpoint:upload_put,profile:ligero}': ['max>=0'],
    'http_req_duration{endpoint:upload_put,profile:medio}': ['max>=0'],
    'http_req_duration{endpoint:upload_put,profile:pesado}': ['max>=0'],
  },
  // 3 videos del pool, hasta SETUP_MAX_WAIT_SECONDS cada uno (1200s por
  // defecto) mas el drenado de cola del nivel anterior.
  setupTimeout: '70m',
};

function crearYSubirVideo(tagPrefix, profile) {
  const createRes = http.post(
    `${BASE_URL}/api/v1/courses/units/${UNIT_ID}/resources`,
    JSON.stringify({
      title: `${tagPrefix} ${profile.name} ${Date.now()}-${Math.floor(Math.random() * 1e6)}`,
      type: 'video',
      is_visible: true,
      is_mandatory: false,
      is_downloadable: false,
      position: 900 + Math.floor(Math.random() * 100),
    }),
    { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'create_resource' } }
  );
  const created = check(createRes, {
    'creacion de recurso responde 201': (r) => r.status === 201,
  });
  if (!created) return null;

  const body = createRes.json();
  const resourceId = body && body.data && body.data.id;
  const objectKey = body && body.data && body.data.object_key;
  const uploadUrl = body && body.data && body.data.presigned_upload_url;
  if (!resourceId || !objectKey || !uploadUrl) return null;

  const putRes = http.put(uploadUrl, profile.bytes, {
    headers: { 'Content-Type': 'video/mp4' },
    tags: { endpoint: 'upload_put', profile: profile.name },
  });
  uploadPutDuration.add(putRes.timings.duration, { profile: profile.name });
  const uploaded = check(putRes, {
    'subida directa a almacenamiento responde 2xx': (r) => r.status >= 200 && r.status < 300,
  });
  if (!uploaded) return null;

  const completeRes = http.post(
    `${BASE_URL}/api/v1/media/resources/${resourceId}/complete-upload`,
    JSON.stringify({ object_key: objectKey }),
    { headers: { 'Content-Type': 'application/json' }, tags: { endpoint: 'complete_upload' } }
  );
  check(completeRes, {
    // 502 es una respuesta HONESTA de que el encolado a asynq fallo (ver
    // enqueueTranscodeTask en el handler) — ya no un 200 enganoso.
    'complete-upload responde 200/502': (r) => [200, 502].includes(r.status),
  });
  if (completeRes.status === 502) {
    enqueueRejected.add(1);
    return null;
  }
  if (completeRes.status !== 200) return null;

  return resourceId;
}

// Espera (con polling) a que stream-url deje de apuntar al archivo crudo y
// pase a apuntar al manifiesto HLS firmado. Devuelve la URL absoluta del
// manifiesto, o null si se agoto el tiempo maximo de espera.
// enSetup: el video del pool se espera con mas margen y NO se registra en
// media_processing_duration / media_processing_timeouts: en el setup el
// worker puede estar drenando la cola del nivel anterior, y ese tiempo no es
// una medicion del nivel que se esta probando.
function esperarManifiesto(resourceId, profile, enSetup) {
  const start = Date.now();
  const maxWait = enSetup ? SETUP_MAX_WAIT_SECONDS : MAX_WAIT_FOR_MANIFEST_SECONDS;
  const deadline = start + maxWait * 1000;

  while (Date.now() < deadline) {
    const streamRes = http.get(`${BASE_URL}/api/v1/media/resources/${resourceId}/stream-url`, {
      tags: { endpoint: 'stream_url' },
    });
    if (streamRes.status === 200) {
      const url = streamRes.json('data.presigned_url');
      if (url && url.indexOf('manifest.m3u8') !== -1) {
        if (!enSetup) processingDuration.add(Date.now() - start, { profile: profile.name });
        return url;
      }
    }
    sleep(POLL_INTERVAL_SECONDS);
  }

  if (!enSetup) processingTimeouts.add(1);
  return null;
}

// Escenario "subida_multimedia": flujo completo de un profesor subiendo un
// video nuevo y esperando a que quede listo para reproducirse.
export function subirVideo() {
  // Rota los 3 perfiles entre VUs e iteraciones para que todos se ejerciten.
  const profile = PROFILES[(__VU + __ITER) % PROFILES.length];
  const resourceId = crearYSubirVideo('Carga HLS k6', profile);
  if (!resourceId) {
    sleep(1);
    return;
  }

  esperarManifiesto(resourceId, profile);

  // "think time": un profesor no encadena subidas espalda con espalda.
  sleep(Math.random() * 3 + 2);
}

// --- Setup: arma el pool de videos ya transcodificados que usa el
// escenario de reproduccion, para no depender de que subida_multimedia
// alcance a terminar alguno a tiempo. ---
export function setup() {
  if (PRETRANSCODED_RESOURCE_IDS.length > 0) {
    console.log(
      `Usando ${PRETRANSCODED_RESOURCE_IDS.length} resource_id ya transcodificados ` +
      '(PRETRANSCODED_RESOURCE_IDS), sin subir nada nuevo en el setup.'
    );
    return { pool: PRETRANSCODED_RESOURCE_IDS.map((id) => ({ id, profile: null })) };
  }

  console.log(`Preparando pool de ${PLAYBACK_POOL_SIZE} video(s) para el escenario de reproduccion...`);
  const pool = [];
  for (let i = 0; i < PLAYBACK_POOL_SIZE; i++) {
    // Un video de cada perfil, en orden, para que el pool cubra los 3.
    const profile = PROFILES[i % PROFILES.length];
    const resourceId = crearYSubirVideo('Pool HLS k6 setup', profile);
    if (!resourceId) {
      console.log(`Setup: no se pudo crear/subir el video ${i + 1}/${PLAYBACK_POOL_SIZE}, se omite.`);
      continue;
    }
    const manifestUrl = esperarManifiesto(resourceId, profile, true);
    if (manifestUrl) {
      pool.push({ id: resourceId, profile: profile.name });
      console.log(`Setup: video ${i + 1}/${PLAYBACK_POOL_SIZE} listo (perfil ${profile.name}, ${resourceId}).`);
    } else {
      console.log(`Setup: video ${i + 1}/${PLAYBACK_POOL_SIZE} no termino de procesar a tiempo, se omite.`);
    }
  }

  if (pool.length === 0) {
    fail(
      'No se pudo dejar listo ningun video para el escenario de reproduccion. ' +
      'Revisa que el worker este sano (Redis/DB alcanzables) antes de reintentar, ' +
      'o pasa PRETRANSCODED_RESOURCE_IDS con recursos ya procesados de una corrida anterior.'
    );
  }

  console.log(`Setup listo: ${pool.length}/${PLAYBACK_POOL_SIZE} video(s) en el pool de reproduccion.`);
  return { pool };
}

// Escenario "consumo_hls": un estudiante pide el stream, baja el
// manifiesto firmado y sus primeros segmentos, y consulta su ultima
// posicion reportada — el patron de trafico de lectura real a escala.
export function reproducirHLS(data) {
  if (!data.pool || data.pool.length === 0) {
    sleep(1);
    return;
  }

  const entry = data.pool[Math.floor(Math.random() * data.pool.length)];
  const resourceId = entry.id;

  const streamRes = http.get(`${BASE_URL}/api/v1/media/resources/${resourceId}/stream-url`, {
    tags: { endpoint: 'stream_url' },
  });
  const streamOk = check(streamRes, {
    'stream-url responde 200': (r) => r.status === 200,
  });
  if (!streamOk) {
    sleep(1);
    return;
  }

  const manifestUrl = streamRes.json('data.presigned_url');
  if (!manifestUrl || manifestUrl.indexOf('manifest.m3u8') === -1) {
    // El video del pool todavia no tiene manifiesto (pool armado con
    // PRETRANSCODED_RESOURCE_IDS invalidos, por ejemplo).
    sleep(1);
    return;
  }

  const manifestRes = http.get(manifestUrl, { tags: { endpoint: 'manifest' } });
  manifestDuration.add(manifestRes.timings.duration);
  const manifestOk = check(manifestRes, {
    'manifiesto HLS responde 200': (r) => r.status === 200,
    'manifiesto trae Content-Type de HLS': (r) => {
      const ct = r.headers['Content-Type'] || '';
      return ct.indexOf('mpegurl') !== -1;
    },
  });
  if (!manifestOk) {
    segmentDownloadFailures.add(1);
    sleep(1);
    return;
  }

  // Manifiesto maestro multi-calidad: lista de renditions. Si no trae
  // #EXT-X-STREAM-INF es una media playlist directa (videos transcodificados
  // antes de la escalera, o audio) y se consume como antes.
  const renditions = parseMasterRenditions(manifestRes.body);
  let mediaPlaylistBody = manifestRes.body;
  let renditionTag = 'unica';

  if (renditions.length > 0) {
    const expected = entry.profile ? profileByName(entry.profile).expectedRenditions : null;
    if (expected !== null && renditions.length !== expected) {
      unexpectedLadder.add(1);
    }

    // Un reproductor adaptativo real elige segun su ancho de banda; aca se
    // elige una al azar para repartir el trafico entre todas las calidades.
    const chosen = renditions[Math.floor(Math.random() * renditions.length)];
    renditionTag = chosen.name;

    const variantRes = http.get(chosen.url, { tags: { endpoint: 'variant_playlist', rendition: chosen.name } });
    variantPlaylistDuration.add(variantRes.timings.duration);
    const variantOk = check(variantRes, {
      'playlist de la rendition responde 200': (r) => r.status === 200,
    });
    if (!variantOk) {
      segmentDownloadFailures.add(1);
      sleep(1);
      return;
    }
    mediaPlaylistBody = variantRes.body;
  }

  // Extrae las URLs de segmento ya firmadas (lineas que no empiezan con #
  // dentro de la media playlist), tope MAX_SEGMENTS_PER_ITERACION para no
  // bajar un video entero por iteracion si el archivo es largo.
  const segmentUrls = mediaPlaylistBody
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0 && !line.startsWith('#'))
    .slice(0, MAX_SEGMENTS_PER_ITERACION);

  for (const segmentUrl of segmentUrls) {
    const segRes = http.get(segmentUrl, { tags: { endpoint: 'segment', rendition: renditionTag } });
    segmentDuration.add(segRes.timings.duration);
    const segOk = check(segRes, {
      // Este es EXACTAMENTE el bug original del escenario: sin el fix del
      // manifiesto firmado, esto daba 403 en vez de 200.
      'segmento HLS responde 200': (r) => r.status === 200,
    });
    if (segOk) {
      segmentBytes.add(segRes.body ? segRes.body.length : 0);
    } else {
      segmentDownloadFailures.add(1);
    }
  }

  // Simula reportar/consultar la ultima posicion reproducida.
  const resumeRes = http.get(`${BASE_URL}/api/v1/media/resources/${resourceId}/resume`, {
    tags: { endpoint: 'resume' },
  });
  check(resumeRes, { 'resume responde 200': (r) => r.status === 200 });

  // "think time": un estudiante ve un rato antes de pedir el siguiente tramo.
  sleep(Math.random() * 4 + 2);
}

// Lee un manifiesto maestro HLS y devuelve [{ name, url }] por cada
// #EXT-X-STREAM-INF, donde name es la altura del RESOLUTION ("720p") y url
// la linea siguiente (ya reescrita por la API para volver a pasar por ella).
function parseMasterRenditions(body) {
  const lines = body.split('\n').map((l) => l.trim());
  const out = [];
  for (let i = 0; i < lines.length; i++) {
    if (!lines[i].startsWith('#EXT-X-STREAM-INF')) continue;
    const m = lines[i].match(/RESOLUTION=\d+x(\d+)/);
    const url = lines.slice(i + 1).find((l) => l.length > 0 && !l.startsWith('#'));
    if (url) out.push({ name: m ? `${m[1]}p` : `rendition${out.length}`, url });
  }
  return out;
}
