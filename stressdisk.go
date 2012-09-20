// This checks a disc for errors.  It is also a sensitive memory tester
// (suprisingly) so if you get a fault using this program then it could
// be disc or RAM.
// 
// Nick Craig-Wood <nick@craig-wood.com>

/*
KEEP a state file - maybe number of rounds so could restart?

Estimate time to finish also

Only print PASSED if actually tested something!

No timeout on write file? Or read once?

Make LEAF be settable
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
	"sync"
	"syscall"
	"time"
)

const (
	SIZE    = 2 * 1024 * 1024 // size of block to do IO with
	RANLEN  = 55              // Magic constants (See Knuth: Seminumerical Algorithms)
	RANLEN2 = 24              // Do not change! (They are for a maximal length LFSR)

	LEAF = "TST_"
)

// Globals
var (
	// Flags
	fileSize      = flag.Int64("s", 1E9, "Size of the file to write")
	writeFile     = flag.Bool("w", false, "Write the check file")
	readFile      = flag.Bool("r", false, "Read the check file back in and check it")
	readFileLoop  = flag.Bool("R", false, "Read the check file back in and check it in a loop")
	checkFile     = flag.Bool("c", false, "Compare two check files")
	checkFileLoop = flag.Bool("C", false, "Compare two check files in a loop forever")
	auto          = flag.Bool("a", false, "Auto check file system")
	remove        = flag.Bool("d", false, "Delete check files")
	cpuprofile    = flag.String("cpuprofile", "", "write cpu profile to file")
	duration      = flag.Duration("duration", time.Hour*24, "Duration to run test")
	statsInterval = flag.Duration("stats", time.Minute*1, "Interval to print stats")
	logfile       = flag.String("logfile", "stressdisk.log", "File to write log to set to empty to ignore")
	stats         *Stats
	wg            sync.WaitGroup
)

type Random struct {
	extendedData []byte
	Data         []byte // A SIZE chunk of data which points to extendedData
	bytes        int    // number of bytes of randomness
	pos          int    // read position for Read
}

// Make a new random
func NewRandom() *Random {
	r := &Random{}
	r.extendedData = make([]byte, SIZE+RANLEN)
	r.Data = r.extendedData[0:SIZE]
	r.Data[0] = 1
	for i := 1; i < RANLEN; i++ {
		r.Data[i] = 0xA5
	}
	r.Randomise() // initial randomisation
	r.bytes = 0   // start buffer empty
	r.pos = 0
	return r
}

// Accumulate statistics
type Stats struct {
	read    uint64
	written uint64
	errors  uint64
	start   time.Time
}

// Initialise the stats
func NewStats() *Stats {
	stats := Stats{}
	stats.start = time.Now()
	return &stats
}

// Convert the stats to a string for printing
func (s *Stats) String() string {
	const MB = 1024 * 1024
	dt := time.Now().Sub(stats.start)
	dt_seconds := dt.Seconds()
	read_speed := 0.0
	write_speed := 0.0
	if dt > 0 {
		read_speed = float64(stats.read) / MB / dt_seconds
		write_speed = float64(stats.written) / MB / dt_seconds
	}
	return fmt.Sprintf(`
Bytes read:    %10d MByte (%7.2f MByte/s)
Bytes written: %10d MByte (%7.2f MByte/s)
Errors:        %10d
Elapsed time:  %v
`,
		stats.read / MB, read_speed,
		stats.written / MB, write_speed,
		stats.errors,
		dt)
}

// Output the stats to the log
func (s *Stats) Log() {
	log.Printf("%v\n", stats)
}

// Update the stats for bytes written
func (s *Stats) Written(bytes uint64) {
	s.written += bytes
}

// Update the stats for bytes read
func (s *Stats) Read(bytes uint64) {
	s.read += bytes
}

// Update the stats for errors
func (s *Stats) Errors(errors uint64) {
	s.errors += errors
}

// Fill the random block up with randomness
//
// This uses a random number generator from Knuth: Seminumerical
// Algorithms.  The magic numbers are the polynomial for a maximal
// length linear feedback shift register The least significant bits of
// the numbers form this sequence (of length 2**55).  The higher bits
// cause the sequence to be some multiple of this.
func (r *Random) Randomise() {
	// copy the old randomness to the end
	copy(r.extendedData[SIZE:], r.extendedData[0:RANLEN])

	// make a new random block
	d := r.extendedData
	for i := SIZE - 1; i >= 0; i-- {
		d[i] = d[i+RANLEN] + d[i+RANLEN2]
	}

	// Show we have some bytes
	r.bytes = SIZE
	r.pos = 0
}

// Implement io.Reader for Random
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

// Diff two byte blocks
func outputDiff(pos int64, a, b []byte) {
	if len(a) != len(b) {
		panic("Blocks passed to outputDiff must be the same length")
	}
	errors := uint64(0)
	for i := range a {
		if a[i] != b[i] {
			log.Printf("%08X: %02X, %02X diff %02X\n",
				pos+int64(i), b[i], a[i], b[i]^a[i])
			errors += 1
		}
	}
	stats.Errors(errors)
}

// Read a file in SIZE chunks until done
// Returns them in the channel
//
// This improves read speed from 62.3 MByte/s to 69 MByte/s in two files read
// and for random from 62 to 90 Mbyte/s
func blockReader(in io.Reader, file string) chan []byte {
	out := make(chan []byte, 1) // channel of size 1 so have one block in flight and one being read
	// FIXME not sure why 3 blocks needed - according to my
	// thinking 2 should be enough but there is obviously
	// something I've not thought of!
	block1 := make([]byte, SIZE)
	block2 := make([]byte, SIZE)
	block3 := make([]byte, SIZE)
	go func() {
		for {
			_, err := io.ReadFull(in, block1)
			if err != nil {
				if err == io.EOF {
					break
				}
				log.Fatalf("Error while reading %q: %s\n", file, err)
			}
			// FIXME bodge - don't account for reading from the random number
			if file != "random" {
				stats.Read(SIZE)
			}
			out <- block1
			block1, block2, block3 = block2, block3, block1
		}
		out <- nil
	}()
	return out
}

// Read the file
//
// Using blockReader improves speed from 62 MByte/s to 182 MByte/s
func read_file(file string) {
	in, err := os.Open(file)
	if err != nil {
		log.Fatalf("Failed to open %s for reading: %s\n", file, err)
	}
	defer in.Close()

	random := NewRandom()
	pos := int64(0)
	log.Printf("Reading file %q\n", file)

	// FIXME this is similar code to read_two_files
	ch1 := blockReader(in, file)
	ch2 := blockReader(random, "random")
	for {
		block1 := <-ch1
		block2 := <-ch2
		if block1 == nil {
			break
		}
		if bytes.Compare(block1, block2) != 0 {
			outputDiff(pos, block1, block2)
		}
		pos += SIZE
	}
}

// Write the file
//
// Returns a boolean whether the write failed
func write_file(file string, size int64) bool {
	out, err := os.Create(file)
	if err != nil {
		log.Fatalf("Couldn't open file %q for write: %s\n", file, err)
	}

	log.Printf("Writing file %q size %d\n", file, size)

	failed := false
	random := NewRandom()
	rnd := blockReader(random, "random")
	for size > 0 {
		_, err := out.Write(<-rnd)
		if err != nil {
			log.Printf("Error while writing %q\n", file)
			failed = true
			break
		}
		size -= SIZE
		stats.Written(SIZE)
	}

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

// Reads two files and checks them as it goes along
func read_two_files(file1, file2 string) {
	in1, err := os.Open(file1)
	if err != nil {
		log.Fatalf("Couldn't open file %q for read\n", file1)
	}
	defer in1.Close()

	in2, err := os.Open(file2)
	if err != nil {
		log.Fatalf("Couldn't open file %q for read\n", file2)
	}
	defer in2.Close()

	log.Printf("Reading file %q, %q\n", file1, file2)

	pos := int64(0)
	ch1 := blockReader(in1, file1)
	ch2 := blockReader(in2, file2)
	for {
		block1 := <-ch1
		block2 := <-ch2
		if block1 == nil || block2 == nil {
			if block1 != nil || block2 != nil {
				log.Fatal("Files %q and %q are different sizes", file1, file2)
			}
			break
		}
		if bytes.Compare(block1, block2) != 0 {
			outputDiff(pos, block1, block2)
		}
		pos += SIZE
	}
}

// bad syntax
func syntax_error() {
	fmt.Fprintf(os.Stderr, `Disk soak testing utility

Automatic usage:
  stressdisk -a directory            - auto fill the directory up and soak test it
  stressdisk -d directory            - delete the check files from the directory

Manual usage:
  stressdisk [ -s size ] -w filename - write a check file
  stressdisk -r filename             - read the check file back
  stressdisk -R filename             - ... repeatedly for duration set
  stressdisk -c filename1 filename2  - compare two check files
  stressdisk -C filename1 filename2  - ... repeatedly for duration set

Full options:
`)
	flag.PrintDefaults()
}

func checkArgs(args []string, n int, message string) {
	if len(args) != n {
		syntax_error()
		fmt.Fprintf(os.Stderr, "%d arguments required: %s\n", n, message)
		os.Exit(1)
	}
}

// Make a single round
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

// Find the check files in the directory passed in, returning all the files
func readDir(dir string) []string {
	matcher := regexp.MustCompile(`^` + regexp.QuoteMeta(LEAF) + `\d{4,}$`)
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Fatalf("Couldn't read directory %q: %s\n", dir, err)
	}
	files := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.Mode()&os.ModeType == 0 && matcher.MatchString(name) {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files
}

// Delete the check files
func deleteFiles(files []string) {
	log.Printf("Removing %d check files\n", len(files))
	for _, file := range files {
		log.Printf("Removing file %q\n", file)
		err := os.Remove(file)
		if err != nil {
			log.Printf("Failed to remove file %q: %s\n", file, err)
		}
	}
}

// Write the check files
func writeFiles(dir string) []string {
	files := make([]string, 0)
	for i := 0; ; i++ {
		file := filepath.Join(dir, fmt.Sprintf("%s%04d", LEAF, i))
		if write_file(file, *fileSize) {
			break
		}
		files = append(files, file)
	}
	if len(files) < 2 {
		deleteFiles(files)
		log.Fatalf("Only generated %d files which isn't enough - reduce the size with -s\n", len(files))
	}
	return files
}

// Read the check files or create new ones
func getFiles(dir string) []string {
	files := readDir(dir)
	if len(files) == 0 {
		log.Printf("No check files - generating\n")
		files = writeFiles(dir)
	} else {
		log.Printf("%d check files found - restarting\n", len(files))
	}
	return files
}

// Initialise the stats and stats printing
func initialiseStats() chan bool {
	stats = NewStats()
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, syscall.SIGINT)
	stats_print := time.Tick(*statsInterval)
	timeout := time.Tick(*duration)
	finished := make(chan bool)
	go func() {
		wg.Add(1)
		defer wg.Done()
		done := false
		for !done {
			select {
			case <-sigint:
				log.Printf("Interrupt received\n")
				done = true
			case <-finished:
				log.Printf("Finished\n")
				done = true
			case <-timeout:
				log.Printf("Exiting after running for > %v", duration)
				done = true
			case <-stats_print:
				stats.Log()
			}
		}
		stats.Log()
		if stats.errors != 0 {
			log.Fatalf("FAILED with %d errors - see %q for details", stats.errors, *logfile)
		}
		log.Println("PASSED with no errors")
		os.Exit(0)
	}()
	return finished
}

func main() {
	flag.Usage = syntax_error
	flag.Parse()
	args := flag.Args()
	finished := initialiseStats()
	runtime.GOMAXPROCS(3)

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

	action := func() bool {
		return false
	}
	switch {
	case *writeFile:
		checkArgs(args, 1, "Need file to write")
		action = func() bool {
			write_file(args[0], *fileSize)
			return false
		}
	case *checkFile || *checkFileLoop:
		checkArgs(args, 2, "Need two files to read")
		action = func() bool {
			read_two_files(args[0], args[1])
			return *checkFileLoop
		}
	case *readFile || *readFileLoop:
		checkArgs(args, 1, "Need file to read")
		action = func() bool {
			read_file(args[0])
			return *readFileLoop
		}
	case *remove:
		checkArgs(args, 1, "Need directory to delete files from")
		action = func() bool {
			dir := args[0]
			deleteFiles(readDir(dir))
			return false
		}
	case *auto:
		// FIXME directory could be omitted?
		// FIXME should be default
		checkArgs(args, 1, "Need directory to write check files")
		dir := args[0]
		files := getFiles(dir)
		action = func() bool {
			a, b := pairs(len(files))
			for i := range a {
				read_two_files(files[a[i]], files[b[i]])
			}
			return true
		}
	default:
		syntax_error()
		os.Exit(1)
	}

	// Run the action
	for round := 0; ; round++ {
		log.Printf("Starting round %d\n", round+1)
		if !action() {
			break
		}
	}

	finished <- true
	wg.Wait() // wait for everything else to finish
}
