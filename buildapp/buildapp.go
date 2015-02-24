// The buildapp command takes one or more file names and
// calls apptemplate.Process on each one.
package main

import (
  "bufio"
  "os"
  "strings"
  "github.com/michaellaszlo/boomerang/apptemplate"
)

func main() {
  writer := bufio.NewWriter(os.Stdout)
  defer writer.Flush()

  // Absolute template paths are resolved relative to the site root.
  // A running app can ask Apache for this value. The app builder cannot.
  siteRoot, error := os.Getwd()
  if error != nil {
    writer.WriteString(error.Error())
    return
  }

  if len(os.Args) == 1  {
    writer.WriteString("No files specified.\n")
    return
  }
  for i := 1; i < len(os.Args); i++ {
    arg := os.Args[i]
    // Minimal switch parsing.
    if strings.Index(arg, "-root=") == 0 {
      siteRoot = arg[6:]  // The root switch affects arguments that follow it.
      continue
    }
    // Parse a top-level template.
    apptemplate.Process(siteRoot, os.Args[i], writer)
  }
} 
