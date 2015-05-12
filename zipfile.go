// zipfile.go Copyright 2009-2015 David Rook. All rights reserved.

/*
 * Use of this source code is governed by a BSD-style
 * license that can be found in the LICENSE file.
 * source can be found at github.com/hotei/zipfile
 *
 * <David Rook> hotei1352@gmail.com
 *
 * This is a work-in-progress
 *      This version does only zip reading, no writing (now or planned)
 *      Updated to match new go package rqmts on 2011-12-13 working again
 *
 *    Additional documentation for package 'zip' can be found in doc.go

 Pkg returns many fatal errors.  Need to feed error back out of package
 and let caller determine what's really fatal.  Current version quits if fed a
 corrupted file.  That's going to be common in our intended use environment.
*/

package zipfile

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/hotei/mdr"
)

// ZipReader adds decompression methods to a seekable io.Reader
//	io.Readers that aren't intrinsicly seekable can be read into an []byte
//	and a bytes.NewReader attached to that buffer.
type ZipReader struct {
	reader io.ReadSeeker
}

// name of original zip archive will be unknown if source is just an io.Reader?
// can we use f.Name() to get original name? no.
//	ArkName     string

// Describes one entry in zip archive
// data be compressed or stored (ie. type 8 or 0 only) - NOTE! other formats exist, "imploded" is one
type ZipfileHeader struct {
	FileName    string // name of file inside archive
	Size        int64  // size while uncompressed
	SizeCompr   int64  // size while compressed
	Typeflag    byte
	Mtime       time.Time // use 'go' version of time, converted from MSDOS version
	Compress    uint16
	Offset      int64
	StoredCrc32 uint32
	Hreader     io.ReadSeeker
}

const (
	bitsInInt      = 32
	msdosEpoch     = 1980
	zipLocalHdrSig = "PK\003\004"
	zipCentDirSig  = "PK\001\002"
	zipStored      = 0
	zipImploded    = 6
	// implode is NOT supported (nor is it supported by the stdlib archive/zip)
	// I have the c code for it but conversion to go is non-trivial :(
	zipDeflated  = 8
	tooBig       = 1<<(bitsInInt-1) - 1
	localHdrSize = 30
	logFileName  = "/home/mdr/gologs/golog.txt"
)

var (
	cantCreatReader  = errors.New("Cant create a NewReader")
	invalidSigError  = errors.New("Bad Local Hdr Sig (invalid magic number)")
	invalidCompError = errors.New("Bad compression method value")
	shortReadError   = errors.New("short read")
	futureTimeError  = errors.New("file's last Mod time is in future")
	slice16Error     = errors.New("SixteenBit() did not get a 16 bit arg")
	slice32Error     = errors.New("thirtytwoBit() did not get a 32 bit arg")
	crc32MatchError  = errors.New("Stored CRC32 doesn't match computed CRC32")
	tooBigError      = errors.New("Can't use CRC32 if file > 2GB, Try unsetting Paranoid")
	expandingError   = errors.New("Cant expand array")
	cantHappenError  = errors.New("Cant happen - but did anyway :-(")
)

// used to control behavior of zip library code
var (
	Paranoid    bool
	LogMethErrs bool
)

// A ZipReader provides sequential or random access to the contents of a zip archive.
// A zip archive consists of a sequence of files.
// The Next method advances to the next file in the archive (including the first),
// and then it can be treated as an io.Reader to access the file's data.
// You can also pull all the headers with  h := rz.ZipfileHeaders() and then open
// an individual file number n with rdr := h[n].Open()  See test suite for more examples.
//
// Example:
// func test_2() {
//	const testfile = "stuf.zip"
//
//	input, err := os.Open(testfile, os.O_RDONLY, 0666)
//	if err != nil {
//		fatal_err(err)
//	}
//	fmt.Printf("opened zip file %s\n", testfile)
//	rz, err := zip.NewReader(input)
//	if err != nil {
//		fatal_err(err)
//	}
//	hdr, err := rz.Next()
//	rdr, err := hdr.Open()
//	_, err = io.Copy(os.Stdout, rdr) // open first file only
//	if err != nil {
//		fatal_err(err)
//	}
// }

func init() {
	now := time.Now()
	t := now.Format(time.UnixDate)
	logErr(fmt.Sprintf("pkg zipfile: init at %s\n", t))
}

func logErr(s string) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	Verbose.Printf("opening logfile %s\n", logFileName)
	//f, err := os.OpenFile(logFileName,os.O_APPEND, 0640)
	// ? fails if file empty when first called, must use RDWR initially?
	f, err := os.OpenFile(logFileName, os.O_RDWR, 0640)
	if err != nil {
		Verbose.Printf("no existing log - opening new logfile %s\n", logFileName)
		f, err = os.Create(logFileName)
		if err != nil {
			log.Panicf("cant create log file\n")
		}
		defer f.Close()
	} else {
		Verbose.Printf("opened %s\n", logFileName)
		defer f.Close()
	}
	Verbose.Printf("orig name of file = %s\n", f.Name())
	n, err := f.Seek(0, 2)
	if err != nil {
		log.Panicf(fmt.Sprintf("cant seek end of log file:%s\n", err))
	}
	Verbose.Printf("seek returned starting point of %d\n", n)
	_, err = f.WriteString(s)
	if err != nil {
		log.Panicf(fmt.Sprintf("cant extend log file:%s\n", err))
	}
}

func NewReader(r io.ReadSeeker) (*ZipReader, error) {
	_, err := r.Seek(0, 0) // make sure we've got a seekable input ?is this needed?
	if err != nil {
		log.Panicf(fmt.Sprintf("cant seek :%s\n", err))
	}
	x := new(ZipReader)
	x.reader = r
	// err might not be nil on return - caller MUST test
	return x, err
}

// Unpack header based on PKWare's APPNOTE.TXT
func (h *ZipfileHeader) unpackLocalHeader(src []byte) error {
	if string(src[0:4]) != zipLocalHdrSig {
		if string(src[0:4]) == zipCentDirSig { // reached last file, now into directory
			h.Size = -1 // signal last file reached
			return nil
		}
		return invalidSigError // has invalid sig and its not last file in archive
	}
	h.Compress = mdr.SixteenBit(src[8:10])

	switch h.Compress {
	case zipStored: // nothing
	case zipDeflated: // nothing
	case zipImploded:
		fmt.Printf("Sorry, I can't handle the zip implode compression method\n")
		if LogMethErrs {
			logErr(fmt.Sprintf("Err-> file %q uses implode method (not handled)\n", h.FileName))
		}
		return invalidCompError
	default:
		fmt.Printf("Sorry, I can't handle zip compression method %d\n", h.Compress)
		return invalidCompError
	}

	//	if h.Compress != zipStored && h.Compress != zipDeflated {
	//		fmt.Printf("I can't handle zip compression method %d\n", h.Compress)
	//		return invalidCompError
	//	}
	h.Size = int64(mdr.ThirtyTwoBit(src[22:26]))
	h.SizeCompr = int64(mdr.ThirtyTwoBit(src[18:22]))
	h.StoredCrc32 = mdr.ThirtyTwoBit(src[14:18])

	pktime := mdr.SixteenBit(src[10:12])
	pkdate := mdr.SixteenBit(src[12:14])
	h.Mtime = makeGoDate(pkdate, pktime)
	if h.Mtime.After(time.Now()) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", h.FileName, futureTimeError)
		if Paranoid {
			fatal_err(futureTimeError)
		} else {
			// warning or just ignore it ?
		}
	}
	if Verbose {
		fmt.Printf("ZipfileHeader time parsed to : %s\n", h.Mtime.String())
	}
	return nil
}

// grabs the next zip header from the archive
// returns one header pointer for each stored file
func (r *ZipReader) ZipfileHeaders() ([]*ZipfileHeader, error) {
	Hdrs := make([]*ZipfileHeader, 0, 20)
	_, err := r.reader.Seek(0, 0)
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}
	for {
		hdr, err := r.Next()
		if err != nil {
			if Paranoid {
				fatal_err(err)
			} else {
				return nil, err
			}
		}
		if hdr == nil {
			break
		}
		if Verbose {
			hdr.Dump()
		}
		Hdrs = append(Hdrs, hdr)
	}
	return Hdrs, nil
}

// BUG(mdr) near line 258 getting short read for UNK reasons in Next()
//		assume body of zip is getting stored in header for some minor savings
//		is it described (and I assume allowed) in spec?

// decode PK formats and convert to go values, returns next ZipfileHeader pointer or
// 		nil when no more data available
func (r *ZipReader) Next() (*ZipfileHeader, error) {

	//	var localHdr [localHdrSize]byte (pre fixup)
	// start by reading fixed size fields (Name,Extra are vari-len)
	// after fixup we need to see this .Read([]byte)
	localHdr := make([]byte, localHdrSize)
	n, err := r.reader.Read(localHdr)
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}
	if n < localHdrSize {
		fmt.Printf("Read %d bytes of header = %v %s\n", localHdrSize, localHdr, localHdr)
		fmt.Printf("n(%d) < localHdrSize(%d)\n", n, localHdrSize)
		if Paranoid {
			fmt.Printf("Read %d bytes of header = %v\n", localHdrSize, localHdr)
			fatal_err(shortReadError)
		} else {
			return nil, shortReadError // BUG why unexpected - sometimes
		}
	}
	if Verbose {
		fmt.Printf("Read %d bytes of header = %v\n", localHdrSize, localHdr)
	}
	hdr := new(ZipfileHeader)
	hdr.Hreader = r.reader
	err = hdr.unpackLocalHeader(localHdr)
	if err != nil {
		return nil, err
	}
	if hdr.Size == -1 { // reached end of archive records, start of directory
		// not an error, return nil to signal no more data
		return nil, nil
	}
	fileNameLen := mdr.SixteenBit(localHdr[26:28])
	// TODO read past end of archive without seeing Central Directory ? NOT POSSIBLE ?
	// what about multi-volume disks?  Do they have any Central Dir data?
	if fileNameLen == 0 {
		fmt.Fprintf(os.Stderr, "read past end of archive and didn't find the Central Directory")
		if Paranoid {
			fatal_err(cantHappenError)
		} else {
			// or is it just end-of-file on multi-vol?
			return nil, nil // ignore it
		}
	}
	fname := make([]byte, fileNameLen)
	n, err = r.reader.Read(fname)
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}
	if n < int(fileNameLen) {
		fmt.Printf("n < fileNameLen\n")
		if Paranoid {
			fatal_err(shortReadError)
		} else {
			return nil, shortReadError
		}
	}
	Verbose.Printf("filename: %s \n", fname)
	hdr.FileName = string(fname)
	// read extra data if present
	Verbose.Printf("reading extra data (if any is present)\n")
	extraFieldLen := mdr.SixteenBit(localHdr[28:30])
	// skip over it if needed, but in either case, save current position after seek
	// ie. degenerate case is Seek(0,1) but do it anyway
	currentPos, err := r.reader.Seek(int64(extraFieldLen), 1)
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}
	hdr.Offset = currentPos
	// seek past compressed/stored blob to start of next header
	_, err = r.reader.Seek(hdr.SizeCompr, 1)
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}

	// NOTE: side effect is to move r.reader pointer to start of next header
	return hdr, nil
}

// Simple listing of header, same data should appear for the command "unzip -v file.zip"
// but with slightly different order and formatting  TODO - make format more similar ?
func (hdr *ZipfileHeader) Dump() {
	if hdr.FileName == "" {
		log.Panicf("FileName is blank")
	}
	Mtime := hdr.Mtime.UTC()
	//	fmt.Printf("%s: Size %d, Size Compressed %d, Type flag %d, LastMod %s, ComprMeth %d, Offset %d\n",
	//		hdr.Name, hdr.Size, hdr.SizeCompr, hdr.Typeflag, Mtime.String(), hdr.Compress, hdr.Offset)
	var method string

	switch hdr.Compress {
	case zipStored:
		method = "Stored"
	case zipDeflated:
		method = "Deflated"
	case zipImploded:
		method = "Imploded"
	default:
		method = "Unknown"
	}

	// sec := time.SecondsToUTC(hdr.Mtime)
	// fmt.Printf("ZipfileHeader time parsed to : %s\n", sec.String())
	// BUG(mdr): near line 383 use order and field length similar to MSDOS version
	fmt.Printf("%8d  %8s  %7d   %4d-%02d-%02d %02d:%02d:%02d  %08x  %s\n",
		hdr.SizeCompr, method, hdr.Size,
		Mtime.Year(), Mtime.Month(), Mtime.Day(), Mtime.Hour(), Mtime.Minute(), Mtime.Second(),
		hdr.StoredCrc32, hdr.FileName)
}

func (h *ZipfileHeader) Open() (io.Reader, error) {
	_, err := h.Hreader.Seek(h.Offset, 0)
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}
	comprData := make([]byte, h.SizeCompr)
	n, err := h.Hreader.Read(comprData)
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}
	if int64(n) < h.SizeCompr {
		fmt.Printf("read(%d) which is less than stored compressed size(%d)", n, h.SizeCompr)
		if Paranoid {
			fatal_err(shortReadError)
		} else {
			return nil, shortReadError
		}
	}
	if Verbose {
		fmt.Printf("ZipfileHeader.Open() Read in %d bytes of compressed (deflated) data\n", n)
		// prints out filename etc so we can later validate expanded data is appropriate
		h.Dump()
	}
	// got it as comprData in RAM, now need to expand it
	in := bytes.NewBuffer(comprData) // fill new buffer with compressed data
	inpt := flate.NewReader(in)      // attach a reader to the buffer
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return inpt, err // TODO not always safe but currently necessary for big files, will fix soon
		}
	}

	// BUG(mdr) near line 433 TODO do we need to handle bigger files gracefully or is 1 GB enough per zipfile?

	if h.Size > tooBig {
		if Paranoid {
			fatal_err(tooBigError)
		} else {
			return nil, tooBigError
		}
	}

	// normally we want to crc32 the buffer and check computed vs stored crc32
	b := new(bytes.Buffer) // create a new buffer with io methods
	var n2 int64
	n2, err = io.Copy(b, inpt) // now fill buffer from compressed data using inpt
	if err != nil {
		if Paranoid {
			fatal_err(err)
		} else {
			return nil, err
		}
	}
	if n2 < h.Size {
		fmt.Printf("Actually copied %d, expected to copy %d\n", n, h.Size)
		if Paranoid {
			fatal_err(shortReadError)
		} else {
			return nil, shortReadError
		}
	}
	// TODO this feels like an extra step but not sure how to shorten it yet
	// problem is we can't run ChecksumIEEE on Buffer, it requires []byte arg
	// update: Russ Cox provided advice on method of attack, still need to implement
	expdData := make([]byte, h.Size) // make the expanded buffer into a byte array
	n, err = b.Read(expdData)        // copy buffer into expdData
	if int64(n) < h.Size {
		fmt.Printf("copied %d, expected %d\n", n, h.Size)
		if Paranoid {
			fatal_err(shortReadError)
		} else {
			return nil, shortReadError
		}
	}
	mycrc32 := crc32.ChecksumIEEE(expdData)
	if Verbose {
		fmt.Printf("Computed Checksum = %0x, stored checksum = %0x\n", mycrc32, h.StoredCrc32)
	}
	if mycrc32 != h.StoredCrc32 {
		if Paranoid {
			fatal_err(crc32MatchError)
		} else {
			return nil, crc32MatchError
		}
	}
	if false {
		// test by copying files out to disk
		//========	this worked, producing legal zip files ==========
		fp, err := ioutil.TempFile(".", "test")
		if err != nil {
			fmt.Printf("walker: err %v\n", err)
		}
		nw, err := fp.Write(expdData)
		_ = nw
		if err != nil {
			fmt.Printf("zipfile: write fail err %v\n", err)
		}
		//===========================================================
	}
	bufReader := bytes.NewReader(expdData)
	return bufReader, nil // who closes bufReader and how?
}

//	convert PKware date, time uint16s into seconds since Unix Epoch
func makeGoDate(d, t uint16) time.Time {
	var year, month, day uint16
	year = d & 0xfe00
	year >>= 9
	month = d & 0x01e0
	month >>= 5
	day = d & 0x001f
	day = day

	var hour, minute, second uint16
	hour = t & 0xf800
	hour >>= 11
	minute = t & 0x01e0
	minute >>= 5
	second = (t & 0x001f) * 2
	second = second

	ftYear := int(year + msdosEpoch)
	ftMonth := time.Month(month)
	ftDay := int(day)
	ftHour := int(hour)
	ftMinute := int(minute)
	ftSecond := int(second)
	//	ftZoneOffset := 0
	ftZone := time.UTC

	ft := time.Date(ftYear, ftMonth, ftDay, ftHour, ftMinute, ftSecond, 0, ftZone)
	if Verbose {
		fmt.Printf("year(%d) month(%d) day(%d) \n", year, month, day)
		fmt.Printf("hour(%d) minute(%d) second(%d)\n", hour, minute, second)
	}
	// TODO this checking is approximate for now, daysinmonth not checked fully
	// TODO we wont know file name at this point unless Verbose is also true
	//  ? is that a problem or not ?
	if Paranoid {
		badDate := false
		// no such thing as a bad year as 0..127 are valid
		// and represent 1980 thru 2107
		// if a file's Mtime is in the future Paranoid will it catch later
		if !mdr.InRangeI(1, int(month), 12) {
			badDate = true
		}
		if !mdr.InRangeI(1, int(day), 31) { // BUG(mdr) near line 547 incomplete date check
			badDate = true
		}
		if !mdr.InRangeI(0, int(hour), 23) {
			badDate = true
		}
		if !mdr.InRangeI(0, int(minute), 59) {
			badDate = true
		}
		if !mdr.InRangeI(0, int(second), 59) {
			badDate = true
		}
		// BUG(mdr) near line 559 TODO ? should we log bad dates
		if badDate {
			fmt.Fprintf(os.Stderr, "Encountered bad Mod Date/Time: \n")
			fmt.Fprintf(os.Stderr, "year(%d) month(%d) day(%d) \n", year, month, day)
			fmt.Fprintf(os.Stderr, "hour(%d) minute(%d) second(%d)\n", hour, minute, second)
		}
	}
	return ft
}

func fatal_err(erx error) {
	fmt.Printf("stopping because: %s \n", erx)
	os.Exit(1)
}

/*
func ReaderAtSection(r io.ReaderAt, start, end int64) io.ReaderAt {
	return nil
}

func ReaderAtStream(r io.ReaderAt) io.Reader {
	return nil
}
*/
