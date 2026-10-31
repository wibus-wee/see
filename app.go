// See is a desktop source code reader: a file tree, tabs, a code editor,
// and Go to Definition through language servers, drawn natively by MyGo,
// with no web view.
//
// Everything else lives under internal/; this file only hands the process
// to the app package, which wires the modules together.
package main

import "see/internal/app"

func main() {
	app.Main()
}
