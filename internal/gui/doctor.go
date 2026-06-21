package gui

import (
	"context"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/domain"
	"github.com/niazlv/kinopub-downloader/internal/lib/httpx"
	"github.com/niazlv/kinopub-downloader/internal/services/doctor"
	"github.com/niazlv/kinopub-downloader/internal/services/feedparser"
	"github.com/niazlv/kinopub-downloader/internal/services/inputresolver"
	"github.com/niazlv/kinopub-downloader/internal/services/mediaresolver"
	"github.com/niazlv/kinopub-downloader/internal/services/pagescraper"
	"github.com/niazlv/kinopub-downloader/internal/services/proxyprovider"
)

// DoctorRequest is the body of POST /api/doctor.
type DoctorRequest struct {
	OutputDir string `json:"outputDir"`
	Fix       bool   `json:"fix"`
	CleanTmp  bool   `json:"cleanTmp"`
	SkipProbe bool   `json:"skipProbe"`
	Cookie    string `json:"cookie"`
	Browser   string `json:"browser"`
	UserAgent string `json:"userAgent"`
	Proxy     string `json:"proxy"`
}

// DoctorIssueView is a serialized doctor.Issue.
type DoctorIssueView struct {
	Key         string `json:"key,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Kind        string `json:"kind"`
	Detail      string `json:"detail"`
	StatePath   string `json:"statePath,omitempty"`
	StateBytes  int64  `json:"stateBytes,omitempty"`
	ActualBytes int64  `json:"actualBytes,omitempty"`
}

// DoctorReportView is the serialized doctor.Report plus captured logs.
type DoctorReportView struct {
	StateFile    string            `json:"stateFile"`
	SeriesID     string            `json:"seriesId,omitempty"`
	SeriesTitle  string            `json:"seriesTitle,omitempty"`
	TotalInState int               `json:"totalInState"`
	Healthy      int               `json:"healthy"`
	Skipped      int               `json:"skipped"`
	Fixed        bool              `json:"fixed"`
	HasIssues    bool              `json:"hasIssues"`
	Issues       []DoctorIssueView `json:"issues"`
	Logs         []LogEntry        `json:"logs,omitempty"`
}

func runDoctor(ctx context.Context, req DoctorRequest) (*DoctorReportView, error) {
	cookie, ua, err := resolveAuth(req.Cookie, req.Browser, req.UserAgent)
	if err != nil {
		// Auth failure here isn't fatal — duration probing just gets skipped.
		cookie, ua = "", defaultUserAgent
	}

	logger, capture := newCaptureLogger(domain.VerbosityNormal)

	proxyProv, err := proxyprovider.New(req.Proxy)
	if err != nil {
		return nil, err
	}
	auth := domain.RequestAuth{
		Cookie:    cookie,
		UserAgent: ua,
		Headers:   map[string]string{"Referer": "https://kino.pub/"},
	}
	httpClient := httpx.WithAuth(proxyProv.HTTPClient(), auth)

	var resolverOpts []inputresolver.Option
	if !auth.IsZero() {
		resolverOpts = append(resolverOpts, inputresolver.WithPageScraper(pagescraper.New(httpClient, logger)))
	}
	deps := doctor.Deps{
		Logger:        logger,
		InputResolver: inputresolver.New(logger, resolverOpts...),
		FeedParser:    feedparser.New(httpClient, logger),
		MediaResolver: mediaresolver.New(httpClient, makeRunOutput(), logger, auth),
	}
	opts := doctor.Options{
		OutputDir: req.OutputDir,
		Fix:       req.Fix,
		CleanTmp:  req.CleanTmp,
		SkipProbe: req.SkipProbe,
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	report, err := doctor.Run(ctx, deps, opts)
	if err != nil {
		return nil, err
	}

	view := &DoctorReportView{
		StateFile:    report.StateFile,
		SeriesID:     report.SeriesID,
		SeriesTitle:  report.SeriesTitle,
		TotalInState: report.TotalInState,
		Healthy:      report.Healthy,
		Skipped:      report.Skipped,
		Fixed:        req.Fix,
		HasIssues:    report.HasIssues(),
		Logs:         capture.entries,
	}
	for _, iss := range report.Issues {
		view.Issues = append(view.Issues, DoctorIssueView{
			Key:         iss.Key,
			Season:      iss.Season,
			Episode:     iss.Episode,
			Kind:        iss.Kind.String(),
			Detail:      iss.Detail,
			StatePath:   iss.StatePath,
			StateBytes:  iss.StateBytes,
			ActualBytes: iss.ActualBytes,
		})
	}
	return view, nil
}
