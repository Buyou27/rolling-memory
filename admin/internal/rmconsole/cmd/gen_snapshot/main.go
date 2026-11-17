package main

import (
	"fmt"
	"os"

	rollingmemory "github.com/ai-go-hub/ai-go-admin/internal/rmconsole"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] == "" {
		fmt.Fprintln(os.Stderr, "Usage: gen_snapshot <new-output.json[.gz]>")
		os.Exit(1)
	}
	if err := rollingmemory.GenerateLocomoStaticSnapshotTo(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating snapshot: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Snapshot generated successfully.")
}
