// The buildapp command takes one or more file names and
// calls apptemplate.Process on each one.
package main

import (
  "github.com/michaellaszlo/boomerang/apptemplate"
  "bufio"
  "os"
  "flag"
  "fmt"
)

func main() {
  writer := bufio.NewWriter(os.Stdout)
  defer writer.Flush()

  // The current working directory is a default value for the site root
  //  and for the starting point of a directory walk.
  workingDirectory, error := os.Getwd()
  if error != nil {
    writer.WriteString(error.Error())
    return
  }

  // Command-line flags:
  var siteRoot, walkDirectory, listPath string

  // Absolute template paths are resolved relative to the site root.
  // A running app can ask Apache for this value. The app builder cannot.
  flag.StringVar(&siteRoot, "root", workingDirectory,
      "the physical location of the website's root directory")

  flag.StringVar(&walkDirectory, "w", "",
      "the starting directory for a recursive walk of .boo files")

  flag.StringVar(&listPath, "l", "",
      "the path of a file that lists files to be processed")

  flag.Parse()
  args := flag.Args()  // These arguments remain after flags are extracted.

  if len(args) == 0  {
    // If no arguments are left over, we're doing one of the following:
    // buildapp -w <directory>  # recursively walk a directory for .boo files
    // buildapp                 # walk from cwd; equivalent to "buildapp -w ."
    // buildapp -l <file>       # process the files listed in the named file
    if listPath != "" {
      fmt.Fprintf(os.Stderr, "reading file names from %s\n", listPath)
      return
    }
    if walkDirectory == "" {
      walkDirectory = workingDirectory
    }
    fmt.Fprintf(os.Stderr, "recursive walk from %s\n", walkDirectory)
  } else {
    // Otherwise, each argument is the path of a template:
    // buildapp <file 1> ...    # process the named files
    for _, arg := range args {
      apptemplate.Process(siteRoot, arg, writer)
    }
  }
} 
