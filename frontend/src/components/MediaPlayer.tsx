import { useEffect, useRef, useState } from 'react'
import Hls from 'hls.js'
import { api } from '../api/client'
import type { Resource } from '../api/types'
import { ErrorNote, Loading } from './ui'

interface Props {
  resource: Resource
  onHeartbeat(positionSeconds: number): void
}

const BEAT_SECONDS = 15

// Reproductor de video/audio. Los recursos transcodificados llegan como HLS
// (manifiesto firmado por la API); el resto, como una URL prefirmada directa.
export default function MediaPlayer({ resource, onHeartbeat }: Props) {
  const ref = useRef<HTMLVideoElement & HTMLAudioElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [ready, setReady] = useState(false)
  const beatRef = useRef(onHeartbeat)
  beatRef.current = onHeartbeat

  const waiting = resource.processing_status === 'pending' || resource.processing_status === 'processing'
  const failed = resource.processing_status === 'failed'

  useEffect(() => {
    if (waiting || failed) return
    let hls: Hls | null = null
    let cancelled = false
    setReady(false)
    setError(null)

    async function start() {
      try {
        const [stream, resume] = await Promise.all([
          api<{ presigned_url: string }>(`/media/resources/${resource.id}/stream-url`),
          api<{ last_position_seconds: number }>(`/media/resources/${resource.id}/resume`).catch(() => ({ last_position_seconds: 0 })),
        ])
        const el = ref.current
        if (cancelled || !el) return
        const url = stream.presigned_url
        const seek = () => { if (resume.last_position_seconds > 5) el.currentTime = resume.last_position_seconds }
        el.addEventListener('loadedmetadata', seek, { once: true })

        if (url.includes('manifest.m3u8') && Hls.isSupported()) {
          hls = new Hls()
          hls.on(Hls.Events.ERROR, (_e, d) => {
            if (d.fatal) setError('No se pudo reproducir el contenido. Recarga la página o inténtalo más tarde.')
          })
          hls.loadSource(url)
          hls.attachMedia(el)
        } else {
          el.src = url // Safari reproduce HLS de forma nativa; el resto son archivos directos
        }
        setReady(true)
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'No se pudo cargar el contenido.')
      }
    }
    start()
    return () => { cancelled = true; hls?.destroy() }
  }, [resource.id, waiting, failed])

  // Pulso de progreso mientras se reproduce.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const id = window.setInterval(() => {
      if (!el.paused && !el.ended) beatRef.current(Math.floor(el.currentTime))
    }, BEAT_SECONDS * 1000)
    const onEnd = () => beatRef.current(Math.floor(el.currentTime))
    el.addEventListener('ended', onEnd)
    return () => { window.clearInterval(id); el.removeEventListener('ended', onEnd) }
  }, [resource.id, ready])

  if (waiting) return <div className="note"><p>Este contenido se está procesando. Vuelve en unos minutos.</p></div>
  if (failed) return <ErrorNote message="El procesamiento de este contenido falló. Avisa a tu docente." />
  if (error) return <ErrorNote message={error} />

  return (
    <div className="player">
      {!ready && <Loading label="Preparando reproductor…" />}
      {resource.type === 'audio'
        ? <audio ref={ref} controls preload="metadata" aria-label={resource.title} />
        : <video ref={ref} controls playsInline preload="metadata" aria-label={resource.title} />}
    </div>
  )
}
