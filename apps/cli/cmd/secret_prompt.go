package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// readSecret asks for a value that must not be echoed. On a terminal it reads
// with echo off; otherwise it takes the next line of standard input through
// sc, the scanner the command reads its other answers with, so piped answers
// arrive in order. With neither, it says how to give the value.
func readSecret(sc *bufio.Scanner, label string) (string, error) {
	fmt.Printf("%s: ", label)
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
		}
		return string(b), nil
	}
	fmt.Println()
	if !sc.Scan() {
		return "", fmt.Errorf("no terminal to ask for the %s: run this in a terminal, or give it on standard input", strings.ToLower(label))
	}
	return strings.TrimRight(sc.Text(), "\r"), nil
}
