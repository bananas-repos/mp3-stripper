package mp3

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
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

/**
 * Detectors for every metadata container that can appear in an MP3 file.
 *
 * Each one anchors on a magic string and then corroborates it before claiming
 * a match. Corroboration is not optional: "TAG" and "3DI" are three bytes and
 * "LYRICS200" occurs inside real APE item values, so an uncorroborated match
 * would silently chop the end off the audio.
 */

/**
 * synchsafe decodes a 28-bit synchsafe integer. Every byte must have its high
 * bit clear; some old taggers wrote a plain big-endian uint32 instead, and
 * honouring such a size would land us inside the audio.
 */
func synchsafe(b []byte) (int64, bool) {
	var v int64
	for _, c := range b[:4] {
		if c&0x80 != 0 {
			return 0, false
		}
		v = v<<7 | int64(c&0x7F)
	}
	return v, true
}

// the id3v2 info struct
type id3v2Info struct {
	major, minor, flags byte
	size                int64 // declared: excludes the 10-byte header and any footer
	total               int64 // full on-disk size
}

/**
 * name returns the correct id
 */
func (i id3v2Info) name() string {
	return fmt.Sprintf("ID3v2.%d", i.major)
}

/**
 * id3v2At decodes an ID3v2 header at off. The caller bounds-checks total.
 */
func id3v2At(rd *reader, off int64) (id3v2Info, bool) {
	b, err := rd.at(off, 10)
	if err != nil || string(b[:3]) != "ID3" {
		return id3v2Info{}, false
	}
	major := b[3]
	if major == 0xFF || major > 4 { // 0xFF is invalid per spec, >4 unknown
		return id3v2Info{}, false
	}
	size, ok := synchsafe(b[6:10])
	if !ok {
		return id3v2Info{}, false
	}
	info := id3v2Info{major: major, minor: b[4], flags: b[5], size: size, total: 10 + size}
	// Bit 4 means "footer present" in ID3v2.4 only. It is undefined in v2.3 and
	// v2.2, where taggers do set junk - trusting it there would eat 10 bytes of
	// the first audio frame, corrupting the Xing header we promise to keep.
	if major == 4 && info.flags&0x10 != 0 {
		info.total += 10
	}
	return info, true
}

/**
 * id3v2AppendedAt matches an ID3v2.4 tag appended at the end of the file, whose
 * 10-byte footer ends at cursor.
 */
func id3v2AppendedAt(rd *reader, cursor, floor int64, _ Kind) (Region, bool, error) {
	f, err := rd.at(cursor-10, 10)
	if err != nil || string(f[:3]) != "3DI" {
		return Region{}, false, nil
	}
	size, ok := synchsafe(f[6:10])
	if !ok {
		return Region{}, false, nil
	}
	total := size + 20 // header + payload + footer, exactly; a footer forbids padding
	if total > cursor-floor {
		return Region{}, false, nil
	}
	start := cursor - total
	// "3DI" is only three bytes, so require the matching header: same declared
	// size, and the footer flag actually set.
	h, ok := id3v2At(rd, start)
	if !ok || h.major != 4 || h.size != size || h.flags&0x10 == 0 {
		return Region{}, false, nil
	}
	return Region{
		Kind:   KindID3v2Appended,
		Name:   h.name() + " (appended)",
		Offset: start,
		Size:   total,
		Detail: id3v2Detail(rd, start, h),
	}, true, nil
}

/**
 * id3v1At matches the final 128-byte ID3v1 block ending at cursor.
 */
func id3v1At(rd *reader, cursor, floor int64, _ Kind) (Region, bool, error) {
	if cursor-floor < 128 {
		return Region{}, false, nil
	}
	b, err := rd.at(cursor-128, 128)
	if err != nil || string(b[:3]) != "TAG" {
		return Region{}, false, nil
	}
	// Three bytes is the weakest anchor here, so corroborate with the year
	// field, which must be digits, spaces or NULs.
	for _, c := range b[93:97] {
		if !(c >= '0' && c <= '9') && c != ' ' && c != 0 {
			return Region{}, false, nil
		}
	}
	name, detail := "ID3v1", ""
	if b[125] == 0 && b[126] != 0 {
		name, detail = "ID3v1.1", fmt.Sprintf("track %d", b[126])
	}
	return Region{Kind: KindID3v1, Name: name, Offset: cursor - 128, Size: 128, Detail: detail}, true, nil
}

/**
 * id3v1ExtendedAt matches the 227-byte "enhanced" TAG+ block. It is only
 * meaningful directly in front of an ID3v1 tag, which is also the only way to
 * tell a real one from a four-byte coincidence.
 */
func id3v1ExtendedAt(rd *reader, cursor, floor int64, prev Kind) (Region, bool, error) {
	if prev != KindID3v1 || cursor-floor < 227 {
		return Region{}, false, nil
	}
	b, err := rd.at(cursor-227, 4)
	if err != nil || string(b) != "TAG+" {
		return Region{}, false, nil
	}
	return Region{Kind: KindID3v1Extended, Name: "ID3v1 (extended)", Offset: cursor - 227, Size: 227}, true, nil
}

// The apeInfo struct
type apeInfo struct {
	version   int64
	tagSize   int64 // includes the footer, excludes the header
	items     int64
	hasHeader bool
}

/**
 * apeAt matches an APE tag whose 32-byte footer ends at cursor. Unlike the
 * weaker anchors, an eight-byte magic with a valid version is trusted enough
 * that a bad size is reported as corruption rather than shrugged off.
 */
func apeAt(rd *reader, cursor, floor int64, _ Kind) (Region, bool, error) {
	f, err := rd.at(cursor-32, 32)
	if err != nil || string(f[:8]) != "APETAGEX" {
		return Region{}, false, nil
	}
	a := apeInfo{
		version: int64(binary.LittleEndian.Uint32(f[8:12])),
		tagSize: int64(binary.LittleEndian.Uint32(f[12:16])),
		items:   int64(binary.LittleEndian.Uint32(f[16:20])),
	}
	flags := binary.LittleEndian.Uint32(f[20:24])
	if a.version != 1000 && a.version != 2000 {
		return Region{}, false, fmt.Errorf("%w: APE tag version %d", ErrMalformed, a.version)
	}
	// tagSize covers the items plus this footer, so 32 is the floor and a
	// footer-only tag with no items is legal.
	if a.tagSize < 32 {
		return Region{}, false, fmt.Errorf("%w: APE tag size %d", ErrMalformed, a.tagSize)
	}
	if a.version >= 2000 {
		if flags&0x20000000 != 0 { // bit 29: this is a header, not a footer
			return Region{}, false, fmt.Errorf("%w: APE header where a footer belongs", ErrMalformed)
		}
		if flags&0x40000000 != 0 { // bit 30: claims to have no footer
			return Region{}, false, fmt.Errorf("%w: APE tag denies having the footer just read", ErrMalformed)
		}
		a.hasHeader = flags&0x80000000 != 0 // bit 31
	}
	// APEv1 leaves the flags undefined and real files put garbage there, so the
	// header flag is only read for v2.
	total := a.tagSize
	if a.hasHeader {
		total += 32
	}
	if total > cursor-floor {
		return Region{}, false, fmt.Errorf("%w: APE tag of %d bytes overruns the audio", ErrMalformed, total)
	}
	name := "APEv2"
	if a.version == 1000 {
		name = "APEv1"
	}
	return Region{
		Kind:   KindAPE,
		Name:   name,
		Offset: cursor - total,
		Size:   total,
		Detail: fmt.Sprintf("%d items", a.items),
	}, true, nil
}

/**
 * apeHeaderAt matches an APE tag placed at the front of the file, identified by
 * the "this is a header" flag.
 */
func apeHeaderAt(rd *reader, off int64) (Region, bool, error) {
	h, err := rd.at(off, 32)
	if err != nil || string(h[:8]) != "APETAGEX" {
		return Region{}, false, nil
	}
	version := int64(binary.LittleEndian.Uint32(h[8:12]))
	tagSize := int64(binary.LittleEndian.Uint32(h[12:16]))
	items := int64(binary.LittleEndian.Uint32(h[16:20]))
	flags := binary.LittleEndian.Uint32(h[20:24])
	if version != 2000 || flags&0x20000000 == 0 || tagSize < 32 {
		return Region{}, false, nil
	}
	// tagSize covers the items and the trailing footer; the header adds 32.
	total := 32 + tagSize
	if total > rd.size-off {
		return Region{}, false, fmt.Errorf("%w: leading APE tag of %d bytes overruns the file", ErrMalformed, total)
	}
	return Region{
		Kind:   KindAPE,
		Name:   "APEv2 (leading)",
		Offset: off,
		Size:   total,
		Detail: fmt.Sprintf("%d items", items),
	}, true, nil
}

/**
 * lyrics3v2At matches a Lyrics3v2 block ending at cursor: six ASCII digits of
 * size followed by "LYRICS200".
 */
func lyrics3v2At(rd *reader, cursor, floor int64, _ Kind) (Region, bool, error) {
	b, err := rd.at(cursor-15, 15)
	if err != nil || string(b[6:]) != "LYRICS200" {
		return Region{}, false, nil
	}
	var size int64
	for _, c := range b[:6] {
		if c < '0' || c > '9' {
			return Region{}, false, nil
		}
		size = size*10 + int64(c-'0')
	}
	total := size + 15 // the size field excludes itself and the magic
	if total > cursor-floor {
		return Region{}, false, nil
	}
	start := cursor - total
	// "LYRICS200" turns up inside APE item values, so the block must actually
	// begin where the size says it does.
	if s, err := rd.at(start, 11); err != nil || string(s) != "LYRICSBEGIN" {
		return Region{}, false, nil
	}
	return Region{Kind: KindLyrics3v2, Name: "Lyrics3v2", Offset: start, Size: total}, true, nil
}

/**
 * maxLyrics3v1 bounds the backward search: the lyrics field itself is capped at
 * 5100 bytes, plus the two magic strings.
 */
const maxLyrics3v1 = 5100 + 11 + 9

/**
 * lyrics3v1At matches a Lyrics3v1 block ending at cursor. It has no size field,
 * so the start has to be searched for.
 */
func lyrics3v1At(rd *reader, cursor, floor int64, _ Kind) (Region, bool, error) {
	if b, err := rd.at(cursor-9, 9); err != nil || string(b) != "LYRICSEND" {
		return Region{}, false, nil
	}
	lo := cursor - maxLyrics3v1
	if lo < floor {
		lo = floor
	}
	n := cursor - lo
	if n < 20 {
		return Region{}, false, nil
	}
	buf := make([]byte, n) // ceiling is a constant, never a file-supplied size
	if _, err := io.ReadFull(io.NewSectionReader(rd.r, lo, n), buf); err != nil {
		return Region{}, false, nil
	}
	// Take the highest-offset match: a decoy "LYRICSBEGIN" inside the lyrics
	// text would otherwise make us strip more than the block, i.e. eat audio.
	i := bytes.LastIndex(buf, []byte("LYRICSBEGIN"))
	if i < 0 {
		return Region{}, false, nil
	}
	start := lo + int64(i)
	return Region{Kind: KindLyrics3v1, Name: "Lyrics3v1", Offset: start, Size: cursor - start}, true, nil
}

/**
 * id3v2Detail lists frame IDs for the verbose report. Nothing here affects the
 * byte ranges, so giving up early is harmless.
 */
func id3v2Detail(rd *reader, off int64, info id3v2Info) string {
	if info.flags&0x80 != 0 {
		// Unsynchronisation rewrites the payload, so frame offsets inside it
		// cannot be walked. The declared size still covers the stored bytes.
		return "unsynchronised"
	}
	idLen, hdrLen := 4, 10
	if info.major == 2 {
		idLen, hdrLen = 3, 6
	}
	pos, end := off+10, off+10+info.size
	if info.major >= 3 && info.flags&0x40 != 0 {
		eb, err := rd.at(pos, 4)
		if err != nil {
			return ""
		}
		if info.major == 4 {
			n, ok := synchsafe(eb) // v2.4: the size includes itself
			if !ok {
				return ""
			}
			pos += n
		} else {
			pos += 4 + int64(binary.BigEndian.Uint32(eb)) // v2.3: it does not
		}
	}
	var ids []string
	for len(ids) < 24 && pos+int64(hdrLen) <= end {
		hb, err := rd.at(pos, hdrLen)
		if err != nil {
			break
		}
		id := string(hb[:idLen])
		if !validFrameID(id) {
			break // padding or garbage
		}
		size, ok := id3v2FrameSize(hb, info.major)
		if !ok || size < 0 || size > end-pos {
			break
		}
		if size > 8<<10 {
			ids = append(ids, fmt.Sprintf("%s(%s)", id, byteCountSI(size)))
		} else {
			ids = append(ids, id)
		}
		pos += int64(hdrLen) + size
	}
	if len(ids) == 0 {
		return ""
	}
	return strings.Join(ids, " ")
}

/**
 * id3v2FrameSize
 */
func id3v2FrameSize(hb []byte, major byte) (int64, bool) {
	switch major {
	case 2:
		return int64(hb[3])<<16 | int64(hb[4])<<8 | int64(hb[5]), true
	case 4:
		return synchsafe(hb[4:8])
	default:
		return int64(binary.BigEndian.Uint32(hb[4:8])), true
	}
}

/**
 * validFrameID checks for a valid frame id
 */
func validFrameID(id string) bool {
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
