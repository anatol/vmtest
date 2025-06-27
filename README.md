# VmTest [![Build Status](https://secure.travis-ci.org/anatol/vmtest.svg?branch=master)](http://travis-ci.org/anatol/vmtest)

VmTest is a [Go language](https://golang.org/) library that provides an easy way to setup a Virtual Machine
for your integration tests written in Go.

#### Running Linux kernel in QEMU

In the following example `vmtest` sets up and lunches an instance of QEMU emulator with the kernel and initramfs specified.
```go
package main

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/anatol/vmtest"
)

func TestBootCurrentLinuxKernelInQemu(t *testing.T) {
	// Configure QEMU emulator
	opts := vmtest.QemuOptions{
		OperatingSystem: vmtest.OS_LINUX,
		Kernel:          "/boot/vmlinuz-linux",
		InitRamFs:       "/boot/initramfs-linux.img",
		Params: []string{
			"-enable-kvm", "-cpu", "host",
			"-m", "8G",
		},
		Append: []string{
			"rd.luks.name=d4440324-32ed-44e6-a99f-5c18859b6bac=cryptroot",
			"root=/dev/mapper/cryptroot",
		},
		Disks: []vmtest.QemuDisk{
			{Path: "testdata/luksv2.disk", Format: "raw"},
		},
		Verbose: testing.Verbose(),
		Timeout: 20 * time.Second,
	}
	// Run QEMU instance
	qemu, err := vmtest.NewQemu(&opts)
	if err != nil {
		t.Fatal(err)
	}
	// Exit QEMU at the end of the test case
	defer qemu.Kill()

	// Wait until a specific string is found in the console output
	if err := qemu.ConsoleExpect("Run /init as init process"); err != nil {
		t.Fatal(err)
	}
	// Write some text to console
	if err := qemu.ConsoleWrite("12345"); err != nil {
		t.Fatal(err)
	}

	// Test the regexp matcher
	re, err := regexp.Compile("exit_code=(\\d+)")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := qemu.ConsoleExpectRE(re)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("Exit code is %v", matches[0])

	// Wait for some text again
	if err := qemu.ConsoleExpect("You are in emergency mode. After logging in, type \"journalctl -xb\" to view"); err != nil {
		t.Fatal(err)
	}
}
```

Then run the test using `go test` toolset:
```shell script
$ go test
PASS
ok  	github.com/anatol/vmtest	2.495s
```

With the verbose mode specified one gets a lot of additional information from QEMU console:
```shell script
$ go test -v
=== RUN   TestBootCurrentLinuxKernelInQemu
2020/04/04 11:15:25 QEMU command line: qemu-system-x86_64 -enable-kvm -cpu host -monitor unix:/tmp/vmtest012738181/monitor.socket -serial unix:/tmp/vmtest012738181/console.socket -no-reboot -m 8G -nographic -display none -device e1000,netdev=net0 -netdev user,id=net0,hostfwd=tcp::5555-:22 -device virtio-scsi-pci,id=scsi -append 'console=ttyS0,115200 ignore_loglevel' -kernel /boot/vmlinuz-linux -initrd /boot/initramfs-linux.img
SeaBIOS (version ?-20191223_100556-anatol)
iPXE (http://ipxe.org) 00:03.0 CA00 PCI2.10 PnP PMM+BFF927B0+BFEF27B0 CA00
Press Ctrl-B to configure iPXE (PCI 00:03.0)...
Booting from ROM...
Probing EDD (edd=off to disable)... o[    0.000000] Linux version 5.5.13-arch2-1 (linux@archlinux) (gcc version 9.3.0 (Arch Linux 9.3.0-1)) #1 SMP PREEMPT Mon, 30 Mar 2020 20:42:41 +0000
[    0.000000] Command line: console=ttyS0,115200 ignore_loglevel
[    0.000000] KERNEL supported cpus:
[    0.000000]   Intel GenuineIntel
[    0.000000]   AMD AuthenticAMD
[    0.000000]   Hygon HygonGenuine
[    0.000000]   Centaur CentaurHauls
[    0.000000]   zhaoxin   Shanghai
[    0.000000] x86/fpu: Supporting XSAVE feature 0x001: 'x87 floating point registers'
[    0.000000] x86/fpu: Supporting XSAVE feature 0x002: 'SSE registers'
[    0.000000] x86/fpu: Supporting XSAVE feature 0x004: 'AVX registers'
[    0.000000] x86/fpu: Supporting XSAVE feature 0x008: 'MPX bounds registers'
....
[  OK  ] Stopped Create list of sta… nodes for the current kernel.
[    2.269008] audit: type=1131 audit(1586105934.576:7): pid=1 uid=0 auid=4294967295 ses=4294967295 msg='unit=initrd-cleanup comm="systemd" exe="/init" hostname=? addr=? terminal=? res=success'
[    2.276930] audit: type=1131 audit(1586105934.589:8): pid=1 uid=0 auid=4294967295 ses=4294967295 msg='unit=systemd-udevd comm="systemd" exe="/init" hostname=? addr=? terminal=? res=success'
[  OK  ] Finished Cleanup udevd DB.
[  OK  ] Reached target Switch Root.
[    2.294760] audit: type=1131 audit(1586105934.599:9): pid=1 uid=0 auid=4294967295 ses=4294967295 msg='unit=systemd-tmpfiles-setup-dev comm="systemd" exe="/init" hostname=? addr=? terminal=? res=success'
         Starting Switch Root...
[FAILED] Failed to start Switch Root.
See 'systemctl status initrd-switch-root.service' for details.
[    2.314979] audit: type=1131 audit(1586105934.606:10): pid=1 uid=0 auid=4294967295 ses=4294967295 msg='unit=kmod-static-nodes comm="systemd" exe="/init" hostname=? addr=? terminal=? res=success'
You are in emergency mode. After logging in, type "journalctl -xb" to view
system logs, "systemctl reboot" to reboot, "systemctl default" or "exit"
to boot into default mode.
Press Enter for maintenance
--- PASS: TestBootCurrentLinuxKernelInQemu (2.92s)
PASS
ok  	github.com/anatol/vmtest	2.919s
```

#### Running ARM bare-metal application in QEMU

`VmTest` provides a way to test bare-metal application as well. In the following example we run ARM bare-metal app and verify that console contains expected output
```go
package main

import (
	"testing"
	"time"

	"github.com/anatol/vmtest"
)

func TestRunArmInQemu(t *testing.T) {
	opts := vmtest.QemuOptions{
		Architecture: vmtest.QEMU_ARM,
		Params: []string{
			"-M", "versatilepb", "-m", "128M",
		},
		Kernel:  "testdata/hello-arm.bin",
		Verbose: testing.Verbose(),
		Timeout: 5 * time.Second,
	}
	qemu, err := vmtest.NewQemu(&opts)
	if err != nil {
		t.Fatal(err)
	}
	defer qemu.Kill()

	if err := qemu.ConsoleExpect("Hello from ARM emulator!"); err != nil {
		t.Fatal(err)
	}
}
```

#### Running tests as root inside a QEMU virtual machine

Some applications require testing with root privileges. `VmTest` library provides a way to do it by running a QEMU
virtual machine, `scp` a test binary to it and then run the test there.

To achieve it please create a Linux kernel binary and rootfs image using [following instructions](docs/prepare_image.md).

Then create a test case that is going to run as a `root` *inside* QEMU vm:

```go
package tests

import (
	"os/user"
	"testing"
)

func TestFoo(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	if u.Username != "root" {
		t.SkipNow()
	}

	// add your tests here
}
```

Then add a test to setup a QEMU vm instance:
```go
package tests

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/anatol/vmtest"
	"github.com/tmc/scp"
	"golang.org/x/crypto/ssh"
)


func TestBootInQemu(t *testing.T) {
	// qemu_run_test.go is your test file from above. We have to compile it as a binary and copy to a QEMU vm
	cmd := exec.Command("go", "test", "-c", "qemu_run_test.go", "-o", "qemu_run_test")
	if testing.Verbose() {
		log.Print("compile in-qemu test binary")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	err := cmd.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove("qemu_run_test")

	// These integration tests use QEMU with a statically-compiled kernel (to avoid inintramfs) and specially
	// prepared rootfs. See TESTING.md.md file for instructions how to prepare
	opts := vmtest.QemuOptions{
		OperatingSystem: vmtest.OS_LINUX,
		Kernel:          "bzImage",
		Params:          []string{"-net", "user,hostfwd=tcp::10022-:22", "-net", "nic", "-enable-kvm", "-cpu", "host"},
		Disks:           []vmtest.QemuDisk{{"rootfs.qcow2", "qcow2"}},
		Append:          []string{"root=/dev/sda", "rw"},
		Verbose:         testing.Verbose(),
		Timeout:         50 * time.Second,
	}
	// Run QEMU instance
	qemu, err := vmtest.NewQemu(&opts)
	if err != nil {
		t.Fatal(err)
	}
	// Shutdown QEMU at the end of the test case
	defer qemu.Shutdown()

	config := &ssh.ClientConfig{
		User:            "root",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	conn, err := ssh.Dial("tcp", "localhost:10022", config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	scpSess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	err = scp.CopyPath("qemu_run_test", "qemu_run_test", scpSess)
	if err != nil {
		t.Error(err)
	}

	testCmd := "./qemu_run_test"
	if testing.Verbose() {
		testCmd += " -test.v"
	}

	output, err := sess.CombinedOutput(testCmd)
	if testing.Verbose() {
		fmt.Print(string(output))
	}
	if err != nil {
		t.Fatal(err)
	}
}
```

## License

See [LICENSE](LICENSE).
