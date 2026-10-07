# 🚀 CD — Despliegue automático a GCP

Hasta ahora, desplegar un cambio significaba: entrar por SSH a cada VM a
mano, hacer `git pull` y reconstruir con `docker compose`. Esta guía
automatiza eso con GitHub Actions: al llegar a `main` y pasar el CI
(`.github/workflows/ci.yml`), `.github/workflows/deploy.yml` se dispara
solo y actualiza `web-server` y `worker-server`.

**Esto requiere una configuración única en GCP** (crear una cuenta de
servicio, un Workload Identity Pool y un firewall rule) antes de que el
workflow pueda correr. Una vez hecha esa configuración, no hay que
repetirla — cada push a `main` despliega solo.

---

## 0. Qué necesitas tener a mano

Antes de empezar, reemplaza estos valores por los reales de tu proyecto
en TODOS los comandos de abajo (son placeholders, no están en el repo
por diseño — ver sección 4):

| Placeholder | Qué es | Cómo lo obtienes |
| :--- | :--- | :--- |
| `PROJECT_ID` | ID del proyecto GCP | `gcloud config get-value project` |
| `ZONE` | Zona donde viven las VMs | p. ej. `us-central1-a` — la que usaste al crear `web-server`/`worker-server` |
| `WEB_INSTANCE` | Nombre de la VM pública | `gcloud compute instances list` |
| `WORKER_INSTANCE` | Nombre de la VM privada | `gcloud compute instances list` |
| `GITHUB_REPO` | `usuario-o-org/nombre-repo` tal cual aparece en la URL de GitHub | — |
| `REPO_PATH_ON_VM` | Carpeta donde ya clonaste el repo en cada VM | `pwd` dentro de la VM, en la carpeta del proyecto |

---

## 1. Permitir que GitHub Actions llegue por SSH (firewall + IAP)

Las VMs no deberían aceptar SSH desde cualquier IP de Internet — con
**IAP (Identity-Aware Proxy)**, Google hace de intermediario: ni
siquiera `worker-server` (sin IP pública) necesita abrir el puerto 22 al
mundo.

```bash
# Permite SSH SOLO desde el rango de IAP (no desde "cualquier IP")
gcloud compute firewall-rules create allow-iap-ssh \
  --project=PROJECT_ID \
  --network=mooc-vpc \
  --direction=INGRESS \
  --action=ALLOW \
  --rules=tcp:22 \
  --source-ranges=35.235.240.0/20
```

Verifica que ya tenías esto o que quedó creado:

```bash
gcloud compute firewall-rules list --project=PROJECT_ID
```

> Prueba manual antes de automatizar nada: desde tu máquina,
> `gcloud compute ssh WEB_INSTANCE --project=PROJECT_ID --zone=ZONE --tunnel-through-iap`
> debe conectarte. Si falla aquí, el workflow tampoco va a funcionar —
> depura esto primero.

---

## 2. Crear la cuenta de servicio dedicada al deploy

Nunca reutilices tu cuenta personal ni una cuenta de servicio con
permisos amplios — esta cuenta solo necesita poder conectarse por SSH vía
IAP, nada más (no puede borrar VMs, no puede tocar Cloud SQL, etc.).

```bash
gcloud iam service-accounts create github-deployer \
  --project=PROJECT_ID \
  --display-name="GitHub Actions - Deploy MOOC"

# Permisos mínimos: solo túnel IAP + ver instancias + loguearse en ellas
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:github-deployer@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/iap.tunnelResourceAccessor"

gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:github-deployer@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/compute.viewer"

gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:github-deployer@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/compute.osLogin"
```

---

## 3. Workload Identity Federation (sin llaves JSON de larga duración)

En vez de descargar una llave JSON (un secreto que vive para siempre y
hay que rotar a mano), GitHub Actions le pide a GCP un token OIDC de
corta duración en cada corrida, y GCP confía en ese token SOLO si viene
de tu repositorio exacto.

```bash
PROJECT_NUMBER=$(gcloud projects describe PROJECT_ID --format="value(projectNumber)")

# 1. El "pool" de identidades externas
gcloud iam workload-identity-pools create "github-actions-pool" \
  --project=PROJECT_ID \
  --location="global" \
  --display-name="GitHub Actions"

# 2. El "provider" dentro del pool, atado a tu repo específico
#    (attribute-condition evita que CUALQUIER repo de GitHub pueda
#    hacerse pasar por el tuyo)
gcloud iam workload-identity-pools providers create-oidc "github-actions-provider" \
  --project=PROJECT_ID \
  --location="global" \
  --workload-identity-pool="github-actions-pool" \
  --display-name="GitHub Actions Provider" \
  --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
  --attribute-condition="assertion.repository=='GITHUB_REPO'"

# 3. Dejar que SOLO los workflows de ese repo "sean" la cuenta de
#    servicio del paso 2 (impersonación, no una llave compartida)
gcloud iam service-accounts add-iam-policy-binding \
  "github-deployer@PROJECT_ID.iam.gserviceaccount.com" \
  --project=PROJECT_ID \
  --role="roles/iam.workloadIdentityUser" \
  --member="principalSet://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github-actions-pool/attribute.repository/GITHUB_REPO"

# 4. Imprime el nombre completo del provider — lo vas a necesitar en el paso 4
gcloud iam workload-identity-pools providers describe "github-actions-provider" \
  --project=PROJECT_ID \
  --location="global" \
  --workload-identity-pool="github-actions-pool" \
  --format="value(name)"
```

El último comando imprime algo como:

```
projects/123456789012/locations/global/workloadIdentityPools/github-actions-pool/providers/github-actions-provider
```

Guárdalo — es el valor exacto de `GCP_WORKLOAD_IDENTITY_PROVIDER` del
paso 4.

---

## 4. Configurar las variables del repositorio en GitHub

En GitHub: **Settings → Secrets and variables → Actions → Variables**
(pestaña "Variables", no "Secrets" — ninguno de estos valores es
secreto, son identificadores, por eso `deploy.yml` los lee como
`${{ vars.X }}`):

| Nombre de la variable | Valor |
| :--- | :--- |
| `GCP_PROJECT_ID` | tu `PROJECT_ID` |
| `GCP_ZONE` | tu `ZONE` |
| `GCP_WEB_INSTANCE` | tu `WEB_INSTANCE` |
| `GCP_WORKER_INSTANCE` | tu `WORKER_INSTANCE` |
| `GCP_REPO_PATH` | tu `REPO_PATH_ON_VM` (misma ruta en ambas VMs) |
| `GCP_SERVICE_ACCOUNT_EMAIL` | `github-deployer@PROJECT_ID.iam.gserviceaccount.com` |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | la salida completa del último comando del paso 3 |

---

## 5. Qué hace `deploy.yml` en cada corrida

Ver [`.github/workflows/deploy.yml`](../.github/workflows/deploy.yml).
Resumen:

1. Se dispara cuando `ci.yml` ("Backend CI Pipeline") termina en `main`
   **con éxito** (o manualmente, vía "Run workflow").
2. Se autentica contra GCP sin ninguna llave almacenada (Workload
   Identity Federation, pasos 2-3 arriba).
3. Por SSH vía IAP, en `web-server`: `git fetch` + `git reset --hard
   origin/main` + `docker-compose -f deploy/docker-compose.web.yml up -d
   --build`.
4. Lo mismo en `worker-server` con `docker-compose.worker.yml`.
5. Verifica `GET /health` (desde dentro de `web-server`, por el mismo
   túnel — `worker-server` no tiene IP pública para pegarle desde fuera).

> **Nota sobre `git reset --hard`**: asume que el checkout en cada VM es
> de solo-despliegue (nadie edita archivos a mano ahí). Si alguna vez
> hiciste un cambio manual directo en una VM para probar algo, ese
> cambio se pierde en el siguiente deploy — es intencional (las VMs
> deben reflejar exactamente lo que hay en `main`, no un estado mezclado).

---

## 6. Primera prueba

1. Completa los pasos 1-4 de este documento.
2. Haz un cambio trivial (p. ej. un comentario) y haz push a `main`.
3. En GitHub → pestaña **Actions**, deberías ver primero correr
   "Backend CI Pipeline" y, si termina en verde, enseguida "Deploy to GCP
   (CD)".
4. Si falla, el error de `gcloud compute ssh` suele decir exactamente
   qué falta (permiso de IAM, firewall, o que el pool/provider no
   coincide con el nombre del repo).

**Confirmado funcionando end-to-end (2026-10-07)**: push a `main` → CI
verde → `deploy.yml` se dispara solo → `web-server` y `worker-server`
quedan actualizados automáticamente. Llegar a este punto requirió
resolver 6 problemas reales de configuración de GCP/VM no documentados
en ningún lado de antemano — ver
[`docs/TROUBLESHOOTING.md`](TROUBLESHOOTING.md) para el detalle de cada
uno (API deshabilitada, permiso de metadata, OS Login rompiendo el
acceso externo, `serviceAccountUser` faltante, ownership de Git, versión
de `docker-compose`).
