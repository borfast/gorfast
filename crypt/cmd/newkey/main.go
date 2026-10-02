package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/borfast/gorfast/crypt"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	algorithms := map[string]crypt.Algorithm{
		"xchacha20poly1305": crypt.XChaCha20Poly1305,
		"aes256gcm":         crypt.AES256GCM,
	}

	flags := flag.NewFlagSet("newkey", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: newkey [-alg xchacha20poly1305|aes256gcm]")
	}
	alg := flags.String("alg", "xchacha20poly1305", "key algorithm")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "newkey does not accept positional arguments")
		flags.Usage()
		return 2
	}

	algorithm, ok := algorithms[*alg]
	if !ok {
		fmt.Fprintln(stderr, "unsupported -alg; choose xchacha20poly1305 or aes256gcm")
		return 2
	}
	key, err := crypt.GenerateKey(algorithm)
	if err != nil {
		fmt.Fprintf(stderr, "generate key: %v\n", err)
		return 2
	}
	if _, err := fmt.Fprintln(stdout, key.Spec()); err != nil {
		fmt.Fprintln(stderr, "write key failed")
		return 2
	}
	return 0
}
