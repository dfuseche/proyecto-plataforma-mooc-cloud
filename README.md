# 🚀 Plataforma Web MOOC — Entrega 2: Despliegue Básico en Google Cloud Platform (GCP)

Este repositorio contiene el código fuente, la configuración de infraestructura, las pruebas de capacidad y la documentación de la **Entrega 2 — Despliegue Básico en la Nube** de la Plataforma Web de Cursos Masivos Abiertos en Línea (MOOC).

---

## 📚 Documentación y Entregables Clave

- 🏛️ **[Informe de Arquitectura Cloud (GCP)](docs/entrega2/ARCHITECTURE_CLOUD.md)**: Detalla la topología de red VPC, subredes, reglas de firewall, correspondencia de servicios (GCE, Cloud SQL, GCS, Redis) y guía de operación.
- ⚙️ **[Operación: escalamiento y recuperación](docs/OPERACIONES.md)**: Cómo escalar `api`/`worker` a múltiples instancias con Compose y el procedimiento de backup/restauración de PostgreSQL (RPO/RTO) con evidencia de una restauración real.
- 📊 **[Informe de Pruebas de Carga y Capacidad](capacity-planning/pruebas_de_carga_entrega2.md)**: Reporte con las mediciones, latencias (p50, p95, p99), throughput y cuellos de botella para el Escenario 1 (Actividad Académica) y Escenario 2 (Multimedia HLS).
- 🧪 **[Scripts de Pruebas de Carga](capacity-planning/scripts/)**: Scripts en K6 para reproducir los escenarios de carga.
- 🛠️ **[Plantillas de Entorno de Despliegue](deploy/)**: Plantillas `.env.example` y composiciones Docker desacopladas para `Web Server` y `Worker Server`.

---

## 🛠️ Arquitectura Resumida en GCP

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

- **Release Tag**: `entrega-2`
