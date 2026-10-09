#!/usr/bin/env bash
# Regenera los 3 videos de prueba del Escenario 2 (perfiles ligero / medio /
# pesado). Solo hace falta correrlo si se quieren cambiar los perfiles: los
# .mp4 ya estan versionados en el repo. Requiere ffmpeg (libx264 + aac).
#
# Son videos sinteticos (patron de prueba + ruido temporal + tono de audio),
# pero archivos reales en lo que le importa a la plataforma: contenedor MP4,
# H.264 + AAC, y resolucion / duracion / tamano claramente distintos. El
# ruido evita que x264 los comprima a casi nada, para que el tamano en disco
# sea representativo del bitrate declarado.
#
#   perfil_ligero_360p_10s.mp4  640x360   10s  ~1.1 MB  -> escalera de 1 rendition  (360p)
#   perfil_medio_720p_20s.mp4   1280x720  20s  ~6.6 MB  -> escalera de 3 renditions (720p/480p/360p)
#   perfil_pesado_1080p_20s.mp4 1920x1080 20s  ~12.9 MB -> escalera de 4 renditions (1080p/720p/480p/360p)
set -euo pipefail
cd "$(dirname "$0")"

gen() { # nombre ancho alto duracion_s fps bitrate_video
  ffmpeg -y -loglevel error \
    -f lavfi -i "testsrc2=size=$2x$3:rate=$5,noise=alls=25:allf=t" \
    -f lavfi -i "sine=frequency=440:sample_rate=44100" \
    -t "$4" \
    -c:v libx264 -preset medium -b:v "$6" -maxrate "$6" -bufsize "$6" -pix_fmt yuv420p \
    -c:a aac -b:a 128k -movflags +faststart -shortest "$1.mp4"
}

gen perfil_ligero_360p_10s   640  360  10 25 800k
gen perfil_medio_720p_20s    1280 720  20 30 2500k
gen perfil_pesado_1080p_20s  1920 1080 20 30 5000k
