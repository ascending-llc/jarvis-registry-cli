package logging

type (
	// Logger is the minimal logging interface required by this module's
	// packages, satisfied by *log.Logger.
	Logger interface {
		Print(v ...any)
		Printf(format string, v ...any)
		Println(v ...any)
	}
)
