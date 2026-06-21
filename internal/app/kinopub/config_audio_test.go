package kinopub

import (
	"errors"
	"reflect"
	"testing"

	"github.com/niazlv/kinopub-downloader/internal/domain"
)

func TestParseAudioPreference(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantInclude []string
		wantExclude []string
		wantErr     bool
	}{
		{name: "empty keeps all", in: ""},
		{name: "all keyword keeps all", in: "all"},
		{name: "single include", in: "anilibria", wantInclude: []string{"anilibria"}},
		{name: "exclude bang", in: "!jpn", wantExclude: []string{"jpn"}},
		{name: "exclude dash", in: "-jpn", wantExclude: []string{"jpn"}},
		{
			name:        "mixed",
			in:          "anilibria,!jpn",
			wantInclude: []string{"anilibria"},
			wantExclude: []string{"jpn"},
		},
		{
			name:        "spaces trimmed",
			in:          " anilibria , ! jpn ",
			wantInclude: []string{"anilibria"},
			wantExclude: []string{"jpn"},
		},
		{name: "empty pattern errors", in: "anilibria,!", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAudioPreference(tt.in)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalidFlag) {
					t.Fatalf("expected ErrInvalidFlag, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got.Include, tt.wantInclude) {
				t.Errorf("Include = %v, want %v", got.Include, tt.wantInclude)
			}
			if !reflect.DeepEqual(got.Exclude, tt.wantExclude) {
				t.Errorf("Exclude = %v, want %v", got.Exclude, tt.wantExclude)
			}
		})
	}
}

func TestApplyDefaults_AudioMenuTimeout(t *testing.T) {
	cfg := &domain.RunConfig{AudioMenu: true}
	ApplyDefaults(cfg)
	if cfg.AudioMenuTimeout == 0 {
		t.Fatal("AudioMenuTimeout should default when menu is enabled")
	}

	// Menu off → no default needed.
	cfg2 := &domain.RunConfig{}
	ApplyDefaults(cfg2)
	if cfg2.AudioMenuTimeout != 0 {
		t.Fatalf("AudioMenuTimeout should stay 0 when menu disabled, got %s", cfg2.AudioMenuTimeout)
	}
}
