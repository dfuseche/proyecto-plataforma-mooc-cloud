# Stage 1: Compilar la SPA de React con Vite
FROM node:22-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .

# Inyectar URL del backend y versión en el build
ARG VITE_API_URL=""
ENV VITE_API_URL=$VITE_API_URL

ARG APP_VERSION="entrega-4"
ENV VITE_APP_VERSION=$APP_VERSION

RUN npm run build

# Stage 2: Servidor web ligero con Nginx
FROM nginx:alpine

# Limpiar archivos por defecto de Nginx
RUN rm -rf /usr/share/nginx/html/*

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
