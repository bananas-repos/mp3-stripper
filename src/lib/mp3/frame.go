package mp3

import (
	"encoding/binary"
	"io"
)

/**
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see http://www.gnu.org/licenses/gpl-3.0.
 *
 * 2026 https://://www.bananas-playground.net/projekt/m3s
 */

// MPEG audio frame header, 4 bytes:
//
//	AAAAAAAA AAABBCCD EEEEFFGH IIJJKLMM
//
//	A  frame sync, 11 bits, all set
//	BB version        CC layer          D  protection (0 = CRC present)
//	EEEE bitrate idx  FF sample rate    G  padding      H private
//	II channel mode   JJ mode extension K  copyright  L original  MM emphasis

// Version is the MPEG audio version, valued as the raw 2-bit header field.
type Version byte

const (
	MPEG25      Version = 0 // 00
	versionBad  Version = 1 // 01, reserved
	MPEG2       Version = 2 // 10
	MPEG1       Version = 3 // 11
	channelMono         = 3 // channel mode 11
)

func (v Version) String() string {
	switch v {
	case MPEG1:
		return "MPEG-1"
	case MPEG2:
		return "MPEG-2"
	case MPEG25:
		return "MPEG-2.5"
	}
	return "invalid"
}

// Bitrates in kbit/s. Index 0 is free format, index 15 is invalid.
var bitrates = [5][16]int{
	// MPEG-1 Layer I
	{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, -1},
	// MPEG-1 Layer II
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, -1},
	// MPEG-1 Layer III
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1},
	// MPEG-2/2.5 Layer I
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, -1},
	// MPEG-2/2.5 Layer II and III
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, -1},
}

var sampleRates = [4][3]int{
	MPEG25: {11025, 12000, 8000},
	MPEG2:  {22050, 24000, 16000},
	MPEG1:  {44100, 48000, 32000},
}

func bitrateRow(v Version, layer int) int {
	if v == MPEG1 {
		return layer - 1
	}
	if layer == 1 {
		return 3
	}
	return 4
}

type frameHeader struct {
	version     Version
	layer       int // 1, 2 or 3
	crc         bool
	bitrate     int // bit/s; 0 means free format
	sampleRate  int
	padding     bool
	channelMode int
	sideInfo    int   // bytes of side information following the header
	length      int64 // total frame length; 0 when free format
}

// parseFrame decodes a frame header from the first 4 bytes of b.
func parseFrame(b []byte) (frameHeader, bool) {
	var h frameHeader
	// The sync word is 11 bits, not 12: the three bits after it encode the
	// version, and MPEG-2.5 encodes as 00, so byte 1 can be 0xEx.
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return h, false
	}
	h.version = Version(b[1] >> 3 & 0x03)
	if h.version == versionBad {
		return h, false
	}
	layerBits := b[1] >> 1 & 0x03
	// Layer index 0 is reserved. Rejecting it is also what rejects ADTS AAC,
	// which shares the 0xFFF sync word but always has layer bits 00.
	if layerBits == 0 {
		return h, false
	}
	h.layer = 4 - int(layerBits)
	h.crc = b[1]&0x01 == 0 // the protection bit is inverted: 0 means CRC present

	brIdx := int(b[2] >> 4 & 0x0F)
	srIdx := int(b[2] >> 2 & 0x03)
	if brIdx == 15 || srIdx == 3 { // both reserved
		return h, false
	}
	h.bitrate = bitrates[bitrateRow(h.version, h.layer)][brIdx] * 1000
	h.sampleRate = sampleRates[h.version][srIdx]
	h.padding = b[2]>>1&0x01 == 1
	h.channelMode = int(b[3] >> 6 & 0x03)
	h.sideInfo = sideInfoSize(h.version, h.channelMode)
	h.length = frameLength(h.version, h.layer, h.bitrate, h.sampleRate, h.padding)
	return h, true
}

// frameLength returns the total frame size in bytes, or 0 if the bitrate is
// free format and the length therefore cannot be derived from the header.
func frameLength(v Version, layer, bitrate, sampleRate int, padding bool) int64 {
	if bitrate == 0 || sampleRate == 0 {
		return 0
	}
	pad := 0
	if padding {
		pad = 1
	}
	if layer == 1 {
		// Layer I counts in 4-byte slots, so padding adds 4 bytes.
		return int64((12*bitrate/sampleRate + pad) * 4)
	}
	// samplesPerFrame/8: 1152/8 = 144 for Layer II and for Layer III on MPEG-1,
	// but Layer III on MPEG-2/2.5 carries 576 samples per frame, so 72.
	coeff := 144
	if layer == 3 && v != MPEG1 {
		coeff = 72
	}
	return int64(coeff*bitrate/sampleRate + pad)
}

func sideInfoSize(v Version, channelMode int) int {
	mono := channelMode == channelMono
	if v == MPEG1 {
		if mono {
			return 17
		}
		return 32
	}
	if mono {
		return 9
	}
	return 17
}

// chainFrames is how many consecutive frames must agree before a sync word is
// believed. Field agreement, not the count, is what kills false positives.
const chainFrames = 3

// chainAt reports whether a frame chain begins at off, reading no further than
// limit. A file with fewer than chainFrames frames still validates.
func chainAt(rd *reader, off, limit int64) (frameHeader, bool) {
	b, err := rd.at(off, 4)
	if err != nil {
		return frameHeader{}, false
	}
	first, ok := parseFrame(b)
	if !ok {
		return frameHeader{}, false
	}
	if first.length == 0 {
		return first, true // free format: nothing to chain to
	}
	cur, pos := first, off
	for i := 1; i < chainFrames; i++ {
		pos += cur.length
		if pos+4 > limit {
			return first, true // ran out of file, not a mismatch
		}
		nb, err := rd.at(pos, 4)
		if err != nil {
			return first, true
		}
		next, ok := parseFrame(nb)
		if !ok {
			return frameHeader{}, false
		}
		// Bitrate and padding vary legitimately in VBR streams; these do not.
		if next.version != first.version || next.layer != first.layer || next.sampleRate != first.sampleRate {
			return frameHeader{}, false
		}
		if next.length == 0 {
			return first, true
		}
		cur = next
	}
	return first, true
}

// maxResync bounds the search for a first frame, so a file of pure noise fails
// with an error instead of being scanned end to end.
const maxResync = 2 << 20

// resync finds the first believable frame in [from, limit).
func resync(rd *reader, from, limit int64) (int64, frameHeader, bool) {
	end := from + maxResync
	if end > limit {
		end = limit
	}
	buf := make([]byte, 64<<10)
	for base := from; base+4 <= end; base += int64(len(buf)) - 3 {
		n := int64(len(buf))
		if base+n > end {
			n = end - base
		}
		chunk := buf[:n]
		if _, err := io.ReadFull(io.NewSectionReader(rd.r, base, n), chunk); err != nil {
			return 0, frameHeader{}, false
		}
		for i := 0; i+4 <= len(chunk); i++ {
			if chunk[i] != 0xFF || chunk[i+1]&0xE0 != 0xE0 {
				continue
			}
			if h, ok := chainAt(rd, base+int64(i), limit); ok {
				return base + int64(i), h, true
			}
		}
	}
	return 0, frameHeader{}, false
}

// vbrTag identifies the VBR header inside the first frame, if any, and reports
// the stream length it declares. This header is deliberately kept, so its
// contents are only ever read for the report.
func vbrTag(rd *reader, frameOff int64, h frameHeader) (name, encoder string, declared int64) {
	// Xing and Info sit immediately after the side information.
	off := frameOff + 4
	if h.crc {
		off += 2
	}
	off += int64(h.sideInfo)
	if b, err := rd.at(off, 4); err == nil {
		switch string(b) {
		case "Xing", "Info":
			return string(b), lameEncoder(rd, off), xingDeclaredBytes(rd, off)
		}
	}
	// VBRI is Fraunhofer's equivalent, always 32 bytes past the header.
	if b, err := rd.at(frameOff+36, 4); err == nil && string(b) == "VBRI" {
		return "VBRI", "", 0
	}
	return "", "", 0
}

// xingDeclaredBytes reads the Xing "Bytes" field, which some taggers filled
// with the whole file size rather than the stream size.
func xingDeclaredBytes(rd *reader, tagOff int64) int64 {
	b, err := rd.at(tagOff+4, 4)
	if err != nil {
		return 0
	}
	flags := binary.BigEndian.Uint32(b)
	off := tagOff + 8
	if flags&0x1 != 0 { // frame count present, skip it
		off += 4
	}
	if flags&0x2 == 0 { // byte count absent
		return 0
	}
	nb, err := rd.at(off, 4)
	if err != nil {
		return 0
	}
	return int64(binary.BigEndian.Uint32(nb))
}

// lameEncoder returns the 9-byte encoder string that LAME and compatible
// encoders write 120 bytes into the Xing header, when it looks like one.
func lameEncoder(rd *reader, tagOff int64) string {
	b, err := rd.at(tagOff+120, 9)
	if err != nil {
		return ""
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7E {
			return ""
		}
	}
	return string(b)
}

// tailAligned walks the frame chain to report whether the audio span ends on a
// frame boundary. Used only for a --verbose warning: the tail is owned by tag
// peeling, never by this walk.
func tailAligned(rd *reader, l *Layout) bool {
	pos := l.AudioStart
	for pos < l.AudioEnd {
		b, err := rd.at(pos, 4)
		if err != nil {
			return false
		}
		h, ok := parseFrame(b)
		if !ok || h.length == 0 {
			return false // free format or garbage: cannot say
		}
		pos += h.length
	}
	return pos == l.AudioEnd
}
