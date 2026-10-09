package media

import (
	"strings"
	"testing"
)

func fakeSign(key string) (string, error) { return "https://s3.test/" + key + "?sig=1", nil }

func fakeVariantURL(entry string) string { return "https://api.test/manifest.m3u8?variant=" + entry }

const masterManifest = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=3220800,RESOLUTION=1280x720
720p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=1020800,RESOLUTION=640x360
360p.m3u8
`

const mediaManifest = `#EXTM3U
#EXT-X-TARGETDURATION:6
#EXTINF:6.000000,
720p_000.ts
#EXTINF:2.000000,
720p_001.ts
#EXT-X-ENDLIST
`

func TestRewriteMasterPointsVariantsToAPI(t *testing.T) {
	got, err := rewriteManifestLines(strings.NewReader(masterManifest), "hls/abc", fakeSign, fakeVariantURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := string(got)
	for _, want := range []string{
		"https://api.test/manifest.m3u8?variant=720p.m3u8\n",
		"https://api.test/manifest.m3u8?variant=360p.m3u8\n",
		"#EXT-X-STREAM-INF:BANDWIDTH=3220800,RESOLUTION=1280x720\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("salida no contiene %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "s3.test") {
		t.Errorf("las variantes del maestro no deben firmarse directo contra S3:\n%s", out)
	}
}

func TestRewriteMediaPlaylistSignsSegments(t *testing.T) {
	got, err := rewriteManifestLines(strings.NewReader(mediaManifest), "hls/abc", fakeSign, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := string(got)
	for _, want := range []string{
		"https://s3.test/hls/abc/720p_000.ts?sig=1\n",
		"https://s3.test/hls/abc/720p_001.ts?sig=1\n",
		"#EXT-X-ENDLIST\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("salida no contiene %q:\n%s", want, out)
		}
	}
}

// Manifiestos de un solo nivel (generados antes de la escalera multi-calidad,
// o audio) listan segmentos .ts directo en master.m3u8: deben seguir
// firmándose como antes aunque se pase variantURL.
func TestRewriteLegacySingleLevelMasterStillSignsSegments(t *testing.T) {
	got, err := rewriteManifestLines(strings.NewReader(mediaManifest), "hls/abc", fakeSign, fakeVariantURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "https://s3.test/hls/abc/720p_000.ts?sig=1\n") {
		t.Errorf("segmento .ts de un manifiesto de un nivel debe firmarse:\n%s", got)
	}
}

func TestRewriteKeepsAbsoluteURLs(t *testing.T) {
	in := "#EXTM3U\nhttps://cdn.example.com/x/seg.ts\n"
	got, err := rewriteManifestLines(strings.NewReader(in), "hls/abc", fakeSign, fakeVariantURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != in {
		t.Errorf("got %q, want %q", got, in)
	}
}
