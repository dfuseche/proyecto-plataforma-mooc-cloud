package worker

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Rendition es un escalon de la escalera HLS adaptativa (una calidad).
type Rendition struct {
	Name             string // sufijo de archivo y de variante: "720p"
	Height           int
	VideoBitrateKbps int
}

// defaultLadder es la escalera completa, de mayor a menor calidad. Solo se
// generan los escalones cuya altura no supera la del archivo original (sin
// upscaling): ver ladderForHeight.
var defaultLadder = []Rendition{
	{Name: "1080p", Height: 1080, VideoBitrateKbps: 5000},
	{Name: "720p", Height: 720, VideoBitrateKbps: 2800},
	{Name: "480p", Height: 480, VideoBitrateKbps: 1400},
	{Name: "360p", Height: 360, VideoBitrateKbps: 800},
}

// hlsSegmentSeconds es la duracion objetivo de cada segmento .ts; tambien
// fija el intervalo de keyframes para que los segmentos queden alineados
// entre calidades (requisito para poder cambiar de calidad en vivo).
const hlsSegmentSeconds = 6

// ladderForHeight devuelve los escalones de defaultLadder con altura <= la
// del original. Si el original es mas chico que el escalon mas bajo (p. ej.
// 240p) devuelve un unico escalon a la altura del original, nunca vacio y
// nunca mayor que el original.
func ladderForHeight(sourceHeight int) []Rendition {
	var ladder []Rendition
	for _, r := range defaultLadder {
		if r.Height <= sourceHeight {
			ladder = append(ladder, r)
		}
	}
	if len(ladder) > 0 {
		return ladder
	}

	h := sourceHeight &^ 1 // altura par: libx264 con yuv420p lo exige
	if h < 2 {
		h = 2
	}
	return []Rendition{{Name: fmt.Sprintf("%dp", h), Height: h, VideoBitrateKbps: 400}}
}

// mediaProbe es lo minimo que se necesita saber del original para armar el
// comando de ffmpeg.
type mediaProbe struct {
	Height   int
	HasAudio bool
}

// parseProbeOutput interpreta la salida JSON de
// `ffprobe -v error -show_entries stream=codec_type,height -of json <archivo>`.
func parseProbeOutput(out []byte) (mediaProbe, error) {
	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return mediaProbe{}, fmt.Errorf("invalid ffprobe output: %w", err)
	}

	var probe mediaProbe
	for _, s := range parsed.Streams {
		switch s.CodecType {
		case "video":
			if probe.Height == 0 {
				probe.Height = s.Height
			}
		case "audio":
			probe.HasAudio = true
		}
	}
	if probe.Height <= 0 {
		return mediaProbe{}, fmt.Errorf("no video stream with a valid height found")
	}
	return probe, nil
}

// buildVideoHLSArgs arma los argumentos de ffmpeg que generan, en UNA sola
// pasada de decodificacion, master.m3u8 + una playlist y sus segmentos por
// cada escalon del ladder. Salida plana en outDir (sin subdirectorios):
//
//	master.m3u8, <nombre>.m3u8, <nombre>_000.ts, ...
func buildVideoHLSArgs(input, outDir string, ladder []Rendition, hasAudio bool) []string {
	n := len(ladder)

	splitLabels := make([]string, n)
	var scales []string
	for i, r := range ladder {
		splitLabels[i] = fmt.Sprintf("[s%d]", i)
		scales = append(scales, fmt.Sprintf("[s%d]scale=-2:%d[v%d]", i, r.Height, i))
	}
	filter := fmt.Sprintf("[0:v:0]split=%d%s;%s", n, strings.Join(splitLabels, ""), strings.Join(scales, ";"))

	args := []string{"-y", "-i", input, "-filter_complex", filter}

	var variantMap []string
	for i, r := range ladder {
		args = append(args, "-map", fmt.Sprintf("[v%d]", i))
		entry := fmt.Sprintf("v:%d", i)
		if hasAudio {
			args = append(args, "-map", "0:a:0")
			entry += fmt.Sprintf(",a:%d", i)
		}
		variantMap = append(variantMap, entry+",name:"+r.Name)
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-sc_threshold", "0",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentSeconds),
	)
	for i, r := range ladder {
		args = append(args,
			fmt.Sprintf("-b:v:%d", i), fmt.Sprintf("%dk", r.VideoBitrateKbps),
			fmt.Sprintf("-maxrate:v:%d", i), fmt.Sprintf("%dk", r.VideoBitrateKbps*107/100),
			fmt.Sprintf("-bufsize:v:%d", i), fmt.Sprintf("%dk", r.VideoBitrateKbps*3/2),
		)
	}
	if hasAudio {
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}

	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", hlsSegmentSeconds),
		"-hls_playlist_type", "vod",
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", strings.Join(variantMap, " "),
		"-hls_segment_filename", filepath.Join(outDir, "%v_%03d.ts"),
		filepath.Join(outDir, "%v.m3u8"),
	)
	return args
}
