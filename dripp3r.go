/*
dripp3r is a simple terminal program for feeding GCode into a Marlin-firmware
3D printer's serial port.

Please note, this has only been tested on Windows and only with Marlin v1.0.6.

Provide the serial port name/path as second argument and the path to the file
containing Gcode as the second argument.

The GCode sent to the printer is printed as it is sent. Any response other than
ok is printed as well. This is spammy yet also, in a strange way, soothing.  On
Windows, you can pause printing by pressing the "pause" button on your
keyboard.  I recall that Ctrl-S & Ctrl-Q traditionally perform the same
function in UNIX/Linux (untested).

A simple menu can be accessed by pressing Ctrl-C. While the menu is shown,
sending GCode to the printer is paused. Press Ctrl-C a second time to exit the
program abruptly. This will stop sending instructions to the printer.

The "continue" option will continue sending GCodes from the file. If you were
previously sending GCodes from another source, it will resume sending GCodes
from the file where it left off.

The "stop" option will stop sending GCodes from the file and start sending
GCodes in the hard-coded stop sequence. This starts the sequence over every
time the option is chosen.

The "abort" option will stop sending any GCodes and exit the program.

The "hacker mode" option will allow you stop sending GCodes from the file and
instead type in GCodes manually.

The "list" option will list all known COM ports in an obscure fashion.

At the end of execution, the elapsed time it took to send GCode over the
serial port is shown.
*/
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"io"
	"log"
	"os"
	"os/signal"
	"time"
)

var (
	serial_mode = &serial.Mode{
		BaudRate: 115200,
	}
	stop_gcode = []byte(`M107
M104 S0
M140 S0
G1 Z50
M84 Z E
`)
)

type ctrlChoice int

const (
	ctrlContinue ctrlChoice = iota
	ctrlStop
	ctrlAbort
	ctrlHackerMode
)

func usage() {
	fmt.Printf("usage: %s [COM port] [Gcode path]\n", os.Args[0])
	os.Exit(2)
}

func main() {
	if len(os.Args) != 3 {
		usage()
	}

	port, err := serial.Open(os.Args[1], serial_mode)
	if err != nil {
		log.Fatal(err)
	}
	defer port.Close()

	f, err := os.Open(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}

	d := newDripper(port, f)
	d.loop()
}

func userInput(f *os.File) <-chan string {
	out := make(chan string)
	go func() {
		// On Windows/cmd.exe, Stdin will give io.EOF when CTRL-C is used
		// So we need to make a new Scanner.
		for {
			s := bufio.NewScanner(f)
			for s.Scan() {
				out <- s.Text()
			}
			if err := s.Err(); err != nil {
				log.Fatal(err)
			}
		}
	}()
	return out
}

func flushUserInput(in <-chan string) {
Loop:
	for {
		select {
		case <-in:
		default:
			break Loop
		}
	}
}

func gcodeLines(f *os.File) <-chan []byte {
	r := bufio.NewReader(f)
	out := make(chan []byte)
	go func() {
		var s []byte
		var err error
		defer f.Close()
		defer close(out)
		for err == nil {
			s, err = r.ReadBytes('\n')
			if len(s) == 0 {
				break
			}
			if i := bytes.IndexByte(s, ';'); i >= 0 {
				s = s[:i]
			}
			s = bytes.TrimSpace(s)
			if len(s) == 0 {
				continue
			}
			out <- s
		}
		if err != io.EOF {
			log.Fatal(err)
		}
	}()
	return out
}

func serialRecv(scan *bufio.Scanner) (lines []string, err error) {
	for scan.Scan() {
		ln := scan.Text()
		switch ln {
		case "ok":
			return lines, nil
		case "":
		default:
			lines = append(lines, ln)
		}
	}
	err = scan.Err()
	if err == nil {
		err = io.EOF
	}
	return lines, err
}

func serialRecvChan(r io.Reader) <-chan error {
	out := make(chan error)
	go func() {
		scan := bufio.NewScanner(r)
		defer close(out)
		// prime the pump
		out <- nil
		var err error
		for err == nil {
			var res []string
			res, err = serialRecv(scan)
			if len(res) > 0 {
				for _, ln := range res {
					fmt.Printf("<< %s\n", ln)
				}
			}
			out <- err
		}
	}()
	return out
}

func serialSendChan(port io.Writer) chan<- []byte {
	// Port reads are buffered but writes do not use bufio.
	// Give chan a buffer of 1 to avoid blocking in drip loop.
	in := make(chan []byte, 1)
	go func() {
		for {
			line, ok := <-in
			if !ok {
				return
			}
			fmt.Printf(">> %s\n", line)
			port.Write(line)
			port.Write([]byte{'\n'})
		}
	}()
	return in
}

type dripper struct {
	gcode_file   <-chan []byte
	serial_send  chan<- []byte
	serial_ready <-chan error
	user_input   <-chan string
	sig_chan     chan os.Signal
	hack_queue   []string
	ready        bool
}

func newDripper(port serial.Port, gcode *os.File) *dripper {
	return &dripper{
		serial_ready: serialRecvChan(port),
		serial_send:  serialSendChan(port),
		gcode_file:   gcodeLines(gcode),
		user_input:   userInput(os.Stdin),
		sig_chan:     make(chan os.Signal),
		ready:        false,
	}
}

func (d *dripper) send(line []byte) {
	d.ready = false
	d.serial_send <- line
}

func (d *dripper) catchSig() {
	signal.Notify(d.sig_chan, os.Interrupt)
}

func (d *dripper) dropSig() {
	signal.Reset(os.Interrupt)
}

func (d *dripper) loop() {
	d.catchSig()
	defer d.dropSig()

	var hack_queue []string
	var hack_mode bool

	gcode := d.gcode_file
	start := time.Now()
	log.Print("Start drip.")
Loop:
	for {
		select {
		case line := <-d.user_input:
			if hack_mode {
				if d.ready {
					d.send([]byte(line))
				} else {
					d.hack_queue = append(d.hack_queue, line)
				}
			}
			// O/W discard user input but keep reading it to flush stdin.
		case <-d.sig_chan:
			// Drop SIGINT handler so ^C twice will exit.
			d.dropSig()
			// Reset hacker mode in case we are in it.
			hack_mode = false
			switch controlMenu(d.user_input) {
			case ctrlContinue:
				fmt.Println("-- DRIP FILE")
				gcode = d.gcode_file
			case ctrlStop:
				fmt.Println("-- DRIP JOB STOP CODES")
				// XXX: this restarts the stop sequence each time
				gcode = stopGCode()
			case ctrlAbort:
				fmt.Println("-- ABORT")
				break Loop
			case ctrlHackerMode:
				fmt.Println("-- HACKER MODE: Type Gcodes now.")
				hack_mode = true
			}
			d.catchSig()
		case err, ok := <-d.serial_ready:
			d.ready = true
			switch {
			case err != nil:
				log.Println(err)
				break Loop
			case !ok:
				break Loop
			case hack_mode:
				if len(d.hack_queue) > 0 {
					d.send([]byte(hack_queue[0]))
					d.hack_queue = d.hack_queue[1:]
				}
			default:
				line, ok := <-gcode
				if !ok {
					break Loop
				}
				d.send(line)
			}
		}
	}

	close(d.serial_send)
	log.Println("Stop drip. Elapsed:", time.Since(start).Round(time.Second))
}

func controlMenu(userin <-chan string) ctrlChoice {
	// discard buffered input
	flushUserInput(userin)

	for {
		fmt.Print(`-- CTRL MENU
c) continue    (drip GCode file)
s) stop job    (drip stop GCode)
a) hard abort  (exits program)
h) hacker mode (enter GCodes on keyboard)
l) list ports  (list COM ports)
`)
		ans, ok := <-userin
		if !ok {
			log.Fatal("cannot read from stdin")
		}
		switch ans {
		case "c":
			return ctrlContinue
		case "s":
			return ctrlStop
		case "a":
			return ctrlAbort
		case "h":
			return ctrlHackerMode
		case "l":
			listPorts()
		default:
			fmt.Printf("invalid entry: %#v\n", ans)
		}
	}
}

func stopGCode() <-chan []byte {
	out := make(chan []byte)
	go func() {
		buf := bytes.NewBuffer(stop_gcode)
		var err error
		for err == nil {
			var ln []byte
			ln, err = buf.ReadBytes('\n')
			if len(ln) > 1 {
				out <- ln[:len(ln)-1]
			}
			if err != nil {
				break
			}
		}
		if err != io.EOF {
			log.Print(err)
		}
		close(out)
	}()
	return out
}

func listPorts() {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		log.Print(err)
	}
	if len(ports) == 0 {
		fmt.Println("(none)")
	}
	fmt.Printf("%#v\n", ports)
}
