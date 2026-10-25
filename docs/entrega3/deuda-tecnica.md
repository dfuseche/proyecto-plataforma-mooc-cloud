# Deuda técnica: diagnóstico y línea base (Entrega 3)

> Borrador. Las secciones marcadas **PENDIENTE** se completan con el análisis de SonarQube del
> commit final de la entrega.

## 1. Alcance y método

- **Componentes revisados:** API y workers (Go, `backend/`), frontend (`frontend/`), migraciones SQL,
  CI/CD (`.github/workflows/`), infraestructura en GCP (MIGs, balanceador, Cloud SQL, Redis,
  Cloud Storage) y documentación.
- **Herramientas:** SonarQube Cloud (análisis en el CI, ver `sonar-project.properties`),
  `golangci-lint` y `gosec` (ya presentes en el CI), `gofmt`, `axe-core` y revisión manual del código.
- **Alcance del análisis:** `backend` y `frontend/src`, incluidas las pruebas. Solo se excluyen
  `node_modules`, `dist` y el reporte de cobertura; no se excluye código de aplicación.

## 2. Medición inicial

| Indicador | Valor | Umbral | Interpretación |
|---|---|---|---|
| Duplicación de código | **1,1 %** | ≤ 20 % | Cumple con amplio margen. |
| Mantenibilidad (rating) | **A** (valor 1,0 en la escala 1 = A … 5 = E) | A, B o C | Cumple. |
| Deuda técnica estimada | **60 min** (1 h) | Registrar | Tiempo estimado por SonarQube para resolver los *code smells*; no incluye deuda arquitectónica ni operativa. |

Hallazgos del código total (rama `main`), antes y después de las correcciones de la sección 3:

| Indicador | Primer análisis (`99f5b38`) | Tras las correcciones (`37605ca`) |
|---|---|---|
| Líneas de código | 7.067 | 7.077 |
| Bugs | 7 | **0** |
| Vulnerabilidades | 9 | **0** |
| *Security hotspots* | 0 | 0 |
| *Code smells* | 3 | 3 (60 min de deuda) |
| Duplicación | 1,1 % | 1,1 % |
| Mantenibilidad | A | A |

- **Fecha de los análisis:** 25 de octubre de 2026.
- **Commit de la medición final:** `37605ca41840427afef23c8922acd31140137a2d`.
- **Herramienta:** SonarQube Cloud, análisis ejecutado en el CI (paso *SonarQube scan*).
- **Perfil de reglas:** **PENDIENTE** de confirmar (se espera "Sonar way").
- **Reporte:** <https://sonarcloud.io/summary/overall?id=dfuseche_proyecto-plataforma-mooc-cloud&branch=main>
- Los tres *code smells* abiertos son la regla `typescript:S8786` (expresiones regulares con retroceso
  super-lineal) en `frontend/src/components/Markdown.tsx`; ver C-13.
- La puerta de calidad por defecto de SonarQube aparece como fallida por la cobertura del código nuevo
  (23,5 % frente a 80 %); la cobertura no es un criterio de la Entrega 3, que evalúa duplicación,
  mantenibilidad y deuda sobre el código total.

## 3. Correcciones realizadas durante este ejercicio

| Id | Descripción | Evidencia |
|---|---|---|
| C-01 | Las rutas de profesor y administrador no exigían rol (`RequireRole` no se aplicaba). Ahora `AccessPolicy` lo exige. | Subtest E2E `05b`; `internal/middleware/roles.go` |
| C-02 | Las cabeceras `X-Admin-ID`/`X-Teacher-ID` sustituían la autenticación y `CreateCourse` usaba un profesor fijo. Eliminado. | `internal/middleware/auth.go`, `internal/course/handler.go` |
| C-03 | No había control de propiedad: cualquier profesor podía modificar cursos ajenos. | `domain.AuthorizeCourseEdit`, `ownership_test.go`, subtest `05b` |
| C-04 | El heartbeat y la inscripción aceptaban `student_id` en el cuerpo. Ahora el estudiante sale de la sesión. | `internal/learning/handler.go` |
| C-05 | `presigned-upload/download` aceptaban claves arbitrarias del bucket para cualquier profesor. Ahora solo administración. | `internal/middleware/roles.go` |
| C-06 | El administrador sembrado tenía la contraseña fija `Admin123!` (y la escribía en el log), y crear un docente sin contraseña usaba `TeacherPass123!`. Ahora la contraseña sale de `ADMIN_PASSWORD` (sin ella no se crea administrador), no se registra en el log y la API exige contraseña de al menos 8 caracteres para docentes. | `internal/user/usecase.go`, `docker-compose.yml`, `deploy/web-server.env.example` |
| C-07 | SonarQube, 7 *bugs*: seis botones de formulario sin atributo `type` (`Auth.tsx` ×2, `Teachers.tsx`, `Dashboard.tsx`, `CourseEditor.tsx`, `ResourceForm.tsx`, regla `typescript:S9011`) y una promesa sin tratar en `MediaPlayer.tsx` (`typescript:S9383`). Se declaró `type="submit"` y `void start()`. | Análisis SonarQube del commit `99f5b38`; `frontend/src/` |
| C-08 | SonarQube, imagen Docker: `npm ci` ejecutaba scripts de ciclo de vida (`docker:S6505`) y el contenedor corría como `root` (`docker:S6471`). Ahora se usa `--ignore-scripts` y un usuario sin privilegios (`app`, uid 10001). | `backend/Dockerfile` |
| C-09 | SonarQube, `go:S4036` ×3: ffmpeg y ffprobe se resolvían por la variable `PATH`. Ahora se invocan por ruta absoluta (`/usr/bin/ffmpeg`, `/usr/bin/ffprobe`). | `internal/worker/processor.go` |
| C-10 | SonarQube `go:S5332` y deuda D-06: CORS aceptaba cualquier origen `http(s)://*` con credenciales. La SPA comparte origen con la API, así que CORS queda desactivado salvo que se definan orígenes en `CORS_ALLOWED_ORIGINS`. | `cmd/api/main.go` |
| C-11 | SonarQube `go:S2077` ×2 (SQL formateado en `ListUsers`): revisado, solo se concatenan condiciones fijas con marcadores `$n`; los valores viajan como argumentos. Se documentó con `NOSONAR` y justificación. | `internal/user/repository.go` |
| C-12 | gosec (14 hallazgos, G114, G204, G304, G703, G705, G706): servidor HTTP con timeouts, lectura de archivos sin entrada de usuario en la SPA y excepciones `#nosec` justificadas donde el dato no es controlable por el usuario. | CI, artefacto `gosec-sarif-report`; `cmd/api/main.go`, `internal/web/spa.go` |
| C-13 | SonarQube `typescript:S8786` ×3: las expresiones regulares del renderizador de Markdown tenían retroceso super-lineal con texto sin cerrar. Se acotaron los cuantificadores, se eliminó una regex de enlaces y se quitó la ambigüedad en la de títulos. Pendiente de confirmar en el siguiente análisis. | `frontend/src/components/Markdown.tsx` |

(El commit de cada una se registra al cerrar la entrega.)

## 4. Inventario de deuda pendiente

| Id | Categoría | Descripción y ubicación | Evidencia | Impacto | Prioridad |
|---|---|---|---|---|---|
| D-02 | Seguridad | El registro devuelve el código de verificación en la respuesta (`internal/user/handler.go`); no hay servicio de correo, así que la verificación de email no prueba posesión del correo. **Deuda aceptada para la Entrega 3:** quitar el código dejaría a los estudiantes sin forma de verificar su cuenta, y el frontend lo usa para completar el registro. Se corrige al integrar un servicio de correo (en local ya existe Mailpit). | Código fuente | Cuentas con correos ajenos | Alta (aceptada) |
| D-03 | Funcional | La imagen de las insignias apunta a `<baseURL>/media/badges/<código>.png`, que nunca se genera ni se sube, y `baseURL` es `http://localhost:<puerto>` (`internal/learning/usecase.go`, `cmd/api/main.go`). | Código fuente | URLs de insignia rotas e internas | Media |
| D-04 | Progreso | Un recurso se marca completado si el cliente reporta ≥10 s de permanencia (`RecordHeartbeat`); no se valida contra la duración real del recurso. | Código fuente | El progreso se puede inflar con llamadas manuales | Media |
| D-05 | API | `backend/docs/openapi.yaml` (versión 3.1.0) documenta 21 paths, mientras el código registra 55 operaciones HTTP; faltan, entre otras, quizzes, recursos, módulos y unidades. | `grep '^  /'` del YAML frente a `grep 'r.Get(\|r.Post('` del código | Contrato incompleto para clientes y pruebas | Media |
| D-07 | Seguridad | El límite de tasa solo cubre las rutas de usuario, no el resto de la API. | `cmd/api/main.go` | Abuso de endpoints costosos | Media |
| D-08 | Código | 10 archivos Go sin formato `gofmt` (`gofmt -l`: `cmd/api/main.go`, `internal/course/handler.go`, `usecase.go`, `internal/domain/course.go`, entre otros). | `gofmt -l` | Ruido en diffs y revisiones | Baja |
| D-09 | Rendimiento | El catálogo publicado se filtra en memoria (`?published=true` lee hasta 1000 cursos y filtra) en lugar de en SQL. | `internal/course/handler.go` | No escala con muchos cursos | Media |
| D-10 | Pruebas | El frontend no tiene pruebas automatizadas; solo `tsc` y `build` en el CI. | `frontend/package.json` | Regresiones de interfaz no detectadas | Media |
| D-11 | Infraestructura | Certificado TLS autofirmado en el balanceador (no hay dominio). | Configuración del balanceador | Advertencias en el navegador; sin validación de identidad | Media |
| D-12 | Infraestructura | Redis (cola y sesiones) corre en una VM única y Cloud SQL sin alta disponibilidad: puntos únicos de falla. | Arquitectura desplegada | Caída de la cola o la base detiene el sistema | Media |
| D-13 | Operación | `docs/CD.md` aún describe el despliegue anterior por SSH a las VM. | Archivo | Documentación que contradice la operación real | Media |
| D-14 | Datos | Datos de prueba de k6 sin limpiar en la base (cursos "Carga HLS k6…", "Pool HLS k6 setup…") y objetos huérfanos en el bucket. | Consultas SQL de la Entrega 2 | Datos sintéticos mezclados con los demostrativos | Baja |

## 5. Priorización y plan de tratamiento

**PENDIENTE:** completar con los hallazgos de SonarQube (bugs, vulnerabilidades, *security
hotspots* y *code smells* más relevantes), asignar responsable y entrega objetivo a cada elemento
y ordenar por impacto, urgencia y esfuerzo. Orden propuesto con lo conocido hoy:
D-04 y D-03 (integridad del progreso e insignias) → D-05, D-07
→ D-09, D-10, D-12 → resto. D-02 queda aceptada hasta contar con un servicio de correo.
