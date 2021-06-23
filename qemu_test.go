package vmtest

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var isTravis bool

func init() {
	isTravis = os.Getenv("TRAVIS") != ""
}

// detectLinuxKernel returns path to kernel/initramfs at the current system
func detectLinuxKernel() (string, string, error) {
	if _, err := os.Stat("/boot/vmlinuz-linux"); err == nil {
		// it looks like Arch Linux
		return "/boot/vmlinuz-linux", "/boot/initramfs-linux.img", nil
	}

	// Check if it is Debian?
	var uts unix.Utsname

	if err := unix.Uname(&uts); err != nil {
		return "", "", fmt.Errorf("uname: %v", err)
	}
	length := bytes.IndexByte(uts.Release[:], 0)
	version := string(uts.Release[:length])

	kernel := fmt.Sprintf("/boot/vmlinuz-%v", version)
	initram := fmt.Sprintf("/boot/initrd.img-%v", version)

	if _, err := os.Stat(kernel); err != nil {
		return "", "", fmt.Errorf("Cannot find Linux kernel file at this system")
	}

	return kernel, initram, nil
}

func TestBootCurrentLinuxKernelInQemu(t *testing.T) {
	// Let's boot current system kernel, but we need to find out what is its path
	kernel, initram, err := detectLinuxKernel()
	if err != nil {
		t.Fatal(err)
	}
	// Check that the file is readable
	var fd *os.File
	if fd, err = os.Open(kernel); err != nil {
		msg := fmt.Sprintf("Cannot open kernel file %v", kernel)
		if isTravis {
			t.Skip(msg)
		} else {
			t.Fatal(msg)
		}
	}
	_ = fd.Close()

	// Configure QEMU emulator
	params := []string{"-m", "512"}
	if !isTravis {
		// Travis CI does not support KVM
		params = append(params, "-enable-kvm", "-cpu", "host")
	}
	opts := QemuOptions{
		OperatingSystem: OS_LINUX,
		Kernel:          kernel,
		InitRamFs:       initram,
		Params:          params,
		Verbose:         testing.Verbose(),
		Timeout:         20 * time.Second,
	}
	// Run QEMU instance
	qemu, err := NewQemu(&opts)
	if err != nil {
		t.Fatal(err)
	}
	// Stop QEMU at the end of the test case
	defer qemu.Kill()

	// Wait until a specific string is found in the console output
	if err := qemu.ConsoleExpect("Run /init as init process"); err != nil {
		t.Fatal(err)
	}

	// Test the regexp matcher
	re, err := regexp.Compile(`Starting version (.*)`)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := qemu.ConsoleExpectRE(re)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected to match systemd version")
	}

	// Write some text to console
	if err := qemu.ConsoleWrite("12345"); err != nil {
		t.Fatal(err)
	}
	// Wait for some text again
	if err := qemu.ConsoleExpect("You are now being dropped into an emergency shell"); err != nil {
		t.Fatal(err)
	}
}

func TestRunArmInQemu(t *testing.T) {
	opts := QemuOptions{
		Architecture: QEMU_ARM,
		Params: []string{
			"-M", "versatilepb", "-m", "128M",
		},
		// This binary sources can be found at https://balau82.wordpress.com/2010/02/28/hello-world-for-bare-metal-arm-using-qemu/
		Kernel:  "testdata/hello-arm.bin",
		Verbose: testing.Verbose(),
		Timeout: 5 * time.Second,
	}
	qemu, err := NewQemu(&opts)
	if err != nil {
		t.Fatal(err)
	}
	defer qemu.Kill()

	if err := qemu.ConsoleExpect("Hello from ARM emulator!"); err != nil {
		t.Fatal(err)
	}
}
