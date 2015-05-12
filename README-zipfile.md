<center>
# zipfile
</center>


Comments can be sent to <hotei1352@gmail.com>

## OVERVIEW
The _zipfile_ package reads files encoded in zip format.  Usage is similar to the
archive/zip package in go standard library.

This package is (hopefully) a little more tolerant of media errors than the 
standard go library package. When released initially as "go-zipfile" it was my 
first "non-toy" program in go.  As "zipfile" I have tweaked the API a bit and
updated the text to make it more idiomatic go.  When originally written there
wasn't nearly as much concensus about what "idiomatic go" looked like.

### Installation

If you have a working go installation on a Unix-like OS:

> ```go get github.com/hotei/zipfile```

Will copy github.com/hotei/zipfile to the first entry of your $GOPATH

or if go is not installed yet :

> ```cd DestinationDirectory```

> ```git clone https://github.com/hotei/zipfile.git```

### Background

Zip was born in the days when diskettes held less than 1 MB and it was common to
use several diskettes or "volumes" to create an archive of a larger disk directory.
In multi-volume cases a central directory was stored on the last diskette so that
you wouldn't have to handle every diskette in a 20 volume set to find which diskette
held a specific file.  The restore program would read the last diskette and then
prompt you to insert the next appropriate diskette.

In our case it doesn't matter, because this version doesn't know or care
if it's multi-volume. This version of the zip library does NOT
look at the central header areas at
the end of the zip archive.  Instead it builds headers on the fly by reading the
actual archived data. Reading the actual data (instead of a catalog) may be useful to validate
the readability of older removeable media like 5.25 inch diskettes and early CDs.

The initial approach was to convert python's zipfile.py into go.  For a number of
reasons I soon decided this method was "too hard", even though I know python
fairly well.  Rather than convert, this package was writen from scratch
based on PKWARE's APPNOTE.TXT.  APPNOTE.TXT describes the contents of zip files
from the perspective of the company who designed the zip protocol.  The resulting zipfile.go
package is ready for testing and passes the initial test suite.


### Limitations

* <font color="red">Files being read must be less than 2 GB in size</font>
* <font color="red">64 bit zips are not supported</font> Files are worked on in
memory and this places a limit

### Usage

See the test examples for usage samples.

### Features

* a log file is optional 
* paranoid mode can be turned off to allow programs to work even with invalid
dates and archives where the crc codes fail to match.

### BUGS

* bad dates are checked, both for sanity of values and to insure the date is
not "in the future" as of the time being listed".  This checking is "stubbed"
and should be improved.  Files with bad dates should probably be logged?
* Short reads in Next() due to strange header storage.  Need a sample file that
demonstrates problem. (OBE?)


### To-Do

* Essential:
  * TBD
* Nice:
  * TBD
* Nice but no immediate need:
  * Allow program to accept archives > 4 GB as long as individual members are
less than the Max
  * Make sure format of Dump() output matches MSDOS flavor.
  
### Change Log

* 2010-04-27 Started
 
### Resources

* [go language reference] [1] 
* [go standard library package docs] [2]
* [Source for program] [3]
* [Specification] [4] for zip format as documented by __pkware__ 

[1]: http://golang.org/ref/spec/ "go reference spec"
[2]: http://golang.org/pkg/ "go package docs"
[3]: http://github.com/hotei/program "github.com/hotei/zipfile"
[4]: http://www.pkware.com/documents/casestudies/APPNOTE.TXT "pkware zip spec"

Comments can be sent to <hotei1352@gmail.com> or to user "hotei" at github.com.
License is BSD-two-clause, in file "LICENSE"

License
-------
The 'zipfile' go package/program is distributed under the Simplified BSD License:

> Copyright (c) 2015 David Rook. All rights reserved.
> 
> Redistribution and use in source and binary forms, with or without modification, are
> permitted provided that the following conditions are met:
> 
>    1. Redistributions of source code must retain the above copyright notice, this list of
>       conditions and the following disclaimer.
> 
>    2. Redistributions in binary form must reproduce the above copyright notice, this list
>       of conditions and the following disclaimer in the documentation and/or other materials
>       provided with the distribution.
> 
> THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDER ``AS IS'' AND ANY EXPRESS OR IMPLIED
> WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND
> FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL <COPYRIGHT HOLDER> OR
> CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
> CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
> SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
> ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
> NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF
> ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

Documentation (c) 2015 David Rook 

// EOF README.md  (this is a markdown document and tested OK with blackfriday)
