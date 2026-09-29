package mp3

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
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

var (
	ErrNotMP3     = errors.New("not an MPEG audio file")
	ErrNoFrames   = errors.New("no valid MPEG audio frame found")
	ErrMalformed  = errors.New("malformed tag structure")
	ErrEmptyAudio = errors.New("no audio left after removing metadata")
	ErrShortRead = errors.New("read past end of file")
)

type reader struct {
	r    io.ReaderAt
	size int64
}

// at reads n bytes at off. n is always a small compile-time constant: no read
// in this package is ever sized from a value found in the file, so a hostile
// size field cannot drive an allocation.
func (rd *reader) at(off int64, n int) ([]byte, error) {
	if off < 0 || n < 0 || off > rd.size-int64(n) {
		return nil, ErrShortRead
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(rd.r, off, int64(n)), b); err != nil {
		return nil, err
	}
	return b, nil
}

// Kind identifies what a Region is.
type Kind int

const (
	KindAudio Kind = iota
	KindID3v2
	KindID3v2Appended
	KindID3v1
	KindID3v1Extended
	KindAPE
	KindLyrics3v2
	KindLyrics3v1
	KindLeadingJunk
)

// Region is a span of the input file.
type Region struct {
	Kind   Kind
	Name   string // display name, e.g. "ID3v2.4" or "APEv2"
	Offset int64
	Size   int64
	Detail string // extra description for the verbose report
}

// AudioInfo describes the stream, read from its first frame.
type AudioInfo struct {
	Version    Version
	Layer      int
	SampleRate int
	Bitrate    int    // of the first frame; varies in VBR streams
	VBRTag     string // "Xing", "Info", "VBRI" or ""
	Encoder    string // encoder string from a LAME-style Xing header, if any
	Declared   int64  // stream length the Xing header claims, 0 if absent
}

// Layout is the byte-level map of an MP3 file: everything to strip, and the one
// contiguous span to keep.
type Layout struct {
	FileSize   int64
	AudioStart int64
	AudioEnd   int64 // exclusive
	Stripped   []Region
	Audio      AudioInfo
}

func (l *Layout) AudioSize() int64 { return l.AudioEnd - l.AudioStart }

func (l *Layout) StrippedSize() int64 {
	var n int64
	for _, r := range l.Stripped {
		n += r.Size
	}
	return n
}

// maxPeel caps the peel loops. No real file stacks more than a handful of tags,
// and a cap turns a detector bug into an error instead of an endless loop.
const maxPeel = 32

// Scan maps the file without loading it: only small fixed windows are read.
func Scan(r io.ReaderAt, size int64) (*Layout, error) {
	if size == 0 {
		return nil, fmt.Errorf("%w: file is empty", ErrNotMP3)
	}
	rd := &reader{r: r, size: size}
	if err := sniff(rd); err != nil {
		return nil, err
	}

	l := &Layout{FileSize: size}
	front, err := peelFront(rd, l)
	if err != nil {
		return nil, err
	}
	tail, err := peelTail(rd, l, front)
	if err != nil {
		return nil, err
	}
	if front > tail {
		return nil, fmt.Errorf("%w: leading tags end at %d, past the trailing tags at %d",
			ErrMalformed, front, tail)
	}

	start, h, err := findAudio(rd, l, front, tail)
	if err != nil {
		return nil, err
	}
	l.AudioStart, l.AudioEnd = start, tail
	if l.AudioStart >= l.AudioEnd {
		return nil, ErrEmptyAudio
	}

	name, encoder, declared := vbrTag(rd, start, h)
	l.Audio = AudioInfo{
		Version:    h.version,
		Layer:      h.layer,
		SampleRate: h.sampleRate,
		Bitrate:    h.bitrate,
		VBRTag:     name,
		Encoder:    encoder,
		Declared:   declared,
	}
	sort.Slice(l.Stripped, func(i, j int) bool { return l.Stripped[i].Offset < l.Stripped[j].Offset })
	return l, nil
}

// foreignContainers are formats that can carry MPEG audio inside them. A
// RIFF/WAVE wrapper in particular holds real frames, so without this check the
// sync scan would happily emit a headerless stream and call it clean.
var foreignContainers = []struct{ magic, name string }{
	{"RIFF", "RIFF/WAVE"},
	{"FORM", "AIFF"},
	{"fLaC", "FLAC"},
	{"OggS", "Ogg"},
	{"\x1aE\xdf\xa3", "Matroska"},
}

func sniff(rd *reader) error {
	b, err := rd.at(0, 4)
	if err != nil {
		return fmt.Errorf("%w: only %d bytes long", ErrNotMP3, rd.size)
	}
	for _, c := range foreignContainers {
		if string(b) == c.magic {
			return fmt.Errorf("%w: looks like %s", ErrNotMP3, c.name)
		}
	}
	if b, err := rd.at(4, 4); err == nil && string(b) == "ftyp" {
		return fmt.Errorf("%w: looks like MP4/M4A", ErrNotMP3)
	}
	return nil
}

// peelFront skips leading tags and returns the offset just past them.
func peelFront(rd *reader, l *Layout) (int64, error) {
	off := int64(0)
	for i := 0; ; i++ {
		if i >= maxPeel {
			return 0, fmt.Errorf("%w: more than %d stacked leading tags", ErrMalformed, maxPeel)
		}
		// Stacked ID3v2 tags are legal and do occur; keep taking them.
		if info, ok := id3v2At(rd, off); ok {
			if info.total > rd.size-off {
				return 0, fmt.Errorf("%w: %s declares %d bytes but only %d remain",
					ErrMalformed, info.name(), info.total, rd.size-off)
			}
			l.Stripped = append(l.Stripped, Region{
				Kind:   KindID3v2,
				Name:   info.name(),
				Offset: off,
				Size:   info.total,
				Detail: id3v2Detail(rd, off, info),
			})
			off += info.total
			continue
		}
		region, ok, err := apeHeaderAt(rd, off)
		if err != nil {
			return 0, err
		}
		if ok {
			l.Stripped = append(l.Stripped, region)
			off += region.Size
			continue
		}
		return off, nil
	}
}

// tailDetectors run in order of anchor confidence, not in the order the specs
// lay the containers out: when two would match at the same cursor, a
// corroborated eight-byte magic must beat a bare three-byte one.
var tailDetectors = []func(rd *reader, cursor, floor int64, prev Kind) (Region, bool, error){
	apeAt,           // "APETAGEX", version and flags validated
	lyrics3v2At,     // "LYRICS200", digits plus "LYRICSBEGIN" at the computed start
	id3v2AppendedAt, // "3DI", cross-checked against the matching header
	id3v1ExtendedAt, // "TAG+", only valid in front of an ID3v1 tag
	id3v1At,         // "TAG", corroborated by the year field
	lyrics3v1At,     // "LYRICSEND", start found by bounded backward search
}

// peelTail strips trailing containers, working down from the end of the file.
// It retries until nothing matches rather than assuming a fixed order, because
// real files stack these inconsistently. Two consequences are deliberate:
// stacked ID3v1 tags are peeled repeatedly, and Lyrics3 is stripped even when
// the ID3v1 tag its spec mandates after it has already been removed.
func peelTail(rd *reader, l *Layout, floor int64) (int64, error) {
	cursor := rd.size
	prev := KindAudio // nothing peeled yet
	for i := 0; ; i++ {
		if i >= maxPeel {
			return 0, fmt.Errorf("%w: more than %d stacked trailing tags", ErrMalformed, maxPeel)
		}
		matched := false
		for _, detect := range tailDetectors {
			region, ok, err := detect(rd, cursor, floor, prev)
			if err != nil {
				return 0, err
			}
			if !ok {
				continue
			}
			// Never clamp a container to fit. A tag reaching past the audio
			// start, or making no progress, means the file is corrupt; trimming
			// to fit is how this tool would quietly become an audio truncator.
			if region.Offset < floor || region.Offset >= cursor {
				return 0, fmt.Errorf("%w: %s spans %d..%d, outside the audio at %d..%d",
					ErrMalformed, region.Name, region.Offset, region.Offset+region.Size, floor, cursor)
			}
			l.Stripped = append(l.Stripped, region)
			cursor = region.Offset
			prev = region.Kind
			matched = true
			break
		}
		if !matched {
			return cursor, nil
		}
	}
}

// findAudio locates the first audio frame in [front, tail).
func findAudio(rd *reader, l *Layout, front, tail int64) (int64, frameHeader, error) {
	// Fast path: in a well-formed file the first frame sits exactly where the
	// leading tags end, so no scanning happens and no byte can be mis-trimmed.
	if h, ok := chainAt(rd, front, tail); ok {
		return front, h, nil
	}
	// Otherwise there is junk in front. Skip over any ID3v2 tag hiding in it
	// first, so the sync scan cannot wander through embedded cover art.
	from := skipEmbeddedID3(rd, front, tail)
	off, h, ok := resync(rd, from, tail)
	if !ok {
		return 0, frameHeader{}, ErrNoFrames
	}
	if off > front {
		// One region covering everything dropped from the front, including any
		// tag that was embedded in the junk.
		l.Stripped = append(l.Stripped, Region{
			Kind:   KindLeadingJunk,
			Name:   "leading junk",
			Offset: front,
			Size:   off - front,
		})
	}
	return off, h, nil
}

// skipEmbeddedID3 looks for an ID3v2 tag in the first 64 KiB of the search
// window and returns the offset just past it, or from if there is none.
func skipEmbeddedID3(rd *reader, from, limit int64) int64 {
	end := from + 64<<10
	if end > limit {
		end = limit
	}
	n := end - from
	if n < 10 {
		return from
	}
	buf := make([]byte, n) // bounded by the constant above, not by the file
	if _, err := io.ReadFull(io.NewSectionReader(rd.r, from, n), buf); err != nil {
		return from
	}
	for i := 0; i+10 <= len(buf); {
		j := bytes.Index(buf[i:], []byte("ID3"))
		if j < 0 {
			return from
		}
		off := from + int64(i+j)
		if info, ok := id3v2At(rd, off); ok && info.total <= limit-off {
			return off + info.total
		}
		i += j + 1
	}
	return from
}
