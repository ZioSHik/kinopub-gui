package domain

import "errors"

// Sentinel errors for the kinopub downloader.
// Each maps to a specific requirement for traceability.
var (
	ErrInvalidProxyURL        = errors.New("proxy URL is invalid")                        // Req 6.4
	ErrProxyUnsupportedFFmpeg = errors.New("proxy scheme not supported by ffmpeg")         // Req 6.6
	ErrFFmpegNotFound         = errors.New("ffmpeg is required but was not found")          // Req 7.3
	ErrFFmpegFailed           = errors.New("ffmpeg exited with a non-zero status")          // Req 7.4
	ErrEmptyOutput            = errors.New("ffmpeg produced an empty or missing file")      // Req 7.7
	ErrOutputDirUnwritable    = errors.New("output directory cannot be created or written") // Req 11.7
	ErrInvalidFlag            = errors.New("invalid flag value")                            // Req 15.4
	ErrMissingDependency      = errors.New("required component dependency not provided")   // Req 16.5
	ErrAuthRequired           = errors.New("content appears to require authentication")    // Req 17.3, 17.4
)
