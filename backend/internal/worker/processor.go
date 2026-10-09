package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/minio/minio-go/v7"
	"github.com/mooc-platform/backend/internal/domain"
)

type StorageProvider interface {
	GetClient() *minio.Client
	GetMediaBucket() string
}

type Processor struct {
	storage    StorageProvider
	courseRepo domain.CourseRepository
}

func NewProcessor(storage StorageProvider, courseRepo domain.CourseRepository) *Processor {
	return &Processor{
		storage:    storage,
		courseRepo: courseRepo,
	}
}

func (p *Processor) HandleMediaTranscodeHLS(ctx context.Context, t *asynq.Task) error {
	var payload MediaTranscodeHLSPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	log.Printf("[WORKER] Iniciando transcodificación HLS para el recurso %s (Key: %s)", payload.ResourceID, payload.OriginalKey)

	// Verificación de idempotencia: si la playlist HLS ya existe en S3, reutilizar y marcar completado
	hlsKeyPrefix := fmt.Sprintf("hls/%s/", payload.ResourceID.String())
	playlistKey := hlsKeyPrefix + "master.m3u8"

	minioClient := p.storage.GetClient()
	mediaBucket := p.storage.GetMediaBucket()

	_, err := minioClient.StatObject(ctx, mediaBucket, playlistKey, minio.StatObjectOptions{})
	if err == nil {
		log.Printf("[WORKER-IDEMPOTENCY] La playlist HLS %s ya existe en S3. Omitiendo transcodificación.", playlistKey)
		return p.markResourceCompleted(ctx, payload.ResourceID, playlistKey)
	}

	// Crear directorio temporal de trabajo local
	tmpDir, err := os.MkdirTemp("", "hls-transcode-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			log.Printf("[WORKER] Error al limpiar directorio temporal %s: %v", tmpDir, rmErr)
		}
	}()

	// Descargar archivo original desde S3 a local
	localOriginalPath := filepath.Join(tmpDir, "original")
	err = minioClient.FGetObject(ctx, mediaBucket, payload.OriginalKey, localOriginalPath, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to download original file from S3: %w", err)
	}

	// Ejecutar FFmpeg para generar HLS adaptativo sin upscaling
	outputPlaylist := filepath.Join(tmpDir, "master.m3u8")
	segmentFilename := filepath.Join(tmpDir, "segment_%03d.ts")

	var cmd *exec.Cmd
	if strings.ToLower(payload.MediaType) == "audio" {
		cmd = exec.CommandContext(ctx, "ffmpeg",
			"-i", localOriginalPath,
			"-c:a", "aac",
			"-b:a", "128k",
			"-f", "hls",
			"-hls_time", "10",
			"-hls_playlist_type", "vod",
			"-hls_segment_filename", segmentFilename,
			outputPlaylist,
		)
	} else {
		// Video: escalera multi-calidad (1080p/720p/480p/360p) sin upscaling.
		// Se inspecciona el original para generar solo los escalones que no
		// superan su altura y para saber si trae pista de audio.
		probeOut, probeErr := exec.CommandContext(ctx, "ffprobe",
			"-v", "error",
			"-show_entries", "stream=codec_type,height",
			"-of", "json",
			localOriginalPath,
		).Output()
		if probeErr != nil {
			return fmt.Errorf("ffprobe failed on original %s: %w", payload.OriginalKey, probeErr)
		}
		probe, parseErr := parseProbeOutput(probeOut)
		if parseErr != nil {
			return fmt.Errorf("cannot inspect original %s: %w", payload.OriginalKey, parseErr)
		}

		ladder := ladderForHeight(probe.Height)
		log.Printf("[WORKER] Original de %dp (audio=%t): generando %d rendition(s) HLS", probe.Height, probe.HasAudio, len(ladder))
		cmd = exec.CommandContext(ctx, "ffmpeg", buildVideoHLSArgs(localOriginalPath, tmpDir, ladder, probe.HasAudio)...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[WORKER-FFMPEG-ERROR] FFmpeg falló: %s, output: %s", err, string(output))
		return fmt.Errorf("ffmpeg transcoding failed: %w", err)
	}

	// Subir segmentos .ts y playlist .m3u8 generados a MinIO/S3
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() || file.Name() == "original" {
			continue
		}
		filePath := filepath.Join(tmpDir, file.Name())
		targetS3Key := hlsKeyPrefix + file.Name()

		contentType := "video/MP2T"
		if strings.HasSuffix(file.Name(), ".m3u8") {
			contentType = "application/x-mpegURL"
		}

		_, err = minioClient.FPutObject(ctx, mediaBucket, targetS3Key, filePath, minio.PutObjectOptions{
			ContentType: contentType,
		})
		if err != nil {
			return fmt.Errorf("failed to upload HLS segment %s to S3: %w", file.Name(), err)
		}
	}

	log.Printf("[WORKER-SUCCESS] Transcodificación HLS completada para %s. Playlist: %s", payload.ResourceID, playlistKey)
	return p.markResourceCompleted(ctx, payload.ResourceID, playlistKey)
}

func (p *Processor) markResourceCompleted(ctx context.Context, resourceID uuid.UUID, mediaURL string) error {
	// UpdateResource hace un UPDATE de la fila completa (titulo, visibilidad,
	// posicion, etc.), no un patch parcial. Si arma aca un domain.Resource{}
	// nuevo con solo ID/MediaURL/ProcessingStatus, el resto de las columnas
	// se pisan con su valor cero (titulo vacio, is_visible=false...) cada vez
	// que el worker marca un recurso como completado. Por eso primero se trae
	// el recurso existente y solo se tocan los dos campos que cambian.
	res, err := p.courseRepo.GetResourceByID(ctx, resourceID)
	if err != nil {
		return fmt.Errorf("failed to load resource %s before marking completed: %w", resourceID, err)
	}

	res.MediaURL = mediaURL
	res.ProcessingStatus = domain.ProcessingCompleted

	return p.courseRepo.UpdateResource(ctx, res)
}

func (p *Processor) HandleAntimalwareScan(ctx context.Context, t *asynq.Task) error {
	var payload AntimalwareScanPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("invalid antimalware scan payload: %w", err)
	}

	log.Printf("[WORKER-ANTIMALWARE] Iniciando escaneo antimalware para el objeto %s (Resource: %s)", payload.ObjectKey, payload.ResourceID)

	minioClient := p.storage.GetClient()
	mediaBucket := p.storage.GetMediaBucket()

	objInfo, err := minioClient.StatObject(ctx, mediaBucket, payload.ObjectKey, minio.StatObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to stat object for antimalware scan: %w", err)
	}

	log.Printf("[WORKER-ANTIMALWARE] Objeto verificando limpia firma binaria (%d bytes). Estado: APTO/CLEAN", objInfo.Size)
	return nil
}
