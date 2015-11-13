// This checks a disc for errors.  It is also a sensitive memory tester
// (suprisingly) so if you get a fault using this program then it could
// be disc or RAM.
//
// Nick Craig-Wood <nick@craig-wood.com>

/*
KEEP a state file - maybe number of rounds so could restart?

Estimate time to finish also

No timeout on write file? Or read once?

Make LEAF be settable

Make blockReader not Fatal error if there is a problem - would then
need to make sure all the goroutines were killed off properly
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ncw/directio"
)

const (
	MB        = 1024 * 1024 // Bytes in a Megabyte
	BlockSize = 2 * MB      // size of block to do IO with
	ranlen    = 55          // Magic constants (See Knuth: Seminumerical Algorithms)
	ranlen2   = 24          // Do not change! (They are for a maximal length LFSR)
	Leaf      = "TST_"      // name of the check files
)

// Globals
var (
	// Flags
	fileSize      = flag.Int64("s", 1E9, "Size of the check files")
	cpuprofile    = flag.String("cpuprofile", "", "Write cpu profile to file")
	duration      = flag.Duration("duration", time.Hour*24, "Duration to run test")
	statsInterval = flag.Duration("stats", time.Minute*1, "Interval to print stats")
	logfile       = flag.String("logfile", "stressdisk.log", "File to write log to set to empty to ignore")
	maxErrors     = flag.Uint64("maxerrors", 64, "Max number of errors to print per file")
	stats         *Stats
)

// enum program mode
const (
	modeNONE = iota
	modeREAD 
	modeREAD_DONE
	modeWRITE
	modeWRITE_DONE
)

// Random contains the state for the random stream generator
type Random struct {
	extendedData []byte
	Data         []byte // A BlockSize chunk of data which points to extendedData
	bytes        int    // number of bytes of randomness
	pos          int    // read position for Read
}

// NewRandom make a new random stream generator
func NewRandom() *Random {
	r := &Random{}
	r.extendedData = make([]byte, BlockSize+ranlen)
	r.Data = r.extendedData[0:BlockSize]
	r.Data[0] = 1
	for i := 1; i < ranlen; i++ {
		r.Data[i] = 0xA5
	}
	r.Randomise() // initial randomisation
	r.bytes = 0   // start buffer empty
	r.pos = 0
	return r
}

// Stats stores accumulated statistics
type Stats struct {
	read         uint64
	written      uint64
	errors       uint64
	start        time.Time
	readStart    time.Time  // start of unaccumulated time of read operation
	readSeconds  float64    // read seconds accumulator
	writeStart   time.Time  // start of unaccumulated time of write operation
	writeSeconds float64    // write seconds accumulator
	mode         int // current mode - modeNONE, modeREAD, modeWRITE
}

// NewStats cretates an initialised Stats
func NewStats() *Stats {
	return &Stats{start: time.Now()}
}


// Be sure to transition from for example modeREAD to mode_READ_DONE, 
// before transitioning to mode_WRITE, otherwise you will lose the 
// corresponding time statistic in the time accumulator.
// Also you must for enter modeREAD before transition to modeREAD_DONE.
func (s *Stats) SetMode(mode int) {
	s.mode = mode
	switch  s.mode	{
	default:
	case modeNONE:
		fallthrough
	case modeREAD:
		s.readStart = time.Now()
		fallthrough
	case modeREAD_DONE:
		s.readSeconds += time.Now().Sub(s.readStart).Seconds()
		fallthrough
	case modeWRITE:
		s.writeStart = time.Now()
		fallthrough
	case modeWRITE_DONE:
		s.writeSeconds += time.Now().Sub(s.writeStart).Seconds()
	}
}

// String convert the Stats to a string for printing
func (s *Stats) String() string {
	dt := time.Now().Sub(s.start)  // total elapsed time
	read, written := atomic.LoadUint64(&s.read), atomic.LoadUint64(&s.written)
	read_speed, write_speed := 0.0, 0.0

	// calculate interim duration - for periodic stats display
	// while operation is not completed.
	switch s.mode  { 
	default:
	case modeNONE:
		// do nothing
		fallthrough
	case modeREAD:
		s.readSeconds += time.Now().Sub(s.readStart).Seconds();
		s.readStart = time.Now()
		fallthrough
	case modeWRITE:
		s.writeSeconds += time.Now().Sub(s.writeStart).Seconds()
		s.writeStart = time.Now()
	}

	if(s.readSeconds != 0)	{
		read_speed = float64(read) / MB / s.readSeconds
	}
	if(s.writeSeconds != 0)	{
		write_speed = float64(written) / MB / s.writeSeconds
	}

	return fmt.Sprintf(`
Bytes read:    %10d MByte (%7.2f MByte/s)
Bytes written: %10d MByte (%7.2f MByte/s)
Errors:        %10d
Elapsed time:  %v
`,
		read/MB, read_speed,
		written/MB, write_speed,
		atomic.LoadUint64(&s.errors),
		dt)
}

// Log outputs the Stats to the log
func (s *Stats) Log() {
	log.Println(stats)
}

// Written updates the stats for bytes written
func (s *Stats) Written(bytes uint64) {
	atomic.AddUint64(&s.written, bytes)
}

// Read updates the stats for bytes read
func (s *Stats) Read(bytes uint64) {
	atomic.AddUint64(&s.read, bytes)
}

// Errors updates the stats for errors
func (s *Stats) Errors(errors uint64) {
	atomic.AddUint64(&s.errors, errors)
}

// Randomise fills the random block up with randomness.
//
// This uses a random number generator from Knuth: Seminumerical
// Algorithms.  The magic numbers are the polynomial for a maximal
// length linear feedback shift register The least significant bits of
// the numbers form this sequence (of length 2**55).  The higher bits
// cause the sequence to be some multiple of this.
func (r *Random) Randomise() {
	// copy the old randomness to the end
	copy(r.extendedData[BlockSize:], r.extendedData[0:ranlen])

	// make a new random block
	d := r.extendedData
	for i := BlockSize - 1; i >= 0; i-- {
		d[i] = d[i+ranlen] + d[i+ranlen2]
	}

	// Show we have some bytes
	r.bytes = BlockSize
	r.pos = 0
}

// Read implements io.Reader for Random
func (r *Random) Read(p []byte) (int, error) {
	bytes_to_write := len(p)
	bytes_written := 0
	for bytes_to_write > 0 {
		if r.bytes <= 0 {
			r.Randomise()
		}
		chunk_size := bytes_to_write
		if bytes_to_write >= r.bytes {
			chunk_size = r.bytes
		}
		copy(p[bytes_written:bytes_written+chunk_size], r.Data[r.pos:r.pos+chunk_size])
		bytes_written += chunk_size
		bytes_to_write -= chunk_size
		r.pos += chunk_size
		r.bytes -= chunk_size
	}
	return bytes_written, nil
}

// outputDiff checks two blocks and outputs differences to the log
func outputDiff(pos int64, a, b []byte, output bool) bool {
	if len(a) != len(b) {
		panic("Assertion failed: Blocks passed to outputDiff must be the same length")
	}
	errors := uint64(0)
	for i := range a {
		if a[i] != b[i] {
			if output {
				if errors < *maxErrors {
					log.Printf("%08X: %02X, %02X diff %02X\n",
						pos+int64(i), b[i], a[i], b[i]^a[i])
				} else if errors >= *maxErrors {
					log.Printf("Error limit %d reached: not printing any more differences in this file\n", *maxErrors)
					output = false
				}
			}
			errors += 1
		}
	}
	stats.Errors(errors)
	return output
}

// BlockReader contains the state for reading blocks out of the file
type BlockReader struct {
	// Input file
	in io.Reader
	// Name of input file
	file string
	// Channel to output blocks
	out chan []byte
	// Channel for spare blocks
	spare chan []byte
	// Channel to signal quit
	quit chan bool
	// Co-ordinate with background goroutine
	wg sync.WaitGroup
}

// blockReader reads a file in BlockSize using BlockSize chunks until done.
//
// It returns them in the channel using a triple buffered goroutine to
// do the reading so as to parallelise the IO.
//
// This improves read speed from 62.3 MByte/s to 69 MByte/s when
// reading two files and and from 62 to 182 Mbyte/s when reading from
// one file and one random source.
func NewBlockReader(in io.Reader, file string) *BlockReader {
	br := &BlockReader{
		in:    in,
		file:  file,
		out:   make(chan []byte, 1),
		spare: make(chan []byte, 4),
		quit:  make(chan bool, 1), // buffer of size 1 so can send into it without blocking
	}
	// Run the reader in the background
	br.wg.Add(1)
	go br.background()
	return br
}

// background routine for BlockReader to read the blocks in the
// background into the channel
//
// It uses triple buffering with chan of length 1. One block being
// filled, one block in the channel, and one block being used by the
// client.
func (br *BlockReader) background() {
	defer br.wg.Done()
	defer close(br.out)
	br.spare <- directio.AlignedBlock(BlockSize)
	br.spare <- directio.AlignedBlock(BlockSize)
	br.spare <- directio.AlignedBlock(BlockSize)
	for {
		block := <-br.spare
		_, err := io.ReadFull(br.in, block)
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Fatalf("Error while reading %q: %s\n", br.file, err)
		}
		// FIXME bodge - don't account for reading from the random number
		if br.file != "random" {
			stats.Read(BlockSize)
		}
		select {
		case br.out <- block:
		case <-br.quit:
			return
		}
	}
}

// Read a block from the BlockReader
//
// Returns nil at end of file
func (br *BlockReader) Read() []byte {
	out, _ := <-br.out
	return out
}

// Return a block to the BlockReader when processed for re-use
//
// Ignores a nil block
func (br *BlockReader) Return(block []byte) {
	if block != nil {
		br.spare <- block
	}
}

// Close the BlockReader shuttting down the background goroutine
func (br *BlockReader) Close() {
	br.quit <- true
	br.wg.Wait()
}

// ReadFile reads the file given and checks it against the random source
func ReadFile(file string) {
	in, err := directio.OpenFile(file, os.O_RDONLY, 0666)
	if err != nil {
		log.Fatalf("Failed to open %s for reading: %s\n", file, err)
	}
	defer in.Close()

	random := NewRandom()
	pos := int64(0)
	log.Printf("Reading file %q\n", file)

	// FIXME this is similar code to ReadTwoFiles
	br1 := NewBlockReader(in, file)
	defer br1.Close()
	br2 := NewBlockReader(random, "random")
	defer br2.Close()
	output := true
	for {
		block1 := br1.Read()
		block2 := br2.Read()
		if block1 == nil {
			break
		}
		if bytes.Compare(block1, block2) != 0 {
			output = outputDiff(pos, block1, block2, output)
		}
		pos += BlockSize
		br1.Return(block1)
		br2.Return(block2)
	}
}

// WriteFile writes the random source for size bytes to the file given
//
// Returns a true if the write failed, false otherwise.
func WriteFile(file string, size int64) bool {
	out, err := directio.OpenFile(file, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Couldn't open file %q for write: %s\n", file, err)
	}

	log.Printf("Writing file %q size %d\n", file, size)

	failed := false
	random := NewRandom()
	br := NewBlockReader(random, "random")
	defer br.Close()
	stats.SetMode(modeWRITE)
	for size > 0 {
		block := br.Read()
		_, err := out.Write(block)
		br.Return(block)
		if err != nil {
			log.Printf("Error while writing %q\n", file)
			failed = true
			break
		}
		size -= BlockSize
		stats.Written(BlockSize)
	}
	stats.SetMode(modeWRITE_DONE)

	out.Close()
	if failed {
		log.Printf("Removing incomplete file %q\n", file)
		err = os.Remove(file)
		if err != nil {
			log.Fatalf("Failed to remove incomplete file %q: %s\n", file, err)
		}
	}
	return failed
}

// ReadTwoFiles reads two files and checks them to be the same as it goes along.
//
// It reads the files in BlockSize chunks.
func ReadTwoFiles(file1, file2 string) {
	in1, err := directio.OpenFile(file1, os.O_RDONLY, 0666)
	if err != nil {
		log.Fatalf("Couldn't open file %q for read\n", file1)
	}
	defer in1.Close()

	in2, err := directio.OpenFile(file2, os.O_RDONLY, 0666)
	if err != nil {
		log.Fatalf("Couldn't open file %q for read\n", file2)
	}
	defer in2.Close()

	log.Printf("Reading file %q, %q\n", file1, file2)

	stats.SetMode(modeREAD)
	pos := int64(0)
	br1 := NewBlockReader(in1, file1)
	defer br1.Close()
	br2 := NewBlockReader(in2, file2)
	defer br2.Close()
	output := true
	for {
		block1 := br1.Read()
		block2 := br2.Read()
		if block1 == nil || block2 == nil {
			if block1 != nil || block2 != nil {
				log.Fatalf("Files %q and %q are different sizes\n", file1, file2)
			}
			break
		}
		if bytes.Compare(block1, block2) != 0 {
			output = outputDiff(pos, block1, block2, output)
		}
		pos += BlockSize
		br1.Return(block1)
		br2.Return(block2)
	}
	stats.SetMode(modeREAD_DONE)
}

// syntaxError prints the syntax
func syntaxError() {
	fmt.Fprintf(os.Stderr, `Disk soak testing utility

Automatic usage:
  stressdisk run directory            - auto fill the directory up and soak test it
  stressdisk clean directory          - delete the check files from the directory

Manual usage:
  stressdisk help                       - this help
  stressdisk [ -s size ] write filename - write a check file
  stressdisk read filename              - read the check file back
  stressdisk reads filename             - ... repeatedly for duration set
  stressdisk check filename1 filename2  - compare two check files
  stressdisk checks filename1 filename2 - ... repeatedly for duration set

Full options:
`)
	flag.PrintDefaults()
}

// Exit with the message
func fatalf(message string, args ...interface{}) {
	syntaxError()
	fmt.Fprintf(os.Stderr, message, args...)
	os.Exit(1)
}

// checkArgs checks there are enough arguments and prints a message if not
func checkArgs(args []string, n int, message string) {
	if len(args) != n {
		fatalf("%d arguments required: %s\n", n, message)
	}
}

// pairs is used to make the schedule for testing pairs of files.
//
// This reads all the data twice in a random order
func pairs(n int) (a, b []int) {
	a = rand.Perm(n)
OUTER:
	for {
		b = rand.Perm(n)
		if n > 1 {
			// Make sure that we don't read the same block twice at the same time
			for i := range a {
				if a[i] == b[i] {
					continue OUTER
				}
			}
		}
		break
	}
	return
}

// ReadDir finds the check files in the directory passed in, returning all the files
func ReadDir(dir string) []string {
	matcher := regexp.MustCompile(`^` + regexp.QuoteMeta(Leaf) + `\d{4,}$`)
	files := make([]string, 0)
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		// Warn only if couldn't open directory
		// See: http://code.google.com/p/go/issues/detail?id=4601
		log.Printf("Couldn't read directory %q: %s\n", dir, err)
		return files
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.Mode()&os.ModeType == 0 && matcher.MatchString(name) {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files
}

// DeleteFiles deletes all the check files
func DeleteFiles(files []string) {
	log.Printf("Removing %d check files\n", len(files))
	for _, file := range files {
		log.Printf("Removing file %q\n", file)
		err := os.Remove(file)
		if err != nil {
			log.Printf("Failed to remove file %q: %s\n", file, err)
		}
	}
}

// WriteFiles writes check files until the disk is full
func WriteFiles(dir string) []string {
	files := make([]string, 0)
	for i := 0; ; i++ {
		file := filepath.Join(dir, fmt.Sprintf("%s%04d", Leaf, i))
		if WriteFile(file, *fileSize) {
			break
		}
		files = append(files, file)
	}
	if len(files) < 2 {
		DeleteFiles(files)
		log.Fatalf("Only generated %d files which isn't enough - reduce the size with -s\n", len(files))
	}
	return files
}

// GetFiles finds existing check files or created new ones
func GetFiles(dir string) []string {
	files := ReadDir(dir)
	if len(files) == 0 {
		log.Printf("No check files - generating\n")
		files = WriteFiles(dir)
	} else {
		log.Printf("%d check files found - restarting\n", len(files))
	}
	return files
}

// finished prints the message and some stats then exits with the correct error code
func finished(message string) {
	log.Println(message)
	stats.Log()
	// Log pass / fail if we did any testing
	log.Printf("Bytes read:    %10d MByte", stats.read / MB)
	log.Printf("Bytes written: %10d MByte", stats.written / MB)
	if stats.read != 0 {
		if stats.errors != 0 {
			log.Fatalf("FAILED with %d errors - see %q for details", stats.errors, *logfile)
		}
		log.Println("PASSED with no errors")
	}
	os.Exit(0)
}

func main() {
	flag.Usage = syntaxError
	flag.Parse()
	args := flag.Args()
	stats = NewStats()
	runtime.GOMAXPROCS(3)
	rand.Seed(time.Now().UnixNano())

	// Setup profiling if desired
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Write to log file as well
	if len(*logfile) > 0 {
		fd, err := os.OpenFile(*logfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			log.Fatal("Failed to open log file")
		}
		defer func() {
			fd.WriteString("\n------\n")
			fd.Close()
		}()
		log.SetOutput(io.MultiWriter(os.Stderr, fd))
	}

	if len(args) < 1 {
		fatalf("No command supplied\n")
	}
	command := strings.ToLower(args[0])
	args = args[1:]
	var action func() bool
	switch command {
	case "write":
		checkArgs(args, 1, "Need file to write")
		action = func() bool {
			WriteFile(args[0], *fileSize)
			return false
		}
	case "check", "checks":
		checkArgs(args, 2, "Need two files to read")
		action = func() bool {
			ReadTwoFiles(args[0], args[1])
			return command == "checks"
		}
	case "read", "reads":
		checkArgs(args, 1, "Need file to read")
		action = func() bool {
			ReadFile(args[0])
			return command == "reads"
		}
	case "clean":
		checkArgs(args, 1, "Need directory to delete files from")
		action = func() bool {
			dir := args[0]
			DeleteFiles(ReadDir(dir))
			return false
		}
	case "run":
		// FIXME directory could be omitted?
		// FIXME should be default
		checkArgs(args, 1, "Need directory to write check files")
		dir := args[0]
		files := GetFiles(dir)
		action = func() bool {
			a, b := pairs(len(files))
			for i := range a {
				ReadTwoFiles(files[a[i]], files[b[i]])
			}
			return true
		}
	case "flash":
		checkArgs(args, 1, "Need directory to perform testing")
		dir := args[0]
		files := ReadDir(dir)
		DeleteFiles(files)
		action = func() bool {
			for j := 0; j < 1000000; j++ { // N* 1,000,000 GB || N* 1,000 TB
				files := GetFiles(dir)
				a, b := pairs(len(files))
				for i := range a {
					ReadTwoFiles(files[a[i]], files[b[i]])
				}
				DeleteFiles(files)
			}
			return false
		}


	default:
		fatalf("Command %q not understood\n", command)
	}

	// Exit on keyboard interrrupt
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT)
		<-ch
		finished("Interrupt received")
	}()

	// Exit on timeout
	go func() {
		<-time.After(*duration)
		finished(fmt.Sprintf("Exiting after running for > %v", duration))
	}()

	// Print the stats every statsInterval
	go func() {
		ch := time.Tick(*statsInterval)
		for {
			<-ch
			stats.Log()
		}
	}()

	// Run the action
	for round := 0; ; round++ {
		log.Printf("Starting round %d\n", round+1)
		if !action() {
			break
		}
	}

	finished("All done")
}
