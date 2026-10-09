package media

import (
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
// sub-playlist .m3u8) reemplazada por una URL absoluta (ver
// rewriteManifestLines).
//
// Esto es necesario porque un presigned URL solo autentica UN objeto: el
// manifiesto en sí. ffmpeg genera referencias relativas a los segmentos
// (p.ej. "720p_000.ts"), y como el bucket de medios es privado, un
// reproductor HLS no puede resolverlas sin firma propia.
//
// variantURL (opcional) construye la URL con la que se sirve cada
// sub-playlist de un manifiesto maestro multi-calidad; con nil, toda
// referencia (incluidas las .m3u8) se firma directo contra S3.
func (s *StorageService) RewriteHLSManifest(ctx context.Context, manifestKey string, variantURL func(entry string) string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.mediaBucket, manifestKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to open HLS manifest %q: %w", manifestKey, err)
	}
	defer func() { _ = obj.Close() }()

	// Los segmentos y sub-playlists se guardan junto al manifiesto bajo el
	// mismo prefijo (hls/<resourceID>/...), así que las rutas relativas del
	// archivo se resuelven contra el directorio del manifiesto.
	sign := func(entryKey string) (string, error) {
		return s.GeneratePresignedDownloadURL(ctx, entryKey, manifestSegmentURLExpiry)
	}
	body, err := rewriteManifestLines(obj, path.Dir(manifestKey), sign, variantURL)
	if err != nil {
		return nil, fmt.Errorf("failed to rewrite HLS manifest %q: %w", manifestKey, err)
	}
	return body, nil
}
