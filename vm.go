package vmtest

import "regexp"

type VM interface {
	ConsoleExpect(str string) error
	ConsoleExpectRE(re *regexp.Regexp) ([]string, error)
	ConsoleWrite(str string) error
	// Stop stops the current virtual machine
	Stop()
}
