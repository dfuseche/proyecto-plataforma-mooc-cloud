# Deuda técnica: diagnóstico y línea base (Entrega 3)

> Medición final de SonarQube registrada sobre el commit `fa3673c`. Queda por asignar responsables en la
> sección 5.

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
| Deuda técnica estimada | **0 min** | Registrar | Tiempo que estima SonarQube para resolver sus *code smells*; no incluye la deuda arquitectónica ni operativa del inventario de la sección 4. |

Hallazgos del código total (rama `main`) en los tres análisis de este ejercicio:

| Indicador | Primero (`99f5b38`) | Intermedio (`37605ca`) | **Final (`fa3673c`)** |
|---|---|---|---|
| Líneas de código | 7.067 | 7.077 | **7.096** |
| Bugs | 7 | 0 | **0** |
| Vulnerabilidades | 9 | 0 | **0** |
| *Security hotspots* | 0 | 0 | **0** |
| *Code smells* | 3 | 3 | **0** |
| Deuda técnica | 60 min | 60 min | **0 min** |
| Duplicación | 1,1 % | 1,1 % | **1,1 %** |
| Mantenibilidad | A | A | **A** |

- **Fecha de los análisis:** 25 de octubre de 2026.
- **Commit de la medición final:** `fa3673c558d00b64742c50b417cdb783971e1fff`.
- **Herramienta:** SonarQube Cloud, análisis ejecutado en el CI (paso *SonarQube scan*); las cifras
  salen del paso *Verify Entrega 3 thresholds*.
- **Perfil de reglas:** "Sonar way core", el perfil integrado y predeterminado de SonarQube Cloud para
  todos los lenguajes. Los usados en el último análisis son Go (52 reglas activas), TypeScript (248),
  JavaScript (249), Docker (35), CSS (30), HTML (36), YAML (2), Shell (11), PowerShell (19) y PL/SQL (49).
  No hay perfiles personalizados.
- **Reporte:** <https://sonarcloud.io/summary/overall?id=dfuseche_proyecto-plataforma-mooc-cloud&branch=main>
- La puerta de calidad por defecto de SonarQube marcó fallo por la cobertura del código nuevo
  (23,5 % frente a 80 %); la cobertura no es un criterio de la Entrega 3, que evalúa duplicación,
  mantenibilidad y deuda sobre el código total.
- Una lectura anterior del paso de umbrales tomó las mediciones del análisis previo (el paso no esperaba
  al análisis del commit en curso); se corrigió en `ci.yml` y la medición final es la del commit `fa3673c`.

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
| C-13 | SonarQube `typescript:S8786` ×3: las expresiones regulares del renderizador de Markdown tenían retroceso super-lineal con texto sin cerrar. Se acotaron los cuantificadores, se eliminó una regex de enlaces y se quitó la ambigüedad en la de títulos. Confirmado en el análisis de `fa3673c` (0 *code smells*). | `frontend/src/components/Markdown.tsx` |

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

SonarQube no deja hallazgos abiertos en el commit final (0 bugs, 0 vulnerabilidades, 0 *code smells*),
así que el plan se apoya en el inventario de la sección 4. Se ordena por impacto y esfuerzo; el
responsable y la fecha concretos los fija el equipo al cerrar la entrega.

| Orden | Id | Acción propuesta | Esfuerzo estimado | Cuándo | Responsable |
|---|---|---|---|---|---|
| 1 | D-04 | Validar el avance contra la duración real del recurso | Medio | Siguiente iteración | Daniel |
| 2 | D-03 | Generar y subir las imágenes de insignias; usar una URL base pública | Medio | Siguiente iteración | Julian |
| 3 | D-05 | Completar `openapi.yaml` con las 55 operaciones | Medio | Siguiente iteración | Carlos |
| 4 | D-07 | Extender el límite de tasa a toda la API | Bajo | Siguiente iteración | Daniel |
| 5 | D-09 | Filtrar publicados en SQL | Bajo | Siguiente iteración | Julian |
| 6 | D-10 | Pruebas del frontend (Vitest y Testing Library) | Alto | Siguiente iteración | Sergio |
| 7 | D-13 | Actualizar `docs/CD.md` al despliegue por MIG | Bajo | Antes de la entrega | Equipo (compañero a cargo de CD) |
| 8 | D-14 | Limpiar datos de prueba de k6 en la base y el bucket | Bajo | Antes de la sustentación | Carlos |
| 9 | D-08 | Ejecutar `gofmt` sobre los archivos pendientes | Bajo | Siguiente iteración | Sergio |
| – | D-02 | Aceptada hasta contar con un servicio de correo | Alto | Cuando exista el servicio | Julian |
| – | D-11 | Aceptada: requiere dominio propio para un certificado válido | Medio | Cuando exista dominio | Carlos |
| – | D-12 | Aceptada: alta disponibilidad de Redis y Cloud SQL fuera del alcance | Alto | Fuera de esta entrega | Daniel |
