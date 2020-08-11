package vmtest

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"
)

const qemuDefaultTimeout = 30 * time.Second

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

type QemuOptions struct {
	Architecture    QemuArchitecture // specifies which architecture to emulate, runs qemu-system-$ARCHITECTURE
	OperatingSystem OperatingSystem
	Params          []string // additional QEMU command line parameters
	Verbose         bool
	Timeout         time.Duration
	Kernel          string
	InitRamFs       string
	Disks           []string
	Append          []string // -append arguments
	CdRom           string
}

type Qemu struct {
	cmd             *exec.Cmd
	socketsDir      string
	consoleListener net.Listener
	console         net.Conn
	monitorListener net.Listener
	monitor         net.Conn
	ctxCancel       context.CancelFunc
	verbose         bool
	consoleData     []byte
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
	if len(kernelArgs) > 0 {
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
		cmdline = append(cmdline, "-drive", fmt.Sprintf("if=none,id=hd%v,file=%v", i, d),
			"-device", fmt.Sprintf("scsi-hd,drive=hd%v", i))
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
		return nil, fmt.Errorf("starting QEMU: %v", err)
	}

	monitor, err := monitorListener.Accept()
	if err != nil {
		return nil, err
	}
	console, err := consoleListener.Accept()
	if err != nil {
		return nil, err
	}

	qemu := &Qemu{
		cmd:             cmd,
		socketsDir:      tempDir,
		monitorListener: monitorListener,
		monitor:         monitor,
		consoleListener: consoleListener,
		console:         console,
		ctxCancel:       ctxCancel,
		verbose:         opts.Verbose,
	}
	return qemu, nil
}

func (q *Qemu) Stop() {
	if _, err := q.monitor.Write([]byte("quit\n")); err != nil {
		log.Printf("monitor: %v", err)
	}
	if err := q.cmd.Wait(); err != nil {
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

var ansiRe = regexp.MustCompile("[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))")
var emptyLineRe = regexp.MustCompile("[\r\n]\\W*[\r\n]")

// sanitizeText cleans the text before printing it to terminal.
// This function removes dangerous ANSI escape symbols, empty lines, ...
func sanitizeText(data []byte) []byte {
	data = ansiRe.ReplaceAll(data, []byte{})
	data = emptyLineRe.ReplaceAll(data, []byte{'\n'})
	return data
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
	return q.ConsoleProcess(p)
}

// ConsoleExpect waits until qemu console matches regexp provided by re
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
	err := q.ConsoleProcess(p)
	if err != nil {
		return nil, err
	} else {
		return matches, nil
	}
}

func (q *Qemu) ConsoleProcess(p LineProcessor) error {
	var buf [4096]byte
	for {
		num, err := q.console.Read(buf[:])
		if err != nil {
			return err
		}

		if num > 0 {
			q.consoleData = append(q.consoleData, buf[:num]...)
			idx := bytes.LastIndexByte(q.consoleData, '\n')
			containsNewLine := idx != -1

			var toPrint []byte
			if containsNewLine {
				toPrint = q.consoleData[:idx+1]
				q.consoleData = q.consoleData[idx+1:]
			} else {
				// In some cases we want to check str on lines without '\n'.
				// For example when the process prints "Please enter the password: '
				toPrint = q.consoleData[:]
			}

			if q.verbose {
				_, _ = os.Stdout.Write(sanitizeText(toPrint))
			}

			matched := p(toPrint)

			if matched {
				if containsNewLine {
					q.consoleData = []byte{}
				}
				return nil
			}
		}

		if num < cap(buf) {
			// QEMU did not fill the buffer completely. In this case let's sleep a bit and give QEMU
			// a chance to do some work.
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (q *Qemu) ConsoleWrite(str string) error {
	_, err := q.console.Write([]byte(str))
	return err
}
