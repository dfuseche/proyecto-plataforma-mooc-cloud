// Comando verify-storage: bitacora/verificacion de integridad de la
// migracion a Cloud Storage (hallazgo de la calificacion de Entrega 2 —
// "el documento admite que los prefijos no validan integridad ni
// referencias SQL").
//
// Recorre cada recurso de curso con media_url en Postgres y confirma
// contra el bucket de medios que el objeto referenciado existe
// realmente, registrando su tamano y ETag (checksum MD5 para objetos
// subidos en una sola parte, que es el caso de este proyecto). No
// compara contra el archivo original porque, al ser una verificacion
// retroactiva, las copias pre-migracion ya no existen -- esa limitacion
// se documenta explicitamente en el reporte en vez de omitirse.
//
// Uso:
//
//	go run ./cmd/verify-storage
//
// Codigo de salida: 0 si todos los objetos referenciados existen,
// 1 si falta alguno (util para un paso de verificacion en CI/CD).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	_ "github.com/lib/pq"
	"github.com/mooc-platform/backend/internal/config"
	"github.com/mooc-platform/backend/internal/media"
)

type resourceRow struct {
	ID               string
	UnitID           string
	Title            string
	MediaURL         string
	ProcessingStatus string
}

// objectKeyFromMediaURL normaliza media_url a una key de objeto valida
// para StatObject. La mayoria de filas guardan ya la key pelada (ver
// internal/media/handler.go: res.MediaURL = req.ObjectKey), pero el
// fallback de internal/course/usecase.go (cuando el llamador no manda
// media_url explicito) persiste "s3://mooc-media/<key>" en su lugar.
// Sin esta normalizacion, esas filas se reportarian como FALTANTE aunque
// el objeto si exista.
func objectKeyFromMediaURL(mediaURL string) string {
	const prefix = "s3://"
	if !strings.HasPrefix(mediaURL, prefix) {
		return mediaURL
	}
	rest := strings.TrimPrefix(mediaURL, prefix)
	// rest tiene forma "<bucket>/<key>"; descartamos el bucket, que no
	// necesariamente coincide con el bucket de medios configurado pero
	// en este proyecto siempre es "mooc-media".
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return mediaURL
	}
	return parts[1]
}

func main() {
	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("no se pudo conectar a PostgreSQL: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		log.Fatalf("PostgreSQL no responde: %v", err)
	}

	storage, err := media.NewStorageService(cfg)
	if err != nil {
		log.Fatalf("no se pudo inicializar el cliente de almacenamiento: %v", err)
	}

	ctx := context.Background()

	rows, err := db.QueryContext(ctx, `
		SELECT id, unit_id, title, media_url, processing_status
		FROM course_resources
		WHERE media_url IS NOT NULL AND media_url <> ''
		ORDER BY created_at
	`)
	if err != nil {
		log.Fatalf("error consultando course_resources: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var resources []resourceRow
	for rows.Next() {
		var r resourceRow
		if err := rows.Scan(&r.ID, &r.UnitID, &r.Title, &r.MediaURL, &r.ProcessingStatus); err != nil {
			log.Fatalf("error leyendo fila: %v", err)
		}
		resources = append(resources, r)
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("error iterando resultados: %v", err)
	}

	_, _ = fmt.Printf("=== Bitacora de verificacion de integridad — Cloud Storage ===\n")
	_, _ = fmt.Printf("Fecha de ejecucion: %s\n", time.Now().Format(time.RFC3339))
	_, _ = fmt.Printf("Bucket de medios: %s\n", storage.GetMediaBucket())
	_, _ = fmt.Printf("Recursos con media_url en course_resources: %d\n\n", len(resources))

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ESTADO\tRECURSO\tOBJECT_KEY\tTAMAÑO (bytes)\tETAG (MD5)\tPROCESSING_STATUS")

	found, missing := 0, 0
	var missingKeys []string

	for _, r := range resources {
		objectKey := objectKeyFromMediaURL(r.MediaURL)
		displayKey := objectKey
		if objectKey != r.MediaURL {
			displayKey = fmt.Sprintf("%s (normalizado de %s)", objectKey, r.MediaURL)
		}

		info, err := storage.StatObject(ctx, objectKey)
		if err != nil {
			missing++
			missingKeys = append(missingKeys, displayKey)
			_, _ = fmt.Fprintf(w, "FALTANTE\t%s (%s)\t%s\t-\t-\t%s\n", r.Title, r.ID, displayKey, r.ProcessingStatus)
			continue
		}
		found++
		_, _ = fmt.Fprintf(w, "OK\t%s (%s)\t%s\t%d\t%s\t%s\n", r.Title, r.ID, displayKey, info.Size, info.ETag, r.ProcessingStatus)
	}
	_ = w.Flush()

	_, _ = fmt.Printf("\n=== Resumen ===\n")
	_, _ = fmt.Printf("Encontrados: %d/%d\n", found, len(resources))
	_, _ = fmt.Printf("Faltantes: %d/%d\n", missing, len(resources))
	if missing > 0 {
		_, _ = fmt.Printf("\nObject keys faltantes:\n")
		for _, k := range missingKeys {
			_, _ = fmt.Printf("  - %s\n", k)
		}
	}

	_, _ = fmt.Printf("\n=== Limitaciones de esta verificacion ===\n")
	_, _ = fmt.Printf("- Confirma EXISTENCIA y tamano/ETag reportados por el propio Cloud\n")
	_, _ = fmt.Printf("  Storage, no una comparacion byte a byte contra el archivo original\n")
	_, _ = fmt.Printf("  pre-migracion (esas copias ya no existen; es una verificacion\n")
	_, _ = fmt.Printf("  retroactiva, no se hizo en el momento de la migracion).\n")
	_, _ = fmt.Printf("- Para manifiestos HLS (media_url tipo hls/<id>/master.m3u8), solo se\n")
	_, _ = fmt.Printf("  verifica el manifiesto en si, no cada segmento .ts individual que\n")
	_, _ = fmt.Printf("  referencia.\n")
	_, _ = fmt.Printf("- Los badges (tabla badges, columna image_url) quedan FUERA de este\n")
	_, _ = fmt.Printf("  reporte: no existe código que suba una imagen real al bucket de\n")
	_, _ = fmt.Printf("  badges (BadgeBucket se configura pero nunca se usa con PutObject),\n")
	_, _ = fmt.Printf("  asi que no hay nada que verificar aun — es un hallazgo aparte,\n")
	_, _ = fmt.Printf("  registrado en el inventario de deuda tecnica de Entrega 3.\n")
	_, _ = fmt.Printf("- Algunas filas de media_url guardan \"s3://mooc-media/<key>\" en vez\n")
	_, _ = fmt.Printf("  de la key pelada (fallback de internal/course/usecase.go); este\n")
	_, _ = fmt.Printf("  reporte las normaliza antes de verificar y lo indica como\n")
	_, _ = fmt.Printf("  \"(normalizado de ...)\" junto a la key real usada.\n")

	if missing > 0 {
		os.Exit(1)
	}
}
