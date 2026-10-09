package media

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
)

// rewriteManifestLines recorre un manifiesto HLS y reescribe cada línea de
// datos (las que no son comentario/etiqueta ni están vacías):
//
//   - URL absoluta ("://"): se deja igual.
//   - Sub-playlist ".m3u8" cuando variantURL != nil (manifiesto maestro de
//     una escalera multi-calidad): se reemplaza por variantURL(entrada), que
//     apunta de vuelta a nuestro endpoint para que ESA playlist también se
//     reescriba al vuelo. Un presigned URL de S3 devolvería la sub-playlist
//     cruda, con sus segmentos relativos sin firmar (403 en un bucket privado).
//   - Cualquier otra (segmento .ts, o .m3u8 con variantURL == nil): se firma
//     con sign(<baseDir>/<entrada>).
func rewriteManifestLines(
	r io.Reader,
	baseDir string,
	sign func(objectKey string) (string, error),
	variantURL func(entry string) string,
) ([]byte, error) {
	var out bytes.Buffer
	scanner := bufio.NewScanner(r)
	// Algunas líneas de atributos HLS (#EXT-X-STREAM-INF, etc.) pueden ser
	// largas; ampliamos el buffer por seguridad.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "", strings.HasPrefix(trimmed, "#"), strings.Contains(trimmed, "://"):
			out.WriteString(line)
		case variantURL != nil && strings.HasSuffix(trimmed, ".m3u8"):
			out.WriteString(variantURL(trimmed))
		default:
			signedURL, err := sign(path.Join(baseDir, trimmed))
			if err != nil {
				return nil, fmt.Errorf("failed to sign manifest entry %q: %w", trimmed, err)
			}
			out.WriteString(signedURL)
		}
		out.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read HLS manifest: %w", err)
	}

	return out.Bytes(), nil
}
