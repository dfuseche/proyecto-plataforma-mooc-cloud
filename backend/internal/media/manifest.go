package media

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// manifestSegmentURLExpiry es cuánto tiempo quedan vigentes las URLs firmadas
// que se inyectan en cada línea del manifiesto (segmentos .ts o sub-playlists).
const manifestSegmentURLExpiry = 2 * time.Hour

// IsHLSManifestKey indica si un object key corresponde a un manifiesto HLS
// (y no al archivo original subido por el usuario).
func IsHLSManifestKey(objectKey string) bool {
	return strings.HasSuffix(objectKey, ".m3u8")
}

// RewriteHLSManifest descarga el manifiesto HLS ubicado en manifestKey y
// devuelve su contenido con cada referencia relativa (segmento .ts o
// sub-playlist .m3u8) reemplazada por una URL prefirmada de S3/MinIO.
//
// Esto es necesario porque un presigned URL solo autentica UN objeto: el
// manifiesto en sí. ffmpeg genera referencias relativas a los segmentos
// (p.ej. "segment_000.ts"), y como el bucket de medios es privado, un
// reproductor HLS no puede resolverlas sin firma propia. Reescribimos el
// manifiesto para que cada línea de datos sea ya una URL absoluta y firmada.
func (s *StorageService) RewriteHLSManifest(ctx context.Context, manifestKey string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.mediaBucket, manifestKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to open HLS manifest %q: %w", manifestKey, err)
	}
	defer func() { _ = obj.Close() }()

	// Los segmentos y sub-playlists se guardan junto al manifiesto bajo el
	// mismo prefijo (hls/<resourceID>/...), así que las rutas relativas del
	// archivo se resuelven contra el directorio del manifiesto.
	baseDir := path.Dir(manifestKey)

	var out bytes.Buffer
	scanner := bufio.NewScanner(obj)
	// Algunas líneas de atributos HLS (#EXT-X-STREAM-INF, etc.) pueden ser
	// largas; ampliamos el buffer por seguridad.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out.WriteString(line)
			out.WriteString("\n")
			continue
		}

		if strings.Contains(trimmed, "://") {
			// Ya es una URL absoluta (manifiesto escrito a mano, CDN externo, etc.).
			out.WriteString(line)
			out.WriteString("\n")
			continue
		}

		entryKey := path.Join(baseDir, trimmed)
		signedURL, err := s.GeneratePresignedDownloadURL(ctx, entryKey, manifestSegmentURLExpiry)
		if err != nil {
			return nil, fmt.Errorf("failed to sign manifest entry %q: %w", trimmed, err)
		}
		out.WriteString(signedURL)
		out.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read HLS manifest %q: %w", manifestKey, err)
	}

	return out.Bytes(), nil
}
