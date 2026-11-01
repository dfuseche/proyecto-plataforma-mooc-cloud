# Prueba de los cambios de autoscaling (bloqueos del Escenario 3)

Pasos para desplegar y comprobar en GCP los cuatro bloqueos corregidos antes de correr el Escenario 3
([guía completa](guia-escenario3-autoscaling.md)). Cada paso dice qué ejecutar, qué debes ver y qué
captura guardar. Tiempo: unas 2 horas la primera vez, de las cuales 35 minutos son de espera en el paso 7.

| Bloqueo | Qué se cambió | Archivos |
|---|---|---|
| 1. Los workers escalaban por CPU | Exportador que publica `custom.googleapis.com/mooc/asynq/backlog` (pendientes + activos) desde la VM de Redis; el grupo de workers escala con 2 trabajos por VM | [`backend/cmd/queue-metrics/`](../../backend/cmd/queue-metrics/main.go), [`backend/internal/queuemetrics/`](../../backend/internal/queuemetrics/), [`deploy/scripts/queue-metrics.sh`](../../deploy/scripts/queue-metrics.sh), [`scripts/gcp/exportador-cola.ps1`](../../scripts/gcp/exportador-cola.ps1) |
| 2. No se sabía qué réplica respondía | Cabeceras `X-Served-By` (nombre de la VM) y `X-App-Version` (commit) en todas las respuestas de la API | [`served_by.go`](../../backend/internal/middleware/served_by.go), [`cmd/api/main.go`](../../backend/cmd/api/main.go), [`Dockerfile`](../../backend/Dockerfile) |
| 3. Los logs se perdían al borrar una VM | Los contenedores escriben en Cloud Logging (`--log-driver=gcplogs`, con respaldo si falta el permiso) | [`deploy/startup/startup.sh`](../../deploy/startup/startup.sh), [`scripts/gcp/actualizar-plantillas.ps1`](../../scripts/gcp/actualizar-plantillas.ps1) |
| 4. `encender.ps1` ponía CPU 60 % en los dos grupos | Una política por grupo (`Set-ApiAutoscaling` / `Set-WorkerAutoscaling`); `estado.ps1` muestra la política efectiva y la decisión del autoscaler | [`config.ps1`](../../scripts/gcp/config.ps1), [`encender.ps1`](../../scripts/gcp/encender.ps1), [`aplicar-autoscaling.ps1`](../../scripts/gcp/aplicar-autoscaling.ps1), [`estado.ps1`](../../scripts/gcp/estado.ps1) |

Las capturas van en `docs/entrega3/evidencias/escenario3/` con los nombres indicados en cada paso.

---

## 0. Preparar tu equipo (una sola vez)

1. Instala **Google Cloud CLI** para Windows: <https://cloud.google.com/sdk/docs/install>. Ahora no está
   instalada en tu equipo y todos los scripts la usan. Al terminar, abre una terminal **nueva** de PowerShell.
2. Inicia sesión y elige el proyecto:
   ```powershell
   gcloud auth login
   gcloud config set project proyecto-plataforma-mooc
   ```
3. Abre PowerShell en la raíz del repo. Si Windows bloquea los scripts, permítelos en esta sesión:
   ```powershell
   Set-ExecutionPolicy -Scope Process Bypass
   ```
4. Para el paso 2 con `-Aplicar` necesitas el rol **Owner** o **Project IAM Admin**. Si no lo tienes, el
   script imprime los comandos para que los ejecute quien sí lo tenga.

---

## 1. Encender y desplegar el código

1. Enciende el proyecto:
   ```powershell
   .\scripts\gcp\encender.ps1
   ```
   **Esperado:** termina con "Todo funcional en … min". En el paso 3/4 dice
   `mooc-worker-mig configurado (cola: 2 trabajos por VM)`. Aún no existe la métrica de la cola; es normal
   y el grupo de workers se queda en 1.
2. Haz commit de los cambios y súbelos a `main` (o abre un PR y combínalo).
3. En GitHub → **Actions**, espera a que **Backend CI Pipeline** termine en verde. **Deploy to GCP (CD)**
   arranca solo, construye la imagen (ya trae `/app/queue-metrics`) y reemplaza las instancias.
   - Si el CD corrió con el proyecto apagado: **Deploy to GCP (CD)** → **Run workflow** sobre `main`.
4. **Esperado:** el pie del frontend (`https://34.49.211.71`) muestra el hash corto del commit
   (`git rev-parse --short HEAD`).

---

## 2. Permisos de las cuentas de servicio (una sola vez)

```powershell
.\scripts\gcp\revisar-permisos.ps1            # solo revisa
.\scripts\gcp\revisar-permisos.ps1 -Aplicar   # además asigna los roles que falten
```

**Esperado:** "Todo en orden". El script revisa:

- **Plantillas de los dos grupos:** `roles/logging.logWriter`, para los logs de los contenedores.
- **VM de Redis:** `roles/monitoring.metricWriter`, `roles/logging.logWriter` y `roles/artifactregistry.reader`,
  además de los *scopes* de la VM.

Si una cuenta tiene `roles/editor` u `roles/owner`, cuenta como cubierta. Si falta un **scope** en la VM
de Redis, el script imprime los tres comandos para corregirlo; hay que detener la VM, lo que detiene la cola unos minutos.

---

## 3. Bloqueo 2: identificador de instancia en la API

Prueba inmediata, después del CD:

```powershell
curl.exe -skI https://34.49.211.71/health
```

**Esperado** (además de `HTTP/… 200`):

```
x-served-by: 3f9c2a7d1b4e          <- antes del paso 4 es el ID del contenedor
x-app-version: 8a09ca5e...          <- commit que desplegó el CD
```

Después del paso 4, `x-served-by` muestra el nombre de la VM (`mooc-api-mig-xxxx`).

**Criterio:** las dos cabeceras existen y `x-app-version` coincide con el commit de `main`.

---

## 4. Bloqueo 3: plantillas nuevas (logs en Cloud Logging y nombre de VM)

Las plantillas guardan una copia del `startup-script`, así que el cambio de `startup.sh` solo llega a las
VMs con una plantilla nueva. El script la crea copiando la actual y reemplaza las instancias sin bajar capacidad:

```powershell
.\scripts\gcp\actualizar-plantillas.ps1
```

**Esperado** (10 minutos aprox.), para cada grupo:

```
==> mooc-api-mig
    plantilla actual: <nombre>
    cuenta de servicio: …  scopes: …
    Plantilla creada: <nombre>-20261031-1530
    mooc-api-mig usa ahora <nombre>-20261031-1530
    Reemplazando instancias (crea la nueva antes de borrar la vieja)...
    mooc-api-mig estable con la plantilla nueva.
```

Al final imprime los comandos para volver a la plantilla anterior: **cópialos a un archivo**.

**Comprobaciones:**

1. Nombre de la VM en la cabecera:
   ```powershell
   curl.exe -skI https://34.49.211.71/health | Select-String 'x-served-by'
   .\scripts\gcp\estado.ps1
   ```
   El valor de `x-served-by` debe aparecer en la lista de instancias de `mooc-api-mig`.
2. Logs en Cloud Logging. Consola → **Logging → Logs Explorer**, con la consulta:
   ```
   logName:"gcplogs-docker-driver"
   ```
   Deben verse líneas de las dos VMs, con `jsonPayload.instance.name` = `mooc-api-mig-…` y `mooc-worker-mig-…`.
   Si no aparece nada, busca `"gcplogs no disponible"`: significa que falta `roles/logging.logWriter`
   (paso 2). Después de asignarlo, vuelve a correr el script de este paso.

**Captura:** `01-logs-gcplogs.png` (Logs Explorer con líneas de las dos instancias).

---

## 5. Bloqueo 1 (parte 1): exportador de la cola en la VM de Redis

```powershell
.\scripts\gcp\exportador-cola.ps1
```

La primera vez, gcloud crea tu llave SSH y puede abrir una ventana de PuTTY: acepta. Si falla con
*timeout* (la VM de Redis no tiene IP pública), usa `.\scripts\gcp\exportador-cola.ps1 -Iap`.

**Esperado** al final:

```
mooc-queue-metrics  Up 20 seconds
[QUEUE-METRICS] Iniciado: colas=[critical default low] intervalo=15s publicar=true
[QUEUE-METRICS] backlog=0 pending=0 active=0 retry=0 archived=0 oldest_pending_s=0.0 processed_total=… failed_total=…
```

| Si ves | Significa | Qué hacer |
|---|---|---|
| `exec: "/app/queue-metrics": no such file` | La imagen es anterior a este cambio | El CD no corrió: vuelve al paso 1 |
| `Publicando en Cloud Monitoring: cloud monitoring returned 403` | Falta `roles/monitoring.metricWriter` o el scope | Paso 2 y vuelve a correr este paso |
| `denied` al hacer `docker pull` | La VM de Redis no puede leer Artifact Registry | Paso 2 |

**Alternativa sin gcloud ssh:** Consola → Compute Engine → VM de Redis → botón **SSH** → menú
**Subir archivo** → `deploy/scripts/queue-metrics.sh` → `sudo bash queue-metrics.sh <imagen>`.
La `<imagen>` es la línea "Imagen: …" que imprimió el script al empezar.

**Comprobación:** Consola → **Monitoring → Metrics Explorer** → métrica "backlog"
(`custom.googleapis.com/mooc/asynq/backlog`, recurso *Global*). Aparece en 1–2 minutos con valor 0.

---

## 6. Bloqueos 1 (parte 2) y 4: políticas por grupo

```powershell
.\scripts\gcp\aplicar-autoscaling.ps1
```

**Esperado:**

```
    La metrica custom.googleapis.com/mooc/asynq/backlog existe en Cloud Monitoring.
    modo=ON min=1 max=3 inicializacion=120s reduccion<=1/300s senal: CPU 0.6
    modo=ON min=1 max=3 inicializacion=180s reduccion<=1/300s senal: custom.googleapis.com/mooc/asynq/backlog (2 por VM)
```

Luego `.\scripts\gcp\estado.ps1`. En `mooc-worker-mig` debe decir `recomendado por el autoscaler=1` y,
después de 2–3 minutos, ya **no** debe mostrar "aviso del autoscaler … did not receive any data".

**Bloqueo 4:** vuelve a correr `.\scripts\gcp\encender.ps1` con el proyecto encendido; no apaga nada, solo
reaplica la configuración. Después, `.\scripts\gcp\estado.ps1` debe seguir mostrando para `mooc-worker-mig`
la señal `custom.googleapis.com/mooc/asynq/backlog (2 por VM)`, no `CPU 0.6`.

**Captura:** `02-politicas.png` (salida de `estado.ps1` con las dos políticas).

---

## 7. Prueba de punta a punta: los workers escalan por la cola

Seis profesores virtuales suben videos sin parar durante 8 minutos. Cada uno tiene siempre un trabajo
en el sistema, así que `backlog` ≈ 6 → ⌈6/2⌉ = **3 workers**.

1. Enciende la VM generadora y entra por SSH (Consola → `mooc-load-generator` → **SSH**):
   ```powershell
   gcloud compute instances start mooc-load-generator --zone=us-central1-a
   ```
2. En esa VM:
   ```bash
   cd ~/proyecto-plataforma-mooc-cloud && git pull     # la carpeta donde clonaron el repo
   k6 run --insecure-skip-tls-verify -e BASE_URL=https://34.49.211.71 \
     -e UPLOAD_VUS=6 -e UPLOAD_DURATION=8m -e PLAYBACK_VUS=1 -e PLAYBACK_DURATION=8m \
     -e PLAYBACK_POOL_SIZE=1 -e MAX_WAIT_FOR_MANIFEST_SECONDS=900 \
     loadtests/k6/media-load-test.js
   ```
3. En tu PowerShell, **empieza a grabar la pantalla** (Win + Alt + R) y deja corriendo:
   ```powershell
   .\scripts\gcp\estado.ps1 -Cada 20
   ```
   En el navegador, ten abiertos el Metrics Explorer con `backlog` y el Logs Explorer en modo **Stream logs** con:
   ```
   jsonPayload.message:"Iniciando transcodificación"
   ```

**Qué debe pasar:**

| Minuto | `estado.ps1` / Metrics Explorer | Logs Explorer |
|---|---|---|
| 0–2 | k6 prepara 1 video; `backlog` 0–1; `recomendado=1` | Un worker procesa el video del *setup* |
| 2–4 | `backlog` sube a ≈ 6 → **`recomendado por el autoscaler=3`**, `tamano objetivo=3`, dos VMs nuevas en `CREATING` | — |
| +2–4 min | Las VMs nuevas pasan a `RUNNING` / `NONE` | Aparece "Iniciando transcodificación" con `instance.name` de las VMs **nuevas** |
| 10–12 | k6 termina; `backlog` baja a 0 en pocos minutos | Se drenan los últimos trabajos |
| +10 min con `backlog` = 0 | `recomendado=1`; `tamano objetivo` baja 3 → 2 | — |
| +5 min | 2 → 1 | — |

Duración total: unos 35 minutos. Que k6 marque umbrales en rojo (por ejemplo `media_processing_timeouts`)
no importa en esta prueba: lo que se valida es el escalamiento.

**Cierre del bloqueo 3:** cuando una VM de worker ya fue borrada, en Logs Explorer filtra por su nombre:
```
jsonPayload.instance.name="mooc-worker-mig-xxxx"
```
Sus logs siguen ahí aunque la VM ya no existe.

**Capturas:**

- `03-workers-escalan-a-3.png`: `estado.ps1` con `recomendado=3` y 3 instancias.
- `04-worker-nuevo-procesa.png`: Logs Explorer con una VM nueva transcodificando.
- `05-backlog-y-tamano.png`: Metrics Explorer con `backlog` y el tamaño del grupo en la misma ventana de tiempo.
- `06-reduccion-a-1.png`: `estado.ps1` de nuevo con 1 instancia.
- `07-logs-vm-borrada.png`: logs de una VM que ya no existe.

---

## 8. (Opcional) Dos réplicas de la API en `x-served-by`

En la VM generadora, carga académica de 150 VUs durante unos 5 minutos:

```bash
k6 run --insecure-skip-tls-verify -e BASE_URL=https://34.49.211.71 -e MAX_VUS=150 -e POOL_SIZE=300 \
  -e LEVEL_RUN=true loadtests/k6/load-test.js
```

En tu PowerShell, a la vez:

```powershell
1..60 | ForEach-Object { "{0:HH:mm:ss}  {1}" -f (Get-Date), (curl.exe -skI https://34.49.211.71/health | Select-String 'x-served-by').Line; Start-Sleep 5 }
```

Si la CPU de la API pasa del 60 %, aparece una segunda réplica y la columna alterna entre dos nombres.
Si no escala, no es un fallo de este cambio: es el dato que necesita la calibración de 3A (sección 5.1 de
la [guía completa](guia-escenario3-autoscaling.md)). Anota la CPU que alcanzó la réplica.

---

## 9. Al terminar

1. Apaga: `.\scripts\gcp\apagar.ps1` y `gcloud compute instances stop mooc-load-generator --zone=us-central1-a`.
   El exportador vuelve a arrancar solo cuando `encender.ps1` enciende la VM de Redis.
2. Haz commit de las capturas.
3. Avisa al equipo (sobre todo a A) de:
   - Los nombres de las plantillas nuevas y los comandos para volver a la anterior (paso 4).
   - Los roles asignados (paso 2).
   - Que la VM de Redis ahora también corre el contenedor `mooc-queue-metrics`.
   - Que el grupo de workers ahora escala por la cola y no por CPU.

## 10. Si algo falla

| Síntoma | Causa probable | Solución |
|---|---|---|
| `gcloud` no se reconoce | Falta Google Cloud CLI o la terminal es anterior a instalarla | Paso 0 y abrir una terminal nueva |
| No aparecen `x-served-by` ni `x-app-version` | El CD no desplegó la imagen nueva | Paso 1.3 (Run workflow) |
| `actualizar-plantillas.ps1` falla al crear la plantilla | Permisos de Compute o un campo que la API rechaza | Copia el mensaje completo y compártelo; los MIG siguen con la plantilla anterior |
| Una VM nueva de la API nunca queda `HEALTHY` | El arranque falló | Logs Explorer: `resource.type="gce_instance" AND "startup-script"`; si es grave, usa los comandos de "volver atrás" del paso 4 |
| `estado.ps1` muestra "did not receive any data from the custom metric" | El exportador no publica | Paso 5; revisa `docker logs mooc-queue-metrics` en la VM de Redis |
| En el paso 7 `recomendado` se queda en 1 | `backlog` no sube: los videos no llegan a la cola | Revisa que k6 no falle en `complete-upload` y que el backlog cambie en Metrics Explorer |
| No reduce después de que k6 termina | Es la estabilización de 10 min + 1 VM cada 5 min | Esperar hasta 25 minutos |
