package worker

import (
	"strings"
	"testing"
)

func TestLadderForHeight(t *testing.T) {
	tests := []struct {
		name   string
		height int
		want   []string
	}{
		{"1080p completo", 1080, []string{"1080p", "720p", "480p", "360p"}},
		{"mayor a 1080 no sube", 2160, []string{"1080p", "720p", "480p", "360p"}},
		{"720p sin 1080", 720, []string{"720p", "480p", "360p"}},
		{"entre escalones no hace upscaling", 600, []string{"480p", "360p"}},
		{"360p unico", 360, []string{"360p"}},
		{"menor al minimo usa su propia altura", 241, []string{"240p"}},
		{"altura minima", 1, []string{"2p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ladderForHeight(tt.height)
			if len(got) != len(tt.want) {
				t.Fatalf("ladderForHeight(%d) = %v, want names %v", tt.height, got, tt.want)
			}
			for i, r := range got {
				if r.Name != tt.want[i] {
					t.Errorf("escalon %d = %q, want %q", i, r.Name, tt.want[i])
				}
				if r.Height > tt.height && tt.height >= 2 {
					t.Errorf("escalon %q (%dp) supera la altura del original (%d)", r.Name, r.Height, tt.height)
				}
			}
		})
	}
}

func TestParseProbeOutput(t *testing.T) {
	probe, err := parseProbeOutput([]byte(`{"streams":[{"codec_type":"video","height":720},{"codec_type":"audio"}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probe.Height != 720 || !probe.HasAudio {
		t.Errorf("probe = %+v, want Height=720 HasAudio=true", probe)
	}

	probe, err = parseProbeOutput([]byte(`{"streams":[{"codec_type":"video","height":360}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probe.HasAudio {
		t.Errorf("HasAudio = true, want false para un video sin pista de audio")
	}

	if _, err := parseProbeOutput([]byte(`{"streams":[{"codec_type":"audio"}]}`)); err == nil {
		t.Error("esperaba error cuando no hay stream de video")
	}
	if _, err := parseProbeOutput([]byte(`no es json`)); err == nil {
		t.Error("esperaba error con salida que no es JSON")
	}
}

func TestBuildVideoHLSArgs(t *testing.T) {
	ladder := ladderForHeight(720)

	withAudio := strings.Join(buildVideoHLSArgs("in.mp4", "/tmp/out", ladder, true), " ")
	for _, want := range []string{
		"split=3[s0][s1][s2]",
		"[s0]scale=-2:720[v0]",
		"[s2]scale=-2:360[v2]",
		"-var_stream_map v:0,a:0,name:720p v:1,a:1,name:480p v:2,a:2,name:360p",
		"-master_pl_name master.m3u8",
		"-b:v:0 2800k",
		"-b:v:2 800k",
		"-c:a aac",
	} {
		if !strings.Contains(withAudio, want) {
			t.Errorf("args con audio no contienen %q:\n%s", want, withAudio)
		}
	}

	noAudio := strings.Join(buildVideoHLSArgs("in.mp4", "/tmp/out", ladder, false), " ")
	if strings.Contains(noAudio, "0:a:0") || strings.Contains(noAudio, "-c:a") || strings.Contains(noAudio, ",a:0") {
		t.Errorf("args sin audio no deberian mapear audio:\n%s", noAudio)
	}
	if !strings.Contains(noAudio, "-var_stream_map v:0,name:720p v:1,name:480p v:2,name:360p") {
		t.Errorf("var_stream_map sin audio incorrecto:\n%s", noAudio)
	}
}
