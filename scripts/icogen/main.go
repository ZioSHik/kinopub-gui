// Command icogen packs one or more PNG files into a Windows .ico file.
//
// Usage: icogen -o out.ico in16.png in32.png in48.png in256.png …
//
// Each PNG is stored verbatim as an ICO image entry (Windows Vista and later
// read PNG-encoded icon entries directly), so no re-encoding or color-depth
// conversion is needed. It avoids a dependency on ImageMagick/icoutils so the
// icon can be regenerated anywhere Go runs.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"os"
)

func main() {
	out := flag.String("o", "", "output .ico path")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: icogen -o out.ico in1.png in2.png …")
		os.Exit(2)
	}
	if err := run(*out, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "icogen:", err)
		os.Exit(1)
	}
}

type entry struct {
	w, h int
	data []byte
}

func run(out string, pngs []string) error {
	var entries []entry
	for _, p := range pngs {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		entries = append(entries, entry{w: cfg.Width, h: cfg.Height, data: data})
	}

	var buf bytes.Buffer
	// ICONDIR header: reserved=0, type=1 (icon), count.
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))

	// Image data starts after the 6-byte header and one 16-byte directory entry
	// per image.
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		dim := func(n int) byte {
			if n >= 256 {
				return 0 // 0 means 256 in the ICO format
			}
			return byte(n)
		}
		buf.WriteByte(dim(e.w))         // width
		buf.WriteByte(dim(e.h))         // height
		buf.WriteByte(0)                // palette count (0 = no palette)
		buf.WriteByte(0)                // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // color planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}
