package main

import "github.com/KyleYu2024/mosctl/internal/diagnostics"

func main() {
	if err := diagnostics.Capture(); err != nil {
		panic(err)
	}
	Execute()
}
