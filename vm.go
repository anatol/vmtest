package vmtest

import "regexp"

type VM interface {
	ConsoleExpect(str string) error
	ConsoleExpectRE(re *regexp.Regexp) ([]string, error)
	ConsoleWrite(str string) error
	// Shutdown sends shutdown event, similar to what a PowerDown button would do
	Shutdown()
	// Kill kills the current VM instance
	Kill()
}
