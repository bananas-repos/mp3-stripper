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

// Summary is the one-line result. An empty outPath means nothing was written.
func Summary(l *Layout, inPath, outPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: ", inPath)
	if len(l.Stripped) == 0 {
		b.WriteString("no metadata found")
	} else {
		parts := make([]string, 0, len(l.Stripped))
		for _, r := range l.Stripped {
			parts = append(parts, fmt.Sprintf("%s (%s)", r.Name, human(r.Size)))
		}
		fmt.Fprintf(&b, "stripped %s", strings.Join(parts, ", "))
	}
	if outPath == "" {
		fmt.Fprintf(&b, " → %s of audio would remain (dry run, nothing written)\n", human(l.AudioSize()))
	} else {
		fmt.Fprintf(&b, " → %s (%s, was %s)\n", outPath, human(l.AudioSize()), human(l.FileSize))
	}
	return b.String()
}

// Verbose lists every region found, plus any caveats about the result.
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
	fmt.Fprintf(&b, "%s (%s)\n", inPath, human(l.FileSize))
	for _, row := range rows {
		action := "strip"
		if row.Kind == KindAudio {
			action = "keep"
		}
		line := fmt.Sprintf("  0x%06x  %-18s %10d B  %-6s %s",
			row.Offset, row.Name, row.Size, action, row.Detail)
		fmt.Fprintln(&b, strings.TrimRight(line, " "))
	}
	for _, w := range warnings(r, l) {
		fmt.Fprintf(&b, "  warning: %s", w)
	}
	return b.String()
}

func audioDetail(a AudioInfo) string {
	parts := []string{fmt.Sprintf("%s Layer %s %s",
		a.Version, roman(a.Layer), sampleRateText(a.SampleRate))}
	if a.VBRTag != "" {
		kept := a.VBRTag + " header kept"
		if a.Encoder != "" {
			kept += " (" + a.Encoder + ")"
		}
		parts = append(parts, kept)
	}
	return strings.Join(parts, ", ")
}

// warnings reports what the result cannot promise. Neither case is repaired:
// rewriting the Xing header would invalidate the LAME CRC over its first 190
// bytes, and re-deriving the tail from the frame chain risks cutting audio.
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
			l.Audio.VBRTag, human(d), human(l.AudioSize())))
	}
	rd := &reader{r: r, size: l.FileSize}
	if !tailAligned(rd, l) {
		out = append(out, "the audio does not end on a frame boundary; trailing bytes were kept as audio")
	}
	return out
}

func roman(layer int) string {
	switch layer {
	case 1:
		return "I"
	case 2:
		return "II"
	case 3:
		return "III"
	}
	return "?"
}

func sampleRateText(hz int) string {
	if hz%1000 == 0 {
		return fmt.Sprintf("%d kHz", hz/1000)
	}
	return fmt.Sprintf("%.1f kHz", float64(hz)/1000)
}

func human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		v /= unit
		if v < unit {
			return fmt.Sprintf("%.1f %s", v, suffix)
		}
	}
	return fmt.Sprintf("%.1f TiB", v/unit)
}

func joinWords(s []string) string { return strings.Join(s, " ") }
