# 🏛️ Documento de Arquitectura de Software — Despliegue Cloud (GCP)
## Entrega 2: Despliegue Básico en la Nube Pública

---

## 📋 1. Visión General y Modelo de Componentes Cloud

La arquitectura desplegada en **Google Cloud Platform (GCP)** traslada la solución monolítica modular en Go desarrollada en la Entrega 1 a una infraestructura como servicio (IaaS) con persistencia administrada en la nube.

```mermaid
graph TD
    Client[Cliente / Generador de Carga K6] -->|HTTPS / REST| PublicSubnet

    subgraph GCP VPC Network: mooc-vpc
        subgraph Subred Pública: mooc-public-subnet 10.0.1.0/24
            PublicSubnet[Web Server VM - GCE]
            PublicSubnet -->|Contenedor Docker| API[API REST Go - cmd/api]
        end

        subgraph Subred Privada: mooc-private-subnet 10.0.2.0/24
            PrivateSubnet[Worker Server VM - GCE]
            PrivateSubnet -->|Contenedor Docker| Worker[Async Worker Go - cmd/worker]
            PrivateSubnet -->|Contenedor Docker| Redis[(Redis 7 - Asynq Queue)]
            
            CloudSQL[(Cloud SQL - PostgreSQL 16)]
        end
    end

    subgraph Servicios Administrados Cloud
        GCS[(Google Cloud Storage - S3 Interoperability)]
    end

    API -->|Consulta / Mutación SQL| CloudSQL
    Worker -->|Consulta / Mutación SQL| CloudSQL
    API -->|Rate Limit / Idempotencia| Redis
    API -->|Encola Tareas Asynq| Redis
    Worker -->|Lee Tareas Asynq| Redis
    API -->|Genera Presigned URLs| GCS
    Worker -->|Descarga Raw / Transcodifica FFmpeg / Sube HLS| GCS
```

---

## ⚙️ 2. Correspondencia de Servicios en GCP

| Componente de Aplicación | Servicio GCP | Configuración Especificada |
| :--- | :--- | :--- |
| **Punto de Entrada API** | Compute Engine (GCE) `web-server` | 2 vCPU, 2 GiB RAM, 30 GB Balanced Disk (IP Pública) |
| **Worker Asíncrono** | Compute Engine (GCE) `worker-server` | 2 vCPU, 2 GiB RAM, 30 GB Balanced Disk (IP Privada) |
| **Cola Asynq & Caché** | Redis 7 en Contenedor Docker | Ejecutado dentro de `worker-server` (Puerto 6379 privado) |
| **Base de Datos Relacional** | Cloud SQL for PostgreSQL 16 | Instancia monozona con Private IP en `mooc-vpc` |
| **Almacenamiento de Objetos** | Google Cloud Storage (GCS) | Buckets `mooc-media` y `mooc-badges` (API Interoperabilidad S3 / HMAC) |
| **Red & Seguridad** | VPC Network & Subnets | `mooc-public-subnet` (10.0.1.0/24), `mooc-private-subnet` (10.0.2.0/24), Cloud NAT |

---

## 🔒 3. Decisiones de Seguridad y Aislamiento de Red

1. **Aislamiento de Componentes**:
   - `web-server` es el único componente expuesto a Internet en la subred pública (`10.0.1.0/24`).
   - `worker-server`, `Redis` y `Cloud SQL` residen exclusivamente en la subred privada (`10.0.2.0/24`) sin IPs públicas de entrada.
   - La comunicación saliente del `worker-server` para descarga de paquetes e imágenes Docker se canaliza vía **Cloud NAT**.
2. **Firewall / Security Groups**:
   - `allow-web-ingress`: Permite TCP 80/443 desde `0.0.0.0/0` a la VM `web-server`.
   - `allow-internal`: Permite tráfico interno TCP/UDP dentro del rango VPC `10.0.0.0/16`.
3. **Manejo de Secretos**:
   - Ninguna clave privada o secreto se almacena en el repositorio.
   - Las variables de entorno se inyectan en tiempo de ejecución mediante los archivos `web-server.env` y `worker-server.env`.

---

## 🛠️ 4. Guía de Operación y Recuperación

### Despliegue Inicial (una sola vez, manual)
1. Aprovisionar la red VPC, subredes, Cloud NAT, Cloud SQL e instancias GCE mediante `gcloud` o la consola GCP.
2. Clonar el repositorio en `web-server` y `worker-server`.
3. Copiar las plantillas `.env.example` a `.env` y configurar credenciales reales de Cloud SQL y HMAC Keys de GCS.
4. En `worker-server`: `docker compose -f deploy/docker-compose.worker.yml up -d`
5. En `web-server`: `docker compose -f deploy/docker-compose.web.yml up -d`

### Despliegue continuo (CD) para cambios posteriores
Los pasos 2, 4 y 5 de arriba ya **no se repiten a mano** en cada cambio:
`.github/workflows/deploy.yml` los automatiza — se dispara solo cuando
el CI pasa en `main` (o manualmente desde GitHub Actions) y hace
`git pull` + `docker compose up -d --build` en ambas VMs por SSH vía
IAP. Ver [`docs/CD.md`](../CD.md) para la configuración única de
GCP que esto requiere (cuenta de servicio, Workload Identity Federation,
firewall de IAP) y el detalle de qué hace cada corrida.

### Migración y Respaldo de Base de Datos
- Las migraciones SQL se ejecutan automáticamente al inicio de la aplicación o manualmente con `golang-migrate` desde el `web-server`.
- Los respaldos automáticos están activados en Cloud SQL con retención diaria.
