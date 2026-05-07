package util

import (
	"fmt"
	"time"
)

// Step runs fn while logging start, finish (with duration), and any error
// using the same `[k8s-itest] ...` prefix as the rest of the framework. Use
// it to wrap discrete install/cleanup operations whose timing is useful in
// test logs.
func Step(name string, fn func() error) error {
	start := time.Now()
	fmt.Printf("[k8s-itest] %s...\n", name)
	if err := fn(); err != nil {
		fmt.Printf("[k8s-itest] failed %s time=%s err=%v\n", name, time.Since(start).Round(time.Millisecond), err)
		return err
	}
	fmt.Printf("[k8s-itest] done %s time=%s\n", name, time.Since(start).Round(time.Millisecond))
	return nil
}
