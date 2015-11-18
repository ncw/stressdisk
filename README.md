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

- http://www.craig-wood.com/nick/pub/stressdisk/

Or alternatively if you have Go installed use

    go get github.com/ncw/stressdisk

and this will build the binary in `$GOPATH/bin`.  You can then modify
the source and submit patches.

Usage
-----

Use `stressdisk -h` to see all the options.

    Disk soak testing utility
    
    Automatic usage:
      stressdisk run directory            - auto fill the directory up and soak test it
      stressdisk flash directory          - auto fill the directory up and stress test it
      stressdisk clean directory          - delete the check files from the directory
    
    Manual usage:
      stressdisk help                       - this help
      stressdisk [ -s size ] write filename - write a check file
      stressdisk [-statsenable=1] [ -statsfile=stats-filename ] flash directory 
                                            - load/store persistent stats file
      stressdisk read filename              - read the check file back
      stressdisk reads filename             - ... repeatedly for duration set
      stressdisk check filename1 filename2  - compare two check files
      stressdisk checks filename1 filename2 - ... repeatedly for duration set
    
    Full options:
      -cpuprofile="": write cpu profile to file
      -duration=24h0m0s: Duration to run test
      -logfile="stressdisk.log": File to write log to set to empty to ignore
      -s=1000000000: Size of the file to write
      -stats=1m0s: Interval to print stats
      -statsenable=1: Enable statistics data on disk
      -statsfile="example-file": Filename to load/store statistics data on disk
      -statsdir="example-dir": Path to load/store statistics data on disk



Quickstart
----------

Install your new media in your computer and format it (make a filesystem on it).

Open a terminal (or cmd prompt if running Windows)

To check the disk

    Linux: ./stressdisk run /media/nameofnewdisk
    Windows: stressdisk.exe run F:

Let run for 24 hours.  Note whether any errors were reported.  Then use

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

Contributors
------------

- Yves Junqueira for code review and helpful suggestions
- dcabro for reporting the windows empty partition issue
- Your name goes here!
