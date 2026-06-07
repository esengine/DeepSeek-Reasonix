package testpkg

import "fmt"

// Greet prints a greeting message.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

// Add adds two integers.
func Add(a, b int) int {
	return a + b
}

