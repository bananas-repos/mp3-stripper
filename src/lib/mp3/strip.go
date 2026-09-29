package mp3

import (
	"fmt"
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

// Write copies the audio span to dst and returns the number of bytes written.
//
// The kept range is one contiguous span, so stripping is a plain copy: no
// reframing and no rewriting, which is what makes the output's audio bytes
// provably identical to the input's.
func Write(dst io.Writer, src io.ReaderAt, l *Layout) (int64, error) {
	// One invariant, checked immediately before the copy, covering every path
	// that could have produced a nonsensical span.
	if l.AudioStart < 0 || l.AudioStart >= l.AudioEnd || l.AudioEnd > l.FileSize {
		return 0, fmt.Errorf("%w: audio span [%d,%d) in a %d byte file",
			ErrMalformed, l.AudioStart, l.AudioEnd, l.FileSize)
	}
	return io.Copy(dst, io.NewSectionReader(src, l.AudioStart, l.AudioSize()))
}
