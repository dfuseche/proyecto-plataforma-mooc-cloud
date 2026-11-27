# Entrega 5: guía de evidencias y video

Todo se ejecuta después de desplegar con Terraform (ver [infra/terraform/README.md](../../infra/terraform/README.md)). Guarda las capturas en `docs/entrega5/evidencias/`.

## 1. Evidencia del despliegue con Terraform

Captura o graba, desde `infra\terraform`:

1. `terraform init "-backend-config=backend.hcl"`
2. `terraform plan "-var=image_tag=<TAG>" -out=tfplan` (muestra los recursos a crear)
3. `terraform apply tfplan` hasta `Apply complete!` y los `Outputs`
4. Una **actualización**: cambia algo pequeño (por ejemplo, `max_instances` a 2), repite plan y apply, y muestra que solo cambia lo necesario.
5. En la consola de GCP: los 3 servicios de Cloud Run, los 2 topics, la suscripción push y la función con su disparador de Pub/Sub.

## 2. Una tarea real, de Pub/Sub a la función y su resultado

**Opción A: flujo real desde la aplicación**

1. Entra al frontend con un docente y sube un video o un PDF a un recurso (se llama a `complete-upload`).
2. Logs de la función:
   ```powershell
   gcloud functions logs read mooc-antimalware-scan-tf --gen2 --region=us-central1 --project=proyecto-plataforma-mooc --limit=20
   ```
   Debe aparecer `[SCAN-RESULT] ... scan_status=clean`.
3. Resultado en el objeto (sustituye `<object_key>` por la ruta que sale en el log):
   ```powershell
   gcloud storage objects describe gs://mooc-media-equipo-3/<object_key> --format="yaml(metadata)"
   ```

**Opción B: mensaje manual (también sirve para mostrar el rechazo y los reintentos)**

1. Crea un archivo falso con cabecera de ejecutable de Windows (8 bytes, inofensivo) y súbelo:
   ```powershell
   [IO.File]::WriteAllBytes("$env:TEMP\falso.mp4", [byte[]](0x4d,0x5a,0x90,0x00,0x03,0x00,0x00,0x00))
   gcloud storage cp "$env:TEMP\falso.mp4" gs://mooc-media-equipo-3/pruebas/falso.mp4
   ```
2. Publica el mensaje de escaneo:
   ```powershell
   gcloud pubsub topics publish mooc-scan-topic-tf --project=proyecto-plataforma-mooc --message='{"resource_id":"11111111-1111-1111-1111-111111111111","bucket":"mooc-media-equipo-3","object_key":"pruebas/falso.mp4"}'
   ```
3. Espera unos segundos y mira los logs: debe aparecer `scan_status=rejected detectado=executable-pe`.
4. **Reintento sin duplicar:** repite el mismo `gcloud pubsub topics publish`. El log ahora dice `[SCAN-IDEMPOTENT] ... ya escaneado`, y `scan_at` en la metadata no cambia.
5. Comprueba que el worker ya no ejecuta el escaneo: en los logs de `mooc-worker-tf` no aparece `[WORKER-ANTIMALWARE]`.

## 3. Guion del video (máximo 20 minutos)

| Min | Qué mostrar |
|---|---|
| 0–3 | Análisis serverless: tabla de [arquitectura.md](arquitectura.md), por qué se eligió el escaneo y qué se documentó como alternativa |
| 3–6 | El cambio: diagrama, el código de la función, el cambio en `complete-upload` y el worker sin la tarea |
| 6–12 | Terraform: `init`, `plan` y `apply` en vivo (o grabado si tarda), los recursos creados en la consola |
| 12–17 | Tarea real: subida desde el frontend, mensaje en Pub/Sub, logs de la función y la metadata del objeto; luego el rechazo y el reintento idempotente |
| 17–19 | Configuración y acceso: variables, cuentas de servicio con mínimos privilegios, secretos en Secret Manager, estado remoto y nada de credenciales en el repositorio |
| 19–20 | Cierre: limitaciones y siguientes pasos |

## 4. Lista final para entregar

- [ ] Análisis y diagrama (`docs/entrega5/arquitectura.md`)
- [ ] Código de la función y Terraform (`functions/`, `infra/terraform/`)
- [ ] Capturas del despliegue y de la tarea real en `docs/entrega5/evidencias/`
- [ ] Enlace del video accesible para el evaluador, en el README
- [ ] Commit final y tag `entrega-5`
