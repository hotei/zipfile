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
	"log"
	"os"
	"strings"
	"time"

	"github.com/hotei/mdr"
)

// ZipReader adds decompression methods to a seekable io.Reader
//	io.Readers that aren't intrinsicly seekable can be read into an []byte
//	and a bytes.NewReader attached to that buffer.
type ZipReader struct {
	reader io.ReadSeeker
}

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
	version      = "0.1.3"
)

var (
	cantCreatReader  = errors.New("Cant create a NewReader")
	invalidSigError  = errors.New("Bad Local Hdr Sig (invalid magic number)")
	invalidCompError = errors.New("Bad compression method value")
	shortReadError   = errors.New("Short read")
	futureTimeError  = errors.New("File's last Mod time is in future")
	slice16Error     = errors.New("SixteenBit() did not get a 16 bit arg")
	slice32Error     = errors.New("ThirtytwoBit() did not get a 32 bit arg")
	crc32MatchError  = errors.New("Stored CRC32 doesn't match computed CRC32")
	tooBigError      = errors.New("Can't use CRC32 if file > 2GB, Try unsetting Paranoid")
	expandingError   = errors.New("Cant expand array")
	cantHappenError  = errors.New("Cant happen - but did anyway :-(")
)

// Exported controls
var (
	Paranoid     bool
	WriteLogErrs bool = true
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	now := time.Now()
	t := now.Format(time.UnixDate)
	logErr(fmt.Sprintf("pkg zipfile: version %s init at %s\n", version, t))
}

func logErr(s string) {
	if len(s) <= 0 {
		return
	}
	s = strings.TrimRight(s, "\r\n\t ") + "\n"
	fmt.Printf("%s", s)
	if !WriteLogErrs {
		return
	}
	//Verbose.Printf("opening logfile %s\n", logFileName)
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
		//Verbose.Printf("opened %s\n", logFileName)
		defer f.Close()
	}
	//Verbose.Printf("orig name of file = %s\n", f.Name())
	n, err := f.Seek(0, 2)
	if err != nil {
		log.Panicf(fmt.Sprintf("cant seek end of log file:%s\n", err))
	}
	if false {
		Verbose.Printf("seek returned starting point of %d\n", n)
	}
	_, err = f.WriteString(s)
	if err != nil {
		log.Panicf(fmt.Sprintf("cant extend log file:%s\n", err))
	}
}

// NewReader returns an io.Reader that provides the unzipped data for an archive memeber
func NewReader(r io.ReadSeeker) (*ZipReader, error) {
	x := new(ZipReader)
	x.reader = r
	return x, nil
}

// unpackLocalHeader decodes header using PKWare's APPNOTE.TXT as guidance (see README.md)
func (h *ZipfileHeader) unpackLocalHeader(src []byte) error {
	if string(src[0:4]) != zipLocalHdrSig {
		if string(src[0:4]) == zipCentDirSig { // reached last file, now into directory
			h.Size = -1 // signal last file reached
			return nil
		}
		logErr(fmt.Sprintf("invalid sig and its not last file in archive"))
		return invalidSigError
	}
	h.Compress = mdr.SixteenBit(src[8:10])
	switch h.Compress {
	case zipStored: // nothing
	case zipDeflated: // nothing
	case zipImploded: // known method but not implemented as yet
	default:
		logErr(fmt.Sprintf("Wrn-> unknown zip compression method %d\n", h.Compress))
	}
	h.Size = int64(mdr.ThirtyTwoBit(src[22:26]))
	h.SizeCompr = int64(mdr.ThirtyTwoBit(src[18:22]))
	h.StoredCrc32 = mdr.ThirtyTwoBit(src[14:18])

	pktime := mdr.SixteenBit(src[10:12])
	pkdate := mdr.SixteenBit(src[12:14])
	h.Mtime = makeGoDate(pkdate, pktime)
	if h.Mtime.After(time.Now()) {
		logErr(fmt.Sprintf("Wrn-> %s mod-time is in the future\n", h.FileName))
		if Paranoid {
			fatalError(futureTimeError)
		}
	}
	Verbose.Printf("%s mod-time parsed to : %s\n", h.FileName, h.Mtime.String())
	return nil
}

func (r *ZipReader) Reset() {
	_, err := r.reader.Seek(0, 0)
	if err != nil {
		logErr("Cant reset ZipReader to beginning of archive")
		fatalError(err)
	}
}

// grabs the next zip header from the archive
// returns one header pointer for each stored file
func (r *ZipReader) ZipfileHeaders() ([]*ZipfileHeader, error) {
	Hdrs := make([]*ZipfileHeader, 0, 20)
	r.Reset()
	for {
		hdr, err := r.Next()
		if err != nil {
			if Paranoid {
				fatalError(err)
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
			fatalError(err)
		} else {
			return nil, err
		}
	}
	if n != localHdrSize {
		// BUG(mdr) need examples so we can fix - if still an issue
		fmt.Printf("Read %d bytes of header, expected %d : %v %s\n", n, localHdrSize, localHdr, localHdr)
		fmt.Printf("n(%d) < localHdrSize(%d)\n", n, localHdrSize)
		if Paranoid {
			fmt.Printf("Read %d bytes of header = %v\n", localHdrSize, localHdr)
			fatalError(shortReadError)
		} else {
			return nil, shortReadError
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
			fatalError(cantHappenError)
		} else {
			// or is it just end-of-file on multi-vol?
			return nil, nil // ignore it
		}
	}
	fname := make([]byte, fileNameLen)
	n, err = r.reader.Read(fname)
	if err != nil {
		if Paranoid {
			fatalError(err)
		} else {
			return nil, err
		}
	}
	if n < int(fileNameLen) {
		fmt.Printf("n < fileNameLen\n")
		if Paranoid {
			fatalError(shortReadError)
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
			fatalError(err)
		} else {
			return nil, err
		}
	}
	hdr.Offset = currentPos
	// seek past compressed/stored blob to start of next header
	_, err = r.reader.Seek(hdr.SizeCompr, 1)
	if err != nil {
		if Paranoid {
			fatalError(err)
		} else {
			return nil, err
		}
	}
	if hdr.Compress == zipImploded {
		logErr(fmt.Sprintf("Wrn-> explode not implemented for %s\n",hdr.FileName))
	}
	// NOTE: side effect is to move r.reader pointer to start of next header
	return hdr, nil
}

// Simple listing of header, same data should appear for the command "unzip -v file.zip"
// but with slightly different order and formatting  TODO - make format more similar ?
func (hdr *ZipfileHeader) Dump() {
	if hdr.FileName == "" {
		logErr("Wrn-> filename is blank")
		//log.Panicf("FileName is blank")
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

// Open is a dispatcher for the various compression methods
func (h *ZipfileHeader) Open() (io.Reader, error) {
	if h.Size <= 0 {
		var buf []byte = []byte{}
		r := bytes.NewReader(buf)
		return r, nil
	}
	var rv io.Reader
	var err error
	switch h.Compress {
	case zipStored: // type 0
		rv, err = h.ReadStored()
	case zipDeflated: // type 8
		rv, err = h.ReadDeflated()
	case zipImploded: // type 6
		rv, err = h.ReadImploded()
	default:
		rv, err = nil, invalidCompError
	}
	return rv, err
}

func (h *ZipfileHeader) ReadStored() (io.Reader, error) {
	//reset the reader in case it has been advanced elsewhere
	_, err := h.Hreader.Seek(h.Offset, 0)
	if err != nil {
		if Paranoid {
			fatalError(err)
		} else {
			return nil, err
		}
	}
	// copy stored data into a bufffer prior to CRC32
	b := new(bytes.Buffer)
	var n2 int64
	n2, err = io.Copy(b, h.Hreader)
	if err != nil {
		if Paranoid {
			fatalError(err)
		} else {
			return nil, err
		}
	}
	if n2 < h.Size {
		logErr(fmt.Sprintf("Actually copied %d, expected to copy %d\n", n2, h.Size))
		if Paranoid {
			fatalError(shortReadError)
		} else {
			return nil, shortReadError
		}
	}
	buf := b.Bytes()
	// debug with fmt.Printf("%s\n", buf[0:30])
	// now we want to crc32 the buffer and check computed vs stored crc32
	mycrc32 := crc32.ChecksumIEEE(buf)
	if Verbose {
		fmt.Printf("Computed Checksum = %0x, stored checksum = %0x\n", mycrc32, h.StoredCrc32)
	}
	if mycrc32 != h.StoredCrc32 {
		if Paranoid {
			fatalError(crc32MatchError)
		} else {
			return nil, crc32MatchError
		}
	}

	_, err = h.Hreader.Seek(h.Offset, 0)
	if err != nil {
		if Paranoid {
			fatalError(err)
		} else {
			return nil, err
		}
	}
	return h.Hreader, nil
}

// BUG(mdr): ReadImploded is a stub till we get it sorted properly
func (h *ZipfileHeader) ReadImploded() (io.Reader, error) {
	logErr(fmt.Sprintf("Wrn-> implode method is not implemented: file %s", h.FileName))
	if Paranoid {
		fatalError(invalidCompError)
	}

	var buf []byte = []byte{}
	r := bytes.NewReader(buf)
	return r, nil
}

func (h *ZipfileHeader) ReadDeflated() (io.Reader, error) {
	_, err := h.Hreader.Seek(h.Offset, 0)
	if err != nil {
		if Paranoid {
			fatalError(err)
		} else {
			return nil, err
		}
	}
	comprData := make([]byte, h.SizeCompr)
	n, err := h.Hreader.Read(comprData)
	if err != nil {
		if Paranoid {
			fatalError(err)
		} else {
			return nil, err
		}
	}
	if int64(n) < h.SizeCompr {
		fmt.Printf("read(%d) which is less than stored compressed size(%d)", n, h.SizeCompr)
		if Paranoid {
			fatalError(shortReadError)
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
			fatalError(err)
		} else {
			return inpt, err // TODO not always safe but currently necessary for big files, will fix soon
		}
	}

	// BUG(mdr) near line 393 TODO do we need to handle bigger files gracefully or is 1 GB enough per zipfile?
	if h.Size > tooBig {
		if Paranoid {
			logErr(fmt.Sprintf("source uncompressed size is larger than %d bytes\n",tooBig))
			fatalError(tooBigError)
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
			fatalError(err)
		} else {
			return nil, err
		}
	}
	if n2 < h.Size {
		logErr(fmt.Sprintf("Actually copied %d, expected to copy %d\n", n, h.Size))
		if Paranoid {
			fatalError(shortReadError)
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
		logErr(fmt.Sprintf("copied %d, expected %d\n", n, h.Size))
		if Paranoid {
			fatalError(shortReadError)
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
			fatalError(crc32MatchError)
		} else {
			return nil, crc32MatchError
		}
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
	// BUG(mdr): pull date check out and put in mdr package.
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

func fatalError(erx error) {
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
