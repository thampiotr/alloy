package util

import "fmt"

// LogPrefix is prepended to every framework log line. Keep it stable: CI
// log scrapers and humans alike use it to spot runner/harness/deps output
// among the test logs.
const LogPrefix = "[k8s-itest] "

// Logf prints a framework log line: LogPrefix + the formatted message + a
// trailing newline. Use this from runner / harness / deps code instead of
// `fmt.Printf("[k8s-itest] ...")` directly so the prefix isn't duplicated.
//
// For the rare case where the line must not end in a newline (e.g. an
// inline y/N prompt), use `fmt.Print(util.LogPrefix + "...")` directly.
func Logf(format string, args ...any) {
	fmt.Printf(LogPrefix+format+"\n", args...)
}
