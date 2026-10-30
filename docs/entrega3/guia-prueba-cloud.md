# Guía: prueba del frontend en el entorno cloud

Objetivo: recorrer los flujos de estudiante, docente y administrador sobre el balanceador de GCP,
guardar la evidencia y llenar las celdas "(A)" de `docs/entrega3/frontend.md`. Dura unas 2 horas
la primera vez (el encendido tarda ~10 minutos y el procesamiento del video unos minutos más).

## 0. Antes de empezar

- `main` con el CI en verde y el commit que quieres entregar.
- Google Cloud CLI instalado y con sesión (`gcloud auth login`), PowerShell abierto en la raíz del repo.
- Material de prueba: un video corto (MP4 de 10 a 30 s con audio), un PDF pequeño y una imagen.
- Contraseña del administrador sembrado (`admin@mooc.com`), que ya está en la base de Cloud SQL.

## 1. Encender el proyecto

```powershell
.\scripts\gcp\estado.ps1      # opcional: confirma que detecta la zona, Redis y los MIGs
.\scripts\gcp\encender.ps1
```

Termina cuando imprime "Todo funcional en … min" y la dirección del balanceador. Si falla, copia el
mensaje completo (el script indica en qué paso se detuvo).

## 2. Confirmar que está desplegada la versión correcta

1. En GitHub, pestaña **Actions**, abre la última ejecución de **Deploy to GCP (CD)** sobre `main`.
   - Si falló o corrió mientras los grupos estaban apagados: **Run workflow** sobre `main` y espera a que termine.
2. Abre `https://34.49.211.71` (el navegador avisa del certificado autofirmado: Avanzado → Continuar).
3. Mira el pie de página: debe mostrar el hash corto del commit (`git rev-parse --short HEAD`).
   Si muestra `dev` o un hash viejo, el despliegue no corrió con el código actual.

## 3. Preparar el bucket de medios (solo la primera vez)

El navegador sube el video y reproduce los segmentos HLS directamente desde Cloud Storage, así que el
bucket debe permitir ese origen. El nombre del bucket es el valor de `S3_MEDIA_BUCKET` del servidor
(`mooc-media-gcp-bucket` en las plantillas de `deploy/`; confírmalo en la consola de Cloud Storage).

```powershell
$bucket = "NOMBRE_DEL_BUCKET"
@'
[{"origin":["https://34.49.211.71"],"method":["GET","HEAD","PUT"],"responseHeader":["Content-Type","Content-Length","Range"],"maxAgeSeconds":3600}]
'@ | Set-Content -Encoding ascii cors.json
gcloud storage buckets update "gs://$bucket" --cors-file=cors.json
gcloud storage buckets describe "gs://$bucket" --format="default(cors_config)"
```

## 4. Recorrido con evidencia

Haz capturas (o una sola grabación de pantalla) y guárdalas en `docs/entrega3/evidencias/` con el
nombre indicado. Usa el navegador en una ventana nueva para cada rol, o ventanas de incógnito.

| # | Rol | Pasos | Resultado esperado | Captura |
|---|---|---|---|---|
| 1 | Administrador | Entrar con `admin@mooc.com` → **Docentes** → crear `docente.a` y `docente.b` | Cuentas listadas en "Creadas en esta sesión" | `01-admin-crea-docentes.png` |
| 2 | Docente A | Entrar → **Docencia** → crear curso → módulo → unidad | Estructura visible en el editor | `02-docente-estructura.png` |
| 3 | Docente A | Intentar **publicar** sin recursos | Lista de errores de publicación | `03-publicacion-rechazada.png` |
| 4 | Docente A | Añadir una lectura, el video (esperar "Listo") y una evaluación con preguntas | Video pasa de "Procesando" a "Listo" | `04-recursos-video-listo.png` |
| 5 | Docente A | **Publicar** | Curso publicado | `05-curso-publicado.png` |
| 6 | Estudiante | Crear cuenta en **Crear cuenta**, entrar, ver el catálogo | El curso aparece | `06-catalogo.png` |
| 7 | Estudiante | Abrir el curso → **Inscribirme** → **Mis cursos** | Curso listado con 0 % de avance | `07-inscripcion.png` |
| 8 | Estudiante | Abrir la lectura y reproducir el video unos segundos | Video se reproduce; el avance sube | `08-video-avance.png` |
| 9 | Estudiante | Presentar la evaluación hasta aprobar | Resultado y curso aprobado | `09-quiz-aprobado.png` |
| 10 | Estudiante | Abrir `/docente` | Acceso denegado | `10-estudiante-sin-docencia.png` |
| 11 | Docente B | Entrar y abrir `/docente/cursos/<id del curso de A>`; intentar añadir un módulo | Error 403 en la acción de edición | `11-docente-b-403.png` |

Para el paso 11, copia el identificador del curso de la barra de direcciones del paso 2.

## 5. Registrar la evidencia

1. En `docs/entrega3/frontend.md`, reemplaza cada "(A)" de la matriz por el nombre del archivo de captura
   (por ejemplo `evidencias/08-video-avance.png`) y anota la fecha y el hash del commit probado.
2. Si algo no funcionó, no lo ocultes: anótalo en `docs/entrega3/deuda-tecnica.md` (sección 4).
3. Haz commit de las capturas y del documento.

## 6. Si algo falla

| Síntoma | Qué revisar |
|---|---|
| El balanceador devuelve 502 o 503 | Los backends tardan unos minutos en marcarse sanos; `.\scripts\gcp\estado.ps1` muestra el estado de cada instancia |
| El pie de página dice `dev` | El despliegue no corrió con la imagen nueva (paso 2) |
| El video se queda en "Procesando" | El worker no tomó la tarea: revisar que el grupo del worker tenga al menos 1 instancia y que Redis esté encendida |
| El video no se reproduce | Consola del navegador (F12): un error de CORS apunta al paso 3; uno de CSP, a `CSP_MEDIA_HOSTS` |
| La subida del archivo falla | Mismo origen de CORS (paso 3); confirma que el bucket es el de `S3_MEDIA_BUCKET` |
| Entrar como administrador falla | La contraseña sembrada es la que ya existe en la base; no cambia por la variable `ADMIN_PASSWORD` |

## 7. Apagar para no gastar

```powershell
.\scripts\gcp\apagar.ps1
```

Baja los grupos a 0, detiene Redis y apaga Cloud SQL. Los datos y la evidencia se conservan.
