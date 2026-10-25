// Peek is a native git diff viewer for macOS.
package main

import (
	"os"

	"peek/internal/app"
)

func main() {
	app.Run(os.Args[1:])
}
