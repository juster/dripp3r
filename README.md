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
