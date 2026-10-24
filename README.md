# 🚀 Plataforma Web MOOC — Entrega 3: Aplicación Elástica en IaaS (GCP)

Este repositorio contiene el código fuente, la infraestructura, las pruebas de carga y la documentación de la **Entrega 3 — Aplicación Elástica** de la Plataforma Web de Cursos Masivos Abiertos en Línea (MOOC): API y workers en grupos de instancias con autoescalado, balanceador HTTPS, frontend versionado y análisis de calidad con SonarQube.

---

## 📚 Documentación de la Entrega 3

- 🏛️ **[Arquitectura elástica](docs/entrega3/arquitectura.md)**: grupos de instancias de la API y de los workers, balanceador, cola, base de datos y almacenamiento.
- 🖥️ **[Frontend y verificación](docs/entrega3/frontend.md)**: flujos de estudiante, docente y administrador, reglas de autorización, accesibilidad y guion de demostración.
- 🧮 **[Deuda técnica y calidad (SonarQube)](docs/entrega3/deuda-tecnica.md)**: línea base, correcciones realizadas, inventario de deuda y plan de tratamiento.
- 📊 **[Pruebas de carga de la Entrega 3](capacity-planning/pruebas_de_carga_entrega3.md)**: escenarios, autoescalado y comparación con la Entrega 2.
- 🚚 **[Despliegue continuo](docs/CD.md)**: construcción de la imagen, publicación y actualización de los grupos de instancias.
- ⚙️ **[Operación: escalamiento y recuperación](docs/OPERACIONES.md)**: escalado con Compose y procedimiento de backup/restauración (RPO/RTO).

## 🗂️ Documentación de la Entrega 2

- 🏛️ **[Informe de Arquitectura Cloud](docs/entrega2/ARCHITECTURE_CLOUD.md)**: red, subredes, firewall y servicios.
- 📊 **[Pruebas de carga de la Entrega 2](capacity-planning/pruebas_de_carga_entrega2.md)** y 🧪 **[scripts k6](capacity-planning/scripts/)**.
- 🛠️ **[Plantillas de despliegue](deploy/)**: `.env.example` y composiciones para el servidor web y el worker.

---

## 🧱 Estructura del repositorio

| Carpeta | Contenido |
|---|---|
| `backend/` | API REST y worker en Go (`cmd/api`, `cmd/worker`, `internal/…`) y pruebas E2E |
| `frontend/` | Aplicación web (React + Vite + TypeScript); el backend la sirve desde la misma imagen |
| `deploy/` | Composiciones y plantillas de entorno para las instancias |
| `docs/` | Documentación por entrega |
| `capacity-planning/` | Pruebas de carga y sus informes |

La imagen de Docker se construye desde la raíz e incluye el frontend compilado:

```bash
docker build -f backend/Dockerfile --build-arg APP_VERSION=$(git rev-parse --short HEAD) .
```

## 💻 Ejecución local

```bash
docker compose up -d --build
```

- Aplicación y API: `http://localhost:8081` (la API está bajo `/api/v1`).
- Administrador inicial: el correo y la contraseña salen de `ADMIN_EMAIL` y `ADMIN_PASSWORD`. El
  `docker-compose.yml` define valores solo para desarrollo local; fuera de él hay que definirlos
  (en GCP, desde Secret Manager). Sin `ADMIN_PASSWORD` no se crea ningún administrador.
- El administrador crea las cuentas de docente desde **Docentes** (`/admin/docentes`). Los
  estudiantes se registran solos.

Pruebas:

```bash
docker compose --profile test build e2e-tests
docker compose --profile test run --rm e2e-tests
```

La suite E2E se reconstruye con el primer comando; sin él se ejecuta la imagen anterior.

---

## 🛠️ Arquitectura resumida

- **API y workers**: dos grupos de instancias independientes (1 a 3 instancias cada uno) con autoescalado por CPU.
- **Balanceador**: Application Load Balancer HTTPS externo con verificación de salud sobre la API.
- **Cola**: Redis fuera de los grupos elásticos.
- **Datos**: Cloud SQL for PostgreSQL 16 y Google Cloud Storage para los medios.
- **Frontend**: aplicación de una sola página servida por la API, versionada con el hash del commit (visible en el pie de página).
- Detalle y diagramas en [`docs/entrega3/arquitectura.md`](docs/entrega3/arquitectura.md).

---

### Arquitectura de la Entrega 2 (referencia)

- **Red VPC (`mooc-vpc`)**: Subred pública (`10.0.1.0/24`) y subred privada (`10.0.2.0/24`) con Cloud NAT.
- **`web-server` (GCE VM 1 - Pública)**: 2 vCPU, 2 GiB RAM. Ejecuta la API REST en Go. Exposición pública HTTP/HTTPS.
- **`worker-server` (GCE VM 2 - Privada)**: 2 vCPU, 2 GiB RAM. Ejecuta el Worker en Go (FFmpeg) y el contenedor Redis (Asynq).
- **Base de Datos Relacional**: `Cloud SQL for PostgreSQL 16` en subred privada.
- **Almacenamiento de Objetos**: `Google Cloud Storage (GCS)` con API de Interoperabilidad S3 y HMAC keys.

---

## 🎥 Video de Sustentación

- **Enlace al Video (Máx 20 min)**: `[Añadir enlace al video subido aquí]`

---

## 🔖 Release y Commit

- **Release Tag**: `entrega-3`
- **Commit de entrega**: `[Añadir hash del commit]`
