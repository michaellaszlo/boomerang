// The buildapp command takes one or more file names and
// calls apptemplate.Process on each one.
package main

import (
  "bufio"
  "os"
  "boomerang/apptemplate"
)

func main() {
  writer := bufio.NewWriter(os.Stdout)
  defer writer.Flush()

  // We resolve absolute paths by consulting the website root.
  siteRoot := "/var/www/dd1"  // Stub. We'll get the real value from Apache.

  numFiles := len(os.Args)-1
  if numFiles == 0 {
    writer.WriteString("No files specified.\n")
    return
  }
  for argIx := 1; argIx <= numFiles; argIx++ {
    // Parse a top-level template.
    apptemplate.Process(siteRoot, os.Args[argIx], writer)
  }
}
