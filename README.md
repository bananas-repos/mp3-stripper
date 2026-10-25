# m3s

This is a tool to strip mp3 from all unwanted meta information and unknown fields you do not need.

More information and official release visit: https://www.bananas-playground.net/projekt/m3s

# Usage

`m3s [options] <file>`. `<file>` is required.

Use m3s on any mp3 file you have to strip it from all meta informations.

Example: `m3s mp3file.mp3`

The example will create `clean_mp3file.mp3` which only includes required mp3 data. Most of the
meta information is removed.

```
VERBOSE  "Using input: 'sample.mp3'"
DEBUG    "sample.mp3 (11.94 MB)
0x000000 ID3v2.3            58.01 kB   strip  APIC(57.47 kB) TCON TALB TCOM TXXX TXXX TIT2 TPE1 TPE2 TCOP TIT3 TXXX TXXX TXXX TYER TXXX TXXX TSSE TXXX TRCK TPOS
0x00e296 audio              11.88 MB   keep   3 Layer III 48.0 kHz, Info header kept (Lavc59.7.)
0xb62c96 ID3v1.1            128 B      strip  track 1
warning: the kept Info header still names the encoder (Lavc59.7.) and its settings"
```


# Options

```
--verbose, -v   be verbose
--debug, -d     print debug information
--simulate, -s  simulate what would be stripped, write nothing
--path, -p      the directory to write the file to
--replace, -r   replace! the inputfile. Use with caution
--version       display version
--help, -h      display help
```

# Contribute

Want to contribute or found a problem? See Contributing document: CONTRIBUTING.md
