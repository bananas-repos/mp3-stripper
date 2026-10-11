package mp3

import (
	"fmt"
	"io"
	"sort"
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
 * Summary is the one-line result. An empty outPath means nothing was written.
 */
func Summary(l *Layout, inPath string, outPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: ", inPath)
	if len(l.Stripped) == 0 {
		b.WriteString("no metadata found")
	} else {
		parts := make([]string, 0, len(l.Stripped))
		for _, r := range l.Stripped {
			parts = append(parts, fmt.Sprintf("%s (%s)", r.Name, byteCountSI(r.Size)))
		}
		fmt.Fprintf(&b, "stripped %s", strings.Join(parts, ", "))
	}
	if outPath == "" {
		fmt.Fprintf(&b, " → %s of audio would remain (dry run, nothing written)\n", byteCountSI(l.AudioSize()))
	} else {
		fmt.Fprintf(&b, " → %s (%s, was %s)\n", outPath, byteCountSI(l.AudioSize()), byteCountSI(l.FileSize))
	}
	return b.String()
}

/**
 * Verbose lists every region found, plus any caveats about the result.
 */
func Verbose(r io.ReaderAt, l *Layout, inPath string) string {
	rows := make([]Region, 0, len(l.Stripped)+1)
	rows = append(rows, l.Stripped...)
	rows = append(rows, Region{
		Kind:   KindAudio,
		Name:   "audio",
		Offset: l.AudioStart,
		Size:   l.AudioSize(),
		Detail: audioDetail(l.Audio),
	})
	sort.Slice(rows, func(i, j int) bool { return rows[i].Offset < rows[j].Offset })

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", inPath, byteCountSI(l.FileSize))
	for _, row := range rows {
		action := "strip"
		if row.Kind == KindAudio {
			action = "keep"
		}
		line := fmt.Sprintf("0x%06x %-18s %-10s %-6s %s", row.Offset, row.Name, byteCountSI(row.Size), action, row.Detail)
		fmt.Fprintln(&b, strings.TrimRight(line, " "))
	}
	for _, w := range warnings(r, l) {
		fmt.Fprintf(&b, "warning: %s", w)
	}
	return b.String()
}

/**
 * audioDetail
 */
func audioDetail(a AudioInfo) string {
	parts := []string{fmt.Sprintf("%v Layer %s %s", a.Version, layer2roman(a.Layer), sampleRateText(a.SampleRate))}
	if a.VBRTag != "" {
		kept := a.VBRTag + " header kept"
		if a.Encoder != "" {
			kept += " (" + a.Encoder + ")"
		}
		parts = append(parts, kept)
	}
	return strings.Join(parts, ", ")
}

/**
 * warnings reports what the result cannot promise. Neither case is repaired:
 * rewriting the Xing header would invalidate the LAME CRC over its first 190
 * bytes, and re-deriving the tail from the frame chain risks cutting audio.
 */
func warnings(r io.ReaderAt, l *Layout) []string {
	var out []string
	if l.Audio.VBRTag != "" && l.Audio.Encoder != "" {
		out = append(out, fmt.Sprintf(
			"the kept %s header still names the encoder (%s) and its settings",
			l.Audio.VBRTag, l.Audio.Encoder))
	}
	if d := l.Audio.Declared; d != 0 && d != l.AudioSize() {
		out = append(out, fmt.Sprintf(
			"%s header declares a %s stream but %s of audio remains; seeking may be slightly off",
			l.Audio.VBRTag, byteCountSI(d), byteCountSI(l.AudioSize())))
	}
	rd := &reader{r: r, size: l.FileSize}
	if !tailAligned(rd, l) {
		out = append(out, "the audio does not end on a frame boundary; trailing bytes were kept as audio")
	}
	return out
}

/**
 * layer2roman to decide of I, II or III
 */
func layer2roman(layer int) string {
	if layer > 3 {
		return "?"
	}
	return strings.Repeat("I", layer)
}


/**
 * sampleRateText format sample rate into human readable text
 */
func sampleRateText(hz int) string {
	return fmt.Sprintf("%.1f kHz", float64(hz)/1000)
}

/**
 * byteCountSI bytes into human readable format with two decimales
 */
func byteCountSI(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
