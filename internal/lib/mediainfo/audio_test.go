package mediainfo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAudioNamesRecoversTitlesAndInvalidatesAfterRemux(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe unavailable")
	}
	path := filepath.Join(t.TempDir(), "episode.mkv")
	write := func(title string) {
		t.Helper()
		args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "0.1", "-c:a", "aac", "-metadata:s:a:0", "title=" + title, path}
		if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, out)
		}
	}
	write("02. Дубляж. Невафильм (RUS)")
	want := []string{"02. Дубляж. Невафильм (RUS) [AAC 1ch]"}
	if got := AudioNames(context.Background(), path); !reflect.DeepEqual(got, want) {
		t.Fatalf("audio=%v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := AudioNames(ctx, path); !reflect.DeepEqual(got, want) {
		t.Fatalf("cached audio unavailable: %v", got)
	}
	write("01. Авторский. М. Яроцкий (RUS)")
	if got := AudioNames(context.Background(), path); !reflect.DeepEqual(got, []string{"01. Авторский. М. Яроцкий (RUS) [AAC 1ch]"}) {
		t.Fatalf("stale audio after remux: %v", got)
	}
}

func TestAudioNamesDoesNotInventMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-media.mkv")
	if err := os.WriteFile(path, []byte("not a container"), 0600); err != nil {
		t.Fatal(err)
	}
	if names := AudioNames(context.Background(), path); len(names) != 0 {
		t.Fatalf("invented audio: %v", names)
	}
}

func TestMP4WithoutAudioTitlesPreservesKnownStudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe unavailable")
	}
	path := filepath.Join(t.TempDir(), "episode.mp4")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "0.1", "-c:a", "aac", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	got := EnrichAudioNames(context.Background(), path, []string{"Многоголосый. Studio (RUS)"})
	if !reflect.DeepEqual(got, []string{"Многоголосый. Studio (RUS) [AAC 1ch]"}) {
		t.Fatalf("lost known studio: %v", got)
	}
}
