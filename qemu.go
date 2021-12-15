package vmtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

const qemuDefaultTimeout = 30 * time.Second

// QemuArchitecture defines an architecture we launch QEMU for
type QemuArchitecture string

const (
	QEMU_AARCH64      = QemuArchitecture("aarch64")
	QEMU_ALPHA        = QemuArchitecture("alpha")
	QEMU_ARM          = QemuArchitecture("arm")
	QEMU_CRIS         = QemuArchitecture("cris")
	QEMU_HPPA         = QemuArchitecture("hppa")
	QEMU_I386         = QemuArchitecture("i386")
	QEMU_LM32         = QemuArchitecture("lm32")
	QEMU_M68K         = QemuArchitecture("m68k")
	QEMU_MICROBLAZE   = QemuArchitecture("microblaze")
	QEMU_MICROBLAZEEL = QemuArchitecture("microblazeel")
	QEMU_MIPS         = QemuArchitecture("mips")
	QEMU_MIPS64       = QemuArchitecture("mips64")
	QEMU_MIPS64EL     = QemuArchitecture("mips64el")
	QEMU_MIPSEL       = QemuArchitecture("mipsel")
	QEMU_MOXIE        = QemuArchitecture("moxie")
	QEMU_NIOS2        = QemuArchitecture("nios2")
	QEMU_OR1K         = QemuArchitecture("or1k")
	QEMU_PPC          = QemuArchitecture("ppc")
	QEMU_PPC64        = QemuArchitecture("ppc64")
	QEMU_RISCV32      = QemuArchitecture("riscv32")
	QEMU_RISCV64      = QemuArchitecture("riscv64")
	QEMU_S390X        = QemuArchitecture("s390x")
	QEMU_SH4          = QemuArchitecture("sh4")
	QEMU_SH4EB        = QemuArchitecture("sh4eb")
	QEMU_SPARC        = QemuArchitecture("sparc")
	QEMU_SPARC64      = QemuArchitecture("sparc64")
	QEMU_TRICORE      = QemuArchitecture("tricore")
	QEMU_UNICORE32    = QemuArchitecture("unicore32")
	QEMU_X86_64       = QemuArchitecture("x86_64")
	QEMU_XTENSA       = QemuArchitecture("xtensa")
	QEMU_XTENSAEB     = QemuArchitecture("xtensaeb")
)

type OperatingSystem int

const (
	OS_OTHER OperatingSystem = iota
	OS_LINUX
)

// QemuDisk represents a disk image supplied to qemu
type QemuDisk struct {
	// Path is a filesystem path to the image
	Path string
	// Format is a disk format of the image e.g. 'raw' or 'qcow2'
	Format string
	// Controller specified what drive controller is used for this disk, if empty then default "scsi-hd" is used
	Controller string
	// List of arguments appended to the disk's "-device controller,$arg1,$arg2" parameter
	DeviceParams []string
}

// QemuOptions options for qemu vm initialization
type QemuOptions struct {
	// Architecture specifies which architecture to emulate. It configures to run qemu-system-$ARCHITECTURE binary.
	Architecture QemuArchitecture
	// Operation system
	OperatingSystem OperatingSystem
	// additional QEMU command line parameters
	Params []string
	// Enable debug output
	Verbose bool
	// The qemu vm is killed after this timeout
	Timeout time.Duration
	// Kernel path to the kernel binary
	Kernel string
	// Path to ramfs image file
	InitRamFs string
	// Array of '-disk' parameters
	Disks []QemuDisk
	// Append specifies kernel parameters ('-append' qemu param)
	Append []string
	// Value of '-cdrom' parameter
	CdRom string
}

// Qemu represents a VM that is started by vmtest library
type Qemu struct {
	cmd                *exec.Cmd
	waitCh             chan error
	socketsDir         string
	consoleListener    net.Listener
	console            net.Conn
	consolePumpData    []byte
	consolePumpMutex   sync.Mutex
	consoleDataEOF     bool
	consoleData        []byte
	consoleDataArrived bool
	monitorListener    net.Listener
	monitor            net.Conn
	ctxCancel          context.CancelFunc
	verbose            bool
}

var _ VM = (*Qemu)(nil) // ensure Qemu implements VM interface

func quoteCmdline(cmdline []string) string {
	args := make([]string, len(cmdline))
	for i, s := range cmdline {
		if strings.ContainsAny(s, " \t\n") {
			args[i] = fmt.Sprintf("'%s'", s)
		} else {
			args[i] = s
		}
	}

	return strings.Join(args, " ")
}

// NewQemu creates a new qemu instance and starts it
func NewQemu(opts *QemuOptions) (*Qemu, error) {
	if opts.Timeout == 0 {
		opts.Timeout = qemuDefaultTimeout
	}
	if opts.Architecture == "" {
		opts.Architecture = QEMU_X86_64
	}

	tempDir, err := ioutil.TempDir("", "vmtest")
	if err != nil {
		return nil, err
	}

	monitorFile := path.Join(tempDir, "monitor.socket")
	monitorListener, err := net.Listen("unix", monitorFile)
	if err != nil {
		return nil, err
	}
	consoleFile := path.Join(tempDir, "console.socket")
	consoleListener, err := net.Listen("unix", consoleFile)
	if err != nil {
		return nil, err
	}

	qemuBinary := fmt.Sprintf("qemu-system-%v", opts.Architecture)
	cmdline := []string{
		"-monitor", fmt.Sprintf("unix:%v", monitorFile),
		"-serial", fmt.Sprintf("unix:%v", consoleFile),
		"-no-reboot",
		"-nographic", "-display", "none",
	}

	if opts.Kernel != "" {
		cmdline = append(cmdline, "-kernel", opts.Kernel)
	}
	if opts.InitRamFs != "" {
		cmdline = append(cmdline, "-initrd", opts.InitRamFs)
	}

	if opts.Kernel == "" && len(opts.Append) > 0 {
		// it comes from QEMU "qemu-system-x86_64: -append only allowed with -kernel option"
		return nil, fmt.Errorf("opts.Append only allowed with opts.Kernel option")
	}
	kernelArgs := opts.Append
	if opts.OperatingSystem == OS_LINUX {
		kernelArgs = append(kernelArgs, "console=ttyS0,115200", "ignore_loglevel")
	}
	if len(kernelArgs) > 0 && opts.Kernel != "" {
		cmdline = append(cmdline, "-append", strings.Join(kernelArgs, " "))
	}

	if opts.Architecture == "x86_64" {
		// cmdline = append(cmdline, "-device", "e1000,netdev=net0", "-netdev", "user,id=net0,hostfwd=tcp::5555-:22")
	}
	if len(opts.Params) > 0 {
		cmdline = append(cmdline, opts.Params...)
	}

	if opts.CdRom != "" {
		cmdline = append(cmdline, "-boot", "d", "-cdrom", opts.CdRom)
	}

	if len(opts.Disks) > 0 {
		cmdline = append(cmdline, "-device", "virtio-scsi-pci,id=scsi")
	}
	for i, d := range opts.Disks {
		format := ""
		if d.Format != "" {
			format = fmt.Sprintf("format=%s,", d.Format)
		}
		controller := d.Controller
		if controller == "" {
			controller = "scsi-hd"
		}
		drive := fmt.Sprintf("drive=hd%v", i)
		deviceParams := append([]string{controller, drive}, d.DeviceParams...)
		cmdline = append(cmdline, "-drive", format+fmt.Sprintf("if=none,id=hd%d,file=%s", i, d.Path),
			"-device", strings.Join(deviceParams, ","))
	}

	if opts.Verbose {
		log.Printf("QEMU command line: %v %v", qemuBinary, quoteCmdline(cmdline))
	}

	ctx, ctxCancel := context.WithTimeout(context.Background(), opts.Timeout)

	cmd := exec.CommandContext(ctx, qemuBinary, cmdline...)
	if opts.Verbose {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	err = cmd.Start()
	if err != nil {
		ctxCancel()
		return nil, fmt.Errorf("starting QEMU: %v", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		waitCh <- err
		if err != nil {
			ctxCancel()
			// Interrupt the Accept() calls below, which would otherwise
			// deadlock if qemu exits immediately:
			monitorListener.Close()
			consoleListener.Close()
		}
	}()

	monitor, err := monitorListener.Accept()
	if err != nil {
		select {
		case waitErr := <-waitCh:
			return nil, waitErr
		default:
			return nil, err
		}
	}
	console, err := consoleListener.Accept()
	if err != nil {
		select {
		case waitErr := <-waitCh:
			return nil, waitErr
		default:
			return nil, err
		}
	}

	qemu := &Qemu{
		cmd:             cmd,
		waitCh:          waitCh,
		socketsDir:      tempDir,
		monitorListener: monitorListener,
		monitor:         monitor,
		consoleListener: consoleListener,
		console:         console,
		ctxCancel:       ctxCancel,
		verbose:         opts.Verbose,
	}

	go qemu.consolePump(opts.Verbose)

	return qemu, nil
}

// List of escape sequences produced by Seabios/Linux
var ansiRe = regexp.MustCompile(`\x1b(c|M|\[(\d+;\d+H|=3h|[\d;]+m|\?7l|2J|K))`)

func (q *Qemu) consolePump(verbose bool) {
	var buf [4096]byte
	dataLength := 0

	for {
		num, err := q.console.Read(buf[dataLength:])
		if num > 0 {
			dataLength += num
			toPrint := buf[:dataLength]
			dataLength = 0

			// remove ANSI escape sequences
			if bytes.Contains(toPrint, []byte{'\x1b'}) {
				toPrint = ansiRe.ReplaceAll(toPrint, []byte{})
				// Sometimes ASCII sequences are not fully pumped to the buffer yet.
				// Print out the beginning of the string but leave incomplete ASCII sequence in the buffer to process it later
				asciiStart := bytes.LastIndexByte(toPrint, '\x1b')

				const asciiSeqMaxLength = 30 // some sequences might be up to 20 symbols
				if asciiStart != -1 && len(toPrint)-asciiStart < asciiSeqMaxLength {
					// If incomplete ASCII sequence starts close to the end of the buffer
					// then copy the sequence back to the beginning of buf and the rest is
					// printed out.
					copy(buf[:], toPrint[asciiStart:])
					dataLength = len(toPrint) - asciiStart
					toPrint = toPrint[:asciiStart]
				}
			}

			if verbose {
				_, _ = os.Stdout.Write(toPrint)
			}

			q.consolePumpMutex.Lock()
			q.consoleData = append(q.consoleData, toPrint...)
			q.consoleDataArrived = true
			q.consolePumpMutex.Unlock()
		}

		if err != nil {
			if err == io.EOF {
				q.consoleDataEOF = true
			} else {
				log.Print(err)
			}
			return
		}

		if num == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}

}

func (q *Qemu) wait() {
	if err := <-q.waitCh; err != nil {
		log.Printf("Got error while waiting for Qemu process completion: %v", err)
	}
	q.ctxCancel()

	_ = q.console.Close()
	_ = q.consoleListener.Close()
	_ = q.monitor.Close()
	_ = q.monitorListener.Close()
	if err := os.RemoveAll(q.socketsDir); err != nil {
		log.Printf("Cannot remove temporary dir %v: %v", q.socketsDir, err)
	}
}

// Kill shuts down the vm using qemu's 'kill' command
func (q *Qemu) Kill() {
	if _, err := q.monitor.Write([]byte("quit\n")); err != nil {
		log.Printf("monitor: %v", err)
	}
	q.wait()
}

// Shutdown shuts down the vm using qemu's 'system_powerdown' command
func (q *Qemu) Shutdown() {
	if _, err := q.monitor.Write([]byte("system_powerdown\n")); err != nil {
		log.Printf("monitor: %v", err)
	}
	q.wait()
}

// LineProcessor accepts byte array as input data. It returns whether processing has matched the input line
// and thus processing need to be stopped.
type LineProcessor func(data []byte) bool

// ConsoleExpect waits until qemu console matches str
func (q *Qemu) ConsoleExpect(str string) error {
	match := []byte(str)
	p := func(data []byte) bool {
		return bytes.Contains(data, match)
	}
	return q.consoleProcess(p)
}

// ConsoleExpectRE waits until qemu console matches regexp provided by re
// returns array of matched strings
func (q *Qemu) ConsoleExpectRE(re *regexp.Regexp) ([]string, error) {
	var matches []string
	p := func(data []byte) bool {
		m := re.FindAllSubmatch(data, -1)
		if m == nil {
			return false
		}
		for _, s := range m {
			matches = append(matches, string(s[1]))
		}
		return true
	}
	err := q.consoleProcess(p)
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func (q *Qemu) consoleProcess(processor LineProcessor) error {
	var buf []byte
	for {
		q.consolePumpMutex.Lock()
		buf = append(buf, q.consoleData...)
		newDataArrived := q.consoleDataArrived
		consoleDataEOF := q.consoleDataEOF
		q.consoleData = nil
		q.consoleDataArrived = false
		q.consolePumpMutex.Unlock()

		if newDataArrived {
			for {
				var newLine bool

				idx := bytes.IndexByte(buf, '\n')
				if idx == -1 {
					// In some cases we want to check str on lines without '\n'.
					// For example when the process prints "Please enter the password: '
					idx = len(buf)
				} else {
					idx++ // remove trailing \n
					newLine = true
				}
				toProcess := buf[:idx]
				if newLine {
					buf = buf[idx:]
				}

				matched := processor(toProcess)

				if matched {
					// add non-processed data back to the pump
					q.consolePumpMutex.Lock()
					q.consoleData = append(buf, q.consoleData...)
					q.consoleDataArrived = true
					q.consolePumpMutex.Unlock()

					return nil
				}

				if !newLine {
					break
				}
			}
		} else if consoleDataEOF {
			return io.EOF
		} else {
			// QEMU did not fill the buffer completely. In this case let's sleep a bit and give QEMU
			// a chance to do some work.
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// ConsoleWrite writes given string to qemu console
func (q *Qemu) ConsoleWrite(str string) error {
	_, err := q.console.Write([]byte(str))
	return err
}
