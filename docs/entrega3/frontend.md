# Frontend: flujos mínimos, rutas y evidencia

SPA en React + Vite + TypeScript (`frontend/`). La API de Go la sirve desde `/app/web`
(el `backend/Dockerfile` la compila en su primera etapa), por lo que comparte origen con
`/api/v1`: no hay CORS que configurar y la sesión viaja como `Authorization: Bearer`.
La versión desplegada (commit de la imagen) aparece en el pie de página y se inyecta con
`--build-arg APP_VERSION=<sha>` en el job de despliegue.

## Matriz de flujos

| Rol | Flujo | Ruta de interfaz | Endpoints que usa | Evidencia |
|---|---|---|---|---|
| Estudiante | Iniciar y cerrar sesión | `/entrar`, botón "Salir" | `POST /auth/login`, `POST /auth/logout` | [captura](evidencias/LOGIN.jpg) |
| Estudiante | Catálogo y detalle de un curso publicado | `/`, `/cursos/:id` | `GET /courses?published=true`, `GET /courses/{id}` | [captura](evidencias/ESTUDIANTE%20-%20lista%20de%20cursos%20disponibles.jpg), [captura](evidencias/ESTUDIANTE%20-%20pagina%20de%20curso.jpg) |
| Estudiante | Inscribirse | `/cursos/:id` | `POST /learning/enrollments` | [captura](evidencias/ESTUDIANTE%20-%20pagina%20de%20curso.jpg), [captura](evidencias/ESTUDIANTE%20-%20mis%20cursos.jpg) |
| Estudiante | Sus cursos y progreso | `/mis-cursos` | `GET /learning/enrollments` | [captura](evidencias/ESTUDIANTE%20-%20mis%20cursos.jpg) |
| Estudiante | Navegar módulos, unidades y recursos | `/aprender/:cursoId/:recursoId` | `GET /courses/{id}` | [captura](evidencias/ESTUDIANTE%20-%20lectura%20de%20curso.jpg) |
| Estudiante | Video/audio HLS, lecturas, PDF, imágenes, enlaces | ídem | `GET /media/resources/{id}/stream-url`, `.../manifest.m3u8`, `.../resume`, `POST /learning/heartbeat` | [captura](evidencias/ESTUDIANTE%20-%20video%20de%20curso.jpg), [captura](evidencias/ESTUDIANTE%20-%20lectura%20de%20curso.jpg) |
| Estudiante | Presentar un quiz y ver su resultado | ídem (recurso tipo evaluación) | `GET /learning/resources/{id}/quiz`, `POST .../start-attempt`, `PATCH /learning/attempts/{id}`, `POST .../submit` | [captura](evidencias/ESTUDIANTE%20-%20curso%20aprobado.jpg) |
| Profesor | Iniciar y cerrar sesión | `/entrar` | ídem | [captura](evidencias/LOGIN.jpg) |
| Administrador | Crear la cuenta de un docente | `/admin/docentes` | `POST /admin/teachers` | [captura](evidencias/ADMIN%20-%20crear%20docente.jpg) |
| Profesor | Consultar sus cursos | `/docente` | `GET /courses` (filtrado por propietario) | [captura](evidencias/DOCENTE%20-%20crear%20curso.jpg) |
| Profesor | Crear curso y estructura (módulos, unidades) | `/docente`, `/docente/cursos/:id` | `POST /courses`, `.../modules`, `.../units` | [captura](evidencias/DOCENTE%20-%20crear%20curso.jpg), [captura](evidencias/DOCENTE%20-%20lista%20de%20recursos%20de%20unidad.jpg) |
| Profesor | Agregar recursos, subir archivos y ver su procesamiento | `/docente/cursos/:id` | `POST .../resources`, `PUT <URL prefirmada>`, `POST /media/resources/{id}/complete-upload` | [captura](evidencias/DOCENTE%20-%20crear%20recurso%20video.jpg), [captura](evidencias/DOCENTE%20-%20subiendo%20recurso%20video.jpg), [captura](evidencias/DOCENTE%20-%20procesando%20video.jpg), [captura](evidencias/DOCENTE%20-%20lista%20de%20recursos%20de%20unidad.jpg) |
| Profesor | Eliminar un recurso del borrador (con confirmación) | `/docente/cursos/:id` | `DELETE /courses/resources/{id}` | Pendiente: sin captura |
| Profesor | Crear un quiz básico | `/docente/cursos/:id` | `POST /courses/resources/{id}/quiz` | Pendiente: sin captura |
| Profesor | Publicar y ver el resultado | `/docente/cursos/:id` | `POST /courses/versions/{id}/publish` (muestra la lista de errores si falla) | Pendiente: sin captura |
| Profesor | Editar un curso ya publicado | `/docente/cursos/:id` | `POST /courses/{id}/create-draft` | Pendiente: sin captura |

Estados de interfaz cubiertos: carga, vacío, error con reintento, éxito, y "procesando" para
videos y audios (un recurso en procesamiento nunca se reproduce ni se muestra como disponible).

## Reglas de autorización verificables

| Regla | Dónde se hace cumplir | Evidencia |
|---|---|---|
| Un estudiante no ejecuta acciones de profesor | `AccessPolicy` (`internal/middleware/roles.go`) | Subtest E2E `05b_roles_y_propiedad_de_cursos`; [captura](evidencias/ESTUDIANTE%20-%20incapaz%20de%20acceder%20pagina%20de%20docencia.jpg) |
| Un profesor no modifica cursos ajenos | `domain.AuthorizeCourseEdit` en cada handler de autoría, quizzes y `complete-upload` | `internal/domain/ownership_test.go` y subtest E2E `05b` |
| Sin sesión se rechaza (401) | `AccessPolicy` | Subtest E2E `05b` |

## Verificación realizada

- **Accesibilidad automática (axe-core 4.10, reglas WCAG 2.0/2.1/2.2 A y AA):** 0 violaciones en
  las 9 vistas (catálogo, detalle, entrar, registro, mis cursos, visor de lectura, visor de
  evaluación, panel docente y editor de curso). Se ejecutó sobre el build de producción con una
  API simulada, así que acredita la interfaz y no el recorrido conectado. Las reglas de contraste
  que axe marca para revisión manual se calcularon a mano: todos los textos superan 4.5:1 y los
  bordes de los campos, 3:1.
- **Compilación:** `tsc --noEmit` y `vite build` en el job `frontend` del CI.

## Evidencia del recorrido en cloud

Recorrido ejecutado sobre el balanceador con el commit `c453d1f7e0a6ab300423e22bb2b4b6f2cfbdce29`
(el hash también aparece en el pie de página de la aplicación). Las capturas están en
`docs/entrega3/evidencias/`. Las filas marcadas «Pendiente: sin captura» se probaron en local y
en el E2E automatizado, pero no se capturaron en cloud: eliminar un recurso con confirmación,
crear un quiz, publicar (incluida la lista de errores) y editar un curso publicado.
