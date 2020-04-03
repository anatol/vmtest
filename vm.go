package vmtest

type VM interface {
	ConsoleExpect(str string) error
	ConsoleWrite(str string) error
	// Stop stops the current virtual machine
	Stop()
}
