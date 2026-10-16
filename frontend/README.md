# Frontend (React + Vite + TypeScript)

SPA de estudiantes y docentes. En producción la sirve la propia API de Go desde
`/app/web` (el `Dockerfile` de `backend/` la compila en su primera etapa), por lo
que comparte origen con `/api/v1`.

```bash
cd frontend
npm ci
npm run dev        # http://localhost:5173, /api -> http://localhost:8080
npm run build      # tsc + vite build -> dist/
```

- Variable `VITE_API_TARGET` cambia el destino del proxy de desarrollo.
- Rutas: `/` catálogo, `/cursos/:id`, `/entrar`, `/registro`, `/mis-cursos`,
  `/aprender/:cursoId/:recursoId`, `/docente`, `/docente/cursos/:id`.
- El panel de administración no está incluido.
- Videos HLS: el navegador descarga segmentos desde el bucket, que necesita CORS
  (GET y PUT) para el origen del balanceador; la CSP de la API ya admite
  `storage.googleapis.com` (ampliable con `CSP_MEDIA_HOSTS`).
