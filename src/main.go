package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"m3s/lib/mp3"
	"os"
	"path/filepath"

	"github.com/jpillora/opts"
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
 * 2026 https://://www.bananas-playground.net/projekt/mp3-stripper
 */

var m3sVersion = "0.1.0"
var m3sWebsite = "https://www.bananas-playground.net/projekt/m3s"

// command line options
type cliParams struct {
	Verbose bool `help:"be verbose"`
	Debug bool `help:"print debug information"`
	Simulate bool `help:"simulate what would be stripped, write nothing"`
	Path string `help:"the directory to write the file to"`
	Replace bool `help:"replace! the inputfile. Use with caution"`
	File string `opts:"mode=arg" help:"<file> is required"`
}
var cli cliParams

func main() {
	// parse cli params and make sure required input is given
	//cli := cliParams{}
	opts.New(&cli).
		Version(m3sVersion).
		Repo(m3sWebsite).
		Parse()

	// ready set  go
	verboseMsg(fmt.Sprintf("Using input : %s", cli.File))
	inputFile, err := os.Open(cli.File)
	errorCheck(err, "Can not open input.")
	defer inputFile.Close()

	inputInfo, err := inputFile.Stat()
	errorCheck(err, "Can not get more information of input.")
	if inputInfo.IsDir() {
		log.Fatalf("ERROR \"'%s' is a directory. Not implemented yet.\"\n", cli.File)
	}

	mp3Layout, err := mp3.Scan(inputFile, inputInfo.Size())
	errorCheck(err, "Invalid filetype.")
	dbgMsg(mp3.Verbose(inputFile, mp3Layout, cli.File))

	// just simulate what would be done and exit
	if cli.Simulate {
		fmt.Print(mp3.Summary(mp3Layout, cli.File, ""))
		return
	}

	newFile := "clean_" + cli.File
	verboseMsg(fmt.Sprintf("New filename: '%s'. Replace: %t", newFile, cli.Replace))
	if _, err := os.Stat(newFile); err == nil {
		// fatalf exit the programm, so no more check needed.
		if !cli.Replace {
			log.Fatalf("ERROR \"Outputfile '%s' exists\"\n", newFile)
		}
		log.Printf("INFO \" Outputfile '%s' will be overwritten!\"\n", newFile)
	}

	// all good. Write the new file
	err = writeFile(newFile, inputFile, mp3Layout, inputInfo.Mode().Perm())
	errorCheck(err, "Can not write output file.")

	verboseMsg(fmt.Sprintf("Created: %s", newFile))
}

/**
 * Check and display error with additional message
 */
func errorCheck(e error, msg string) {
	if e != nil {
		log.Fatalf("ERROR \"%s\" Error \"%s\"",msg,e)
	}
}

/**
 * Print a verbose message
 */
func verboseMsg(msg string) {
	if cli.Verbose {
		log.Printf("VERBOSE \"%s\"", msg)
	}
}

/**
 * Print a debug message
 */
func dbgMsg(msg string) {
	if cli.Debug {
		log.Printf("DEBUG \"%s\"", msg)
	}
}

/**
 * writeFile writes the audio to a temporary file in the destination
 * directory and renames it into place, so a failure part-way through can never
 * leave a truncated file that looks valid.
 */
func writeFile(dst string, src io.ReaderAt, l *mp3.Layout, mode os.FileMode) error {
	if st, err := os.Stat(dst); err == nil {
		if st.IsDir() {
			return fmt.Errorf("output %s is a directory", dst)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".m3s-*")
	if err != nil {
		return err
	}
	defer func() {
		if tmp != nil { // still set means we failed: leave nothing behind
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	if _, err := mp3.Write(tmp, src, l); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	tmp = nil
	if err := os.Rename(name, dst); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
