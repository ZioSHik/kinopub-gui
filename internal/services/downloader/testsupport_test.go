package downloader

import (
	"bytes"
	"io"
	"strconv"
	"time"
)

var testModTime = time.Unix(0, 0)

func itoa(n int) string { return strconv.Itoa(n) }

// bytesReadSeeker wraps a byte slice into an io.ReadSeeker for http.ServeContent.
func bytesReadSeeker(b []byte) io.ReadSeeker { return bytes.NewReader(b) }
