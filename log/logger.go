package log

import "log"

// EnableDebugLog is the flag to enable the debug log
var EnableDebugLog = false

// Print is the wrapper function of the standard log.Print
func Print(v ...interface{}) {
	log.Print(v...)
}

// Printf is the wrapper function of the standard log.Printf
func Printf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// Println is the wrapper function of the standard log.Println
func Println(v ...interface{}) {
	log.Println(v...)
}

// Debug calls standard log.Print function if the `EnableDebugLog` is true
func Debug(v ...interface{}) {
	if EnableDebugLog {
		log.Print(v...)
	}
}

// Debugf calls standard log.Printf function if the `EnableDebugLog` is true
func Debugf(format string, v ...interface{}) {
	if EnableDebugLog {
		log.Printf(format, v...)
	}
}

// Debugln calls standard log.Println function if the `EnableDebugLog` is true
func Debugln(v ...interface{}) {
	if EnableDebugLog {
		log.Println(v...)
	}
}

func init() {
	log.SetFlags(0)
}
