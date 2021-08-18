package vmtest

import "regexp"

// VM represents a virtual machine instance that can be managed using API
type VM interface {

	// ConsoleExpect waits till particular string appears in the VM console output
	ConsoleExpect(str string) error

	// ConsoleExpectRE waits until console output matches given regexp.
	// The function returns a list of submatches
	ConsoleExpectRE(re *regexp.Regexp) ([]string, error)

	// ConsoleWrite write the string to VM console
	ConsoleWrite(str string) error

	// Shutdown sends shutdown event, similar to what a PowerDown button would do
	Shutdown()

	// Kill kills the current VM instance
	Kill()
}
