# Frontend: flujos mínimos, rutas y evidencia

SPA en React + Vite + TypeScript (`frontend/`). La API de Go la sirve desde `/app/web`
(el `backend/Dockerfile` la compila en su primera etapa), por lo que comparte origen con
`/api/v1`: no hay CORS que configurar y la sesión viaja como `Authorization: Bearer`.
La versión desplegada (commit de la imagen) aparece en el pie de página y se inyecta con
`--build-arg APP_VERSION=<sha>` en el job de despliegue.

## Matriz de flujos

| Rol | Flujo | Ruta de interfaz | Endpoints que usa | Evidencia |
|---|---|---|---|---|
| Estudiante | Iniciar y cerrar sesión | `/entrar`, botón "Salir" | `POST /auth/login`, `POST /auth/logout` | (A) |
| Estudiante | Catálogo y detalle de un curso publicado | `/`, `/cursos/:id` | `GET /courses?published=true`, `GET /courses/{id}` | (A) |
| Estudiante | Inscribirse | `/cursos/:id` | `POST /learning/enrollments` | (A) |
| Estudiante | Sus cursos y progreso | `/mis-cursos` | `GET /learning/enrollments` | (A) |
| Estudiante | Navegar módulos, unidades y recursos | `/aprender/:cursoId/:recursoId` | `GET /courses/{id}` | (A) |
| Estudiante | Video/audio HLS, lecturas, PDF, imágenes, enlaces | ídem | `GET /media/resources/{id}/stream-url`, `.../manifest.m3u8`, `.../resume`, `POST /learning/heartbeat` | (A) |
| Estudiante | Presentar un quiz y ver su resultado | ídem (recurso tipo evaluación) | `GET /learning/resources/{id}/quiz`, `POST .../start-attempt`, `PATCH /learning/attempts/{id}`, `POST .../submit` | (A) |
| Profesor | Iniciar y cerrar sesión | `/entrar` | ídem | (A) |
| Administrador | Crear la cuenta de un docente | `/admin/docentes` | `POST /admin/teachers` | (A) |
| Profesor | Consultar sus cursos | `/docente` | `GET /courses` (filtrado por propietario) | (A) |
| Profesor | Crear curso y estructura (módulos, unidades) | `/docente`, `/docente/cursos/:id` | `POST /courses`, `.../modules`, `.../units` | (A) |
| Profesor | Agregar recursos, subir archivos y ver su procesamiento | `/docente/cursos/:id` | `POST .../resources`, `PUT <URL prefirmada>`, `POST /media/resources/{id}/complete-upload` | (A) |
| Profesor | Eliminar un recurso del borrador (con confirmación) | `/docente/cursos/:id` | `DELETE /courses/resources/{id}` | (A) |
| Profesor | Crear un quiz básico | `/docente/cursos/:id` | `POST /courses/resources/{id}/quiz` | (A) |
| Profesor | Publicar y ver el resultado | `/docente/cursos/:id` | `POST /courses/versions/{id}/publish` (muestra la lista de errores si falla) | (A) |
| Profesor | Editar un curso ya publicado | `/docente/cursos/:id` | `POST /courses/{id}/create-draft` | (A) |

Estados de interfaz cubiertos: carga, vacío, error con reintento, éxito, y "procesando" para
videos y audios (un recurso en procesamiento nunca se reproduce ni se muestra como disponible).

## Reglas de autorización verificables

| Regla | Dónde se hace cumplir | Evidencia |
|---|---|---|
| Un estudiante no ejecuta acciones de profesor | `AccessPolicy` (`internal/middleware/roles.go`) | Subtest E2E `05b_roles_y_propiedad_de_cursos` |
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

## Pendiente de evidenciar en el entorno cloud

(A) = "recorrido conectado en cloud": falta ejecutarlo sobre el balanceador y registrar capturas o
grabación. Guion de la demostración:

1. Profesor: entrar → crear curso → módulo → unidad → recurso de lectura, video (esperar a
   "Listo") y evaluación con sus preguntas → publicar.
2. Intento de publicar sin recursos visibles: se muestra la lista exhaustiva de errores.
3. Estudiante: entrar → encontrar el curso en el catálogo → inscribirse → ver el video con
   avance → presentar el quiz y ver el resultado.
4. Comprobar que un estudiante no ve el panel docente y que un segundo profesor recibe 403 sobre
   el curso del primero.
