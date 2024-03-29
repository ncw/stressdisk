StressDisk
==========

This is a program designed to stress test your disks and find failures
in them.

Use it to soak test your new disks / memory cards / USB sticks before
trusting your valuable data to it.

Use it to soak test your new PC hardware also for the same reason.

Note that it turns out to be quite a sensitive memory tester too so
errors can sometimes be caused by bad RAM in your computer rather than
disk errors.

Install
-------

StressDisk is a Go program and comes as a single binary file.

Download the relevant binary from

- https://github.com/ncw/stressdisk/releases

Or alternatively if you have Go installed use

    go install github.com/ncw/stressdisk@latest

If you want to modify the sources, it is recommended to check out the repository.

    git clone https://github.com/ncw/stressdisk.git
    cd stressdisk
    go build .

You can then modify the source, rebuild as needed, and submit patches.

Usage
-----

Use `stressdisk -h` to see all the options.

```
Disk soak testing utility

Automatic usage:
  stressdisk run directory            - auto fill the directory up and soak test it
  stressdisk cycle directory          - fill, test, delete, repeat - torture for flash
  stressdisk clean directory          - delete the check files from the directory

Manual usage:
  stressdisk help                       - this help
  stressdisk [ -s size ] write filename - write a check file
  stressdisk read filename              - read the check file back
  stressdisk reads filename             - ... repeatedly for duration set
  stressdisk check filename1 filename2  - compare two check files
  stressdisk checks filename1 filename2 - ... repeatedly for duration set

Full options:
  -cpuprofile string
        Write cpu profile to file
  -duration duration
        Duration to run test (default 24h0m0s)
  -logfile string
        File to write log to set to empty to ignore (default "stressdisk.log")
  -maxerrors uint
        Max number of errors to print per file (default 64)
  -nodirect
        Don't use O_DIRECT
  -s int
        Size of the check files (default 1000000000)
  -stats duration
        Interval to print stats (default 1m0s)
  -statsfile string
        File to load/store statistics data (default "stressdisk_stats.json")

Note that flags must be provided BEFORE the stressdisk command, eg
  stressdisk -duration 48h run /mnt
```

Quickstart
----------

Install your new media in your computer and format it (make a filesystem on it).

Open a terminal (or cmd prompt if running Windows).

To check the disk:

    Linux: ./stressdisk run /media/nameofnewdisk
    Windows: stressdisk.exe run F:

Let it run for 24 hours.  It will finish on its own.  Note whether any errors
were reported.  Then use the following to remove the check files:

    Linux: ./stressdisk clean /media/nameofnewdisk
    Windows: stressdisk.exe clean F:

If you find errors, then you can use the `read` / `reads` / `check` /
`checks` sub-commands to investigate further.

    2012/09/20 22:23:20 Exiting after running for > 30s
    2012/09/20 22:23:20 
    Bytes read:         20778 MByte ( 692.59 MByte/s)
    Bytes written:          0 MByte (   0.00 MByte/s)
    Errors:                 0
    Elapsed time:  30.00033s
    
    2012/09/20 22:23:20 PASSED with no errors

Stress disk can be interrupted after it has written its check files
and it will continue from where it left off.

The default running time for stressdisk is 24h which is a sensible
minimum.  However if you want to run it for longer then use `-duration
48h` for instance.

Errors
------

If stressdisk finds an error it will print lines like this:

    2019/03/07 10:55:09 0AA00000: 2D, A1 diff 8C

The fields are `offset`, `file1 value`, `file 2 value` and the diff
which is `file1_value XOR file2_value` all in hexadecimal.  The diff
will be a binary number for a single bit error so `01`, `02`, `04`,
`08`, `10`, `20`, `40`, `80`.

This may give some insight into the problem (eg a single bit flipped),
or errors starting 4k boundaries, but may not.

However, the actual errors aren't that important, you shouldn't get
**any**. If you do then:

1. run [memtest86](https://www.memtest86.com/) on the machine for 48 hours to check for RAM problems, if this passes then
2. try the stressdisk test on another machine if you can, if this fails then
3. discard or return the media

If you didn't get to step 3. then you'll need to play with the
hardware of the machine, replace the RAM etc.  Stressdisk errors are
*usually* caused by bad media, but not always.  Bad RAM is a fairly
likely cause of stressdisk errors too.

Testing Flash
-------------

Stressdisk has a special mode which is good for giving flash / SSD
media a hard time.  The normal "run" test will fill the disk and read
the files back continually which a good test but doesn't torture flash
as much as it could as writing is a much more intensive operation for
flash than reading.

To test flash / SSD harder "cycle" mode does lots of write cycles as
well as read cycles. It works by filling the media with test files
verifying that the data is valid, deleting the test files, and
repeating the write + verify process continually.

**Caution**: This will be destructive to flash media if run long periods
of time, since flash devices have a limited number of writes per
sector/cell.

**This Is Intentional**!  You can use this to stress test flash harder.

You can also use this mode to find the breaking point of flash devices
to determine what the lifetime of the media is if you are quality
testing flash media before making a bulk buy.  The `-statsfile` option
is useful when doing this to save persistent stats to disk in case the
process is interrupted.

If you are merely interested in doing a less destructive test of the
flash device for data integrity, then should use the "run" mode, as
this mode only writes the check files once, and does reads operations
to verify data integrity which have little destructive penalty.


How it works
------------

Stressdisk fills up your disk with identical large files (1 GB by
default) full of random data.  It then randomly chooses a pair of
these and reads them back checking they are the same.

This causes the disk head to seek backwards and forwards across the
disk surface very quickly which is the worst possible access pattern
for disk drives and flushes out errors.

It seems to work equally well for non-rotating media.

The access patterns are designed so that your computer won't cache the
data being read off the disk so your computer will be forced to read
it off the disk.

Stressdisk uses OS specific commands to make sure the data isn't
cached in RAM so that you won't just be testing your computer RAM.

History
-------

I wrote the first version of stressdisk in about 1995 after
discovering that the CD I had just written at great expense had bit
errors in it.  I discovered that my very expensive SCSI disk was
returning occasional errors.

It has been used over the years to soak test 1000s of disks, memory
cards, usb sticks and found many with errors.  It has also found quite
a few memory errors (bad RAM).

The original stressdisk was written in C with a perl wrapper but it
was rather awkward to use because of that, so I re-wrote it in Go in
2012 as an exercise in learning Go and so that I could distribute it
in an easy to run single executable format.

License
-------

This is free software under the terms of MIT the license (check the
COPYING file included in this package).

Contact and support
-------------------

The project website is at:

- https://github.com/ncw/stressdisk

There you can file bug reports, ask for help or contribute patches.

Authors
-------

- Nick Craig-Wood <nick@craig-wood.com>
- Tomás Senart <tsenart@gmail.com>
- David Meador <dave@meadorresearch.com>

Contributors
------------

- Yves Junqueira for code review and helpful suggestions
- dcabro for reporting the windows empty partition issue
- Colin Lord for fixing documentation issues
- Your name goes here!
