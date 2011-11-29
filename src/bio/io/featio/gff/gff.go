// Package to read and write GFF format files
package gff
// Copyright ©2011 Dan Kortschak <dan.kortschak@adelaide.edu.au>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/kortschak/BioGo/bio"
	"github.com/kortschak/BioGo/bio/feat"
	"github.com/kortschak/BioGo/bio/io/seqio/fasta"
	"github.com/kortschak/BioGo/bio/seq"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	seqName = iota
	source
	feature
	start
	end
	score
	strand
	frame
	attributes
	comments
)

var (
	DefaultVersion                    = 2
	DefaultToOneBased                 = true
	strandToChar      map[int8]string = map[int8]string{1: "+", 0: ".", -1: "-"}
	charToStrand      map[string]int8 = map[string]int8{"+": 1, ".": 0, "-": -1}
)

// GFF format reader type.
type Reader struct {
	f             io.ReadCloser
	r             *bufio.Reader
	Version       int
	OneBased      bool
	SourceVersion []byte
	Date          *time.Time
	TimeFormat    string // required for parsing date fields
	Type          byte
}

// Returns a new GFF format reader using f.
func NewReader(f io.ReadCloser) *Reader {
	return &Reader{
		f:        f,
		r:        bufio.NewReader(f),
		OneBased: DefaultToOneBased,
	}
}

// Returns a new GFF reader using a filename.
func NewReaderName(name string) (r *Reader, err error) {
	var f *os.File
	if f, err = os.Open(name); err != nil {
		return
	}
	return NewReader(f), nil
}

func (self *Reader) commentMetaline(line []byte) (f *feat.Feature, err error) {
	// Load these into a slice in a MetaField of the Feature
	fields := strings.Split(string(line), " ")
	switch fields[0] {
	case "gff-version":
		if self.Version, err = strconv.Atoi(fields[1]); err != nil {
			self.Version = DefaultVersion
		}
	case "source-version":
		if len(fields) > 1 {
			self.SourceVersion = []byte(strings.Join(fields[1:], " "))
		} else {
			return nil, bio.NewError("Incomplete source-version metaline", 0, fields)
		}
	case "date":
		if len(fields) > 1 {
			self.Date, err = time.Parse(self.TimeFormat, strings.Join(fields[1:], " "))
		} else {
			return nil, bio.NewError("Incomplete date metaline", 0, fields)
		}
	case "Type":
		if len(fields) > 1 {
			self.Type = bio.StringToType[fields[1]] // self.Type should be a map[string]byte to allow for extend type defs
		} else {
			return nil, bio.NewError("Incomplete Type metaline", 0, fields)
		}
	case "sequence-region":
		if len(fields) > 3 {
			var start, end int
			if start, err = strconv.Atoi(fields[2]); err != nil {
				return nil, err
			}
			if end, err = strconv.Atoi(fields[3]); err != nil {
				return nil, err
			}
			f = &feat.Feature{
				Meta: &feat.Feature{
					ID:    []byte(fields[1]),
					Start: start,
					End:   end,
				},
			}
		} else {
			return nil, bio.NewError("Incomplete sequence-region metaline", 0, fields)
		}
	case "DNA", "RNA", "Protein":
		if len(fields) > 1 {
			var s *seq.Seq
			if s, err = self.metaSequence(fields[0], fields[1]); err != nil {
				return
			} else {
				f = &feat.Feature{Meta: s}
			}
		} else {
			return nil, bio.NewError("Incomplete sequence metaline", 0, fields)
		}
	default:
		f = &feat.Feature{Meta: line}
	}

	return
}

func (self *Reader) metaSequence(moltype, id string) (sequence *seq.Seq, err error) {
	var line, body []byte

	for {
		if line, err = self.r.ReadBytes('\n'); err == nil {
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if len(line) == 0 {
				continue
			}
			if len(line) < 2 || !bytes.HasPrefix(line, []byte("##")) {
				return nil, bio.NewError("Corrupt metasequence", 0, line)
			}
			line = bytes.TrimSpace(line[2:])
			if string(line) == "end-"+moltype {
				break
			} else {
				line = bytes.Join(bytes.Fields(line), nil)
				body = append(body, line...)
			}
		} else {
			return nil, err
		}
	}

	sequence = seq.New([]byte(id), body, nil)
	sequence.Moltype = bio.StringToType[moltype]

	return
}

// Read a single feature or part and return it or an error.
func (self *Reader) Read() (f *feat.Feature, err error) {
	var (
		line  []byte
		elems [][]byte
		s     int8
		ok    bool
	)

	for {
		if line, err = self.r.ReadBytes('\n'); err == nil {
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			line = bytes.TrimSpace(line)
			if len(line) == 0 { // ignore blank lines
				continue
			} else if bytes.HasPrefix(line, []byte("##")) {
				f, err = self.commentMetaline(line[2:])
				return
			} else if line[0] != '#' { // ignore comments
				elems = bytes.SplitN(line, []byte{'\t'}, 10)
				break
			}
		} else {
			return
		}
	}

	if s, ok = charToStrand[string(elems[strand])]; !ok {
		s = 0
	}

	startPos, se := strconv.Atoi(string(elems[start]))
	if se != nil {
		startPos = 0
	}
	if self.OneBased && startPos > 0 {
		startPos--
	}

	endPos, se := strconv.Atoi(string(elems[end]))
	if se != nil {
		endPos = 0
	}

	fr, se := strconv.Atoi(string(elems[frame]))
	if se != nil {
		fr = -1
	}

	score, se := strconv.Atof64(string(elems[score]))
	if se != nil {
		score = 0
	}

	f = &feat.Feature{
		Location: elems[seqName],
		Source:   elems[source],
		Start:    startPos,
		End:      endPos,
		Feature:  elems[feature],
		Score:    score,
		Frame:    int8(fr),
		Strand:   s,
		Moltype:  self.Type, // currently we default to bio.DNA
	}

	if len(elems) > attributes {
		f.Attributes = elems[attributes]
	}
	if len(elems) > comments {
		f.Comments = elems[comments]
	}

	return
}

// Rewind the reader.
func (self *Reader) Rewind() (err error) {
	if s, ok := self.f.(io.Seeker); ok {
		_, err = s.Seek(0, 0)
	} else {
		err = bio.NewError("Not a Seeker", 0, self)
	}
	return
}

// Close the reader.
func (self *Reader) Close() (err error) {
	return self.f.Close()
}

// GFF format writer type.
type Writer struct {
	f           io.WriteCloser
	w           *bufio.Writer
	Version     int
	OneBased    bool
	FloatFormat byte
	Precision   int
	Width       int
}

// Returns a new GFF format writer using f.
// When header is true, a version header will be written to the GFF.
func NewWriter(f io.WriteCloser, v, width int, header bool) (w *Writer) {
	w = &Writer{
		f:           f,
		w:           bufio.NewWriter(f),
		Version:     v,
		OneBased:    DefaultToOneBased,
		FloatFormat: bio.FloatFormat,
		Precision:   bio.Precision,
		Width:       width,
	}

	if header {
		w.WriteMetaData(fmt.Sprintf("gff-version %d", v))
	}

	return
}

// Returns a new GFF format writer using a filename, truncating any existing file.
// If appending is required use NewWriter and os.OpenFile.
// When header is true, a version header will be written to the GFF.
func NewWriterName(name string, v, width int, header bool) (w *Writer, err error) {
	var f *os.File
	if f, err = os.Create(name); err != nil {
		return
	}
	return NewWriter(f, v, width, header), nil
}

// Write a single feature and return the number of bytes written and any error.
func (self *Writer) Write(f *feat.Feature) (n int, err error) {
	return self.w.WriteString(self.String(f) + "\n")
}

// Convert a feature to a string.
func (self *Writer) String(f *feat.Feature) (line string) {
	start := f.Start
	if self.OneBased && start >= 0 {
		start++
	}
	line = string(f.Location) + "\t" +
		string(f.Source) + "\t" +
		string(f.Feature) + "\t" +
		strconv.Itoa(start) + "\t" +
		strconv.Itoa(f.End) + "\t" +
		strconv.Ftoa64(f.Score, self.FloatFormat, self.Precision) + "\t"

	if f.Moltype == bio.DNA {
		line += strandToChar[f.Strand] + "\t"
	} else {
		line += ".\t"
	}

	if frame := strconv.Itoa(int(f.Frame)); (f.Moltype == bio.DNA || self.Version < 2) && (frame == "0" || frame == "1" || frame == "2") {
		line += frame + "\t"
	} else {
		line += ".\t"
	}
	line += string(f.Attributes)
	if len(f.Comments) > 0 {
		line += " #" + string(f.Comments)
	}

	return
}

// Write meta data to a GFF file.
func (self *Writer) WriteMetaData(d interface{}) (n int, err error) {
	switch d.(type) {
	case []byte, string:
		n, err = self.w.WriteString("##" + d.(string) + "\n")
	case *seq.Seq:
		sw := fasta.NewWriter(self.f, self.Width)
		sw.IDPrefix = "##" + d.(*seq.Seq).MoltypeAsString() + " "
		sw.SeqPrefix = "##"
		n, err = sw.Write(d.(*seq.Seq))
	case *feat.Feature:
		start := d.(*feat.Feature).Start
		if self.OneBased && start >= 0 {
			start++
		}
		n, err = self.w.WriteString("##sequence-region " + string(d.(*feat.Feature).ID) + " " +
			strconv.Itoa(start) + " " +
			strconv.Itoa(d.(*feat.Feature).End) + "\n")
	}

	return
}

// Write a comment line to a GFF file
func (self *Writer) WriteComment(c string) (n int, err error) {
	n, err = self.w.WriteString("# " + c + "\n")

	return
}

// Close the writer, flushing any unwritten data.
func (self *Writer) Close() (err error) {
	if err = self.w.Flush(); err != nil {
		return
	}
	return self.f.Close()
}
