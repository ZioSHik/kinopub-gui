package main

import (
	"context"
	"fmt"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/services/mediaresolver"
)

type nopLog struct{}

func (nopLog) Debug(m string, f ...domain.Field) {
	fmt.Printf("DEBUG: %s", m)
	for _, ff := range f {
		fmt.Printf(" %s=%v", ff.Key, ff.Value)
	}
	fmt.Println()
}
func (nopLog) Info(m string, f ...domain.Field)  {}
func (nopLog) Warn(m string, f ...domain.Field)  {}
func (nopLog) Error(m string, f ...domain.Field) {}
func (l nopLog) With(f ...domain.Field) domain.Logger   { return l }
func (l nopLog) Component(s string) domain.Logger       { return l }

func main() {
	auth := domain.RequestAuth{
		Cookie:    "test=1",
		UserAgent: "TestUA",
		Headers:   map[string]string{"Referer": "https://kino.pub/"},
	}

	runOutput := func(_ context.Context, name string, args, _ []string) ([]byte, error) {
		fmt.Printf("Command: %s\n", name)
		for i, a := range args {
			fmt.Printf("  arg[%d]: %q\n", i, a)
		}
		return nil, fmt.Errorf("test stop")
	}

	r := mediaresolver.New(nil, runOutput, nopLog{}, auth)
	ep := domain.Episode{
		Key: domain.EpisodeKey{Series: "test", Season: 1, Episode: 1},
		MediaSources: []domain.MediaSource{
			{Kind: domain.MediaProgressive, URL: "https://cdn.example.com/video.mp4?loc=nl"},
		},
	}
	r.Resolve(context.Background(), ep, "")
}
