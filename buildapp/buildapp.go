// The buildapp command takes one or more file names and
// calls apptemplate.Process on each one.
package main

import (
  "github.com/michaellaszlo/boomerang/apptemplate"
  "io"
  "bufio"
  "strings"
  "os"
  "os/exec"
  "flag"
  "fmt"
  "path/filepath"
)

// Command-line flags
var siteRoot, walkDirectory, listPath string

// Print updates and errors to this file, probably stdout or stderr.
var messageFile *os.File

var GoPath = "go"

func processTemplate(path string) {

  // Make a .go file corresponding to the template file.
  dir, file := filepath.Split(path)
  if len(file) >= 4 && file[len(file)-4:] == ".boo" {
    file = file[:len(file)-4]
  }
  goCodePath := filepath.Join(dir, file + ".go")
  outFile, err := os.Create(goCodePath)
  if err == nil {
    fmt.Fprintf(messageFile, "created %s\n", goCodePath)
  } else {
    fmt.Fprintf(messageFile, "error on creating %s\n", goCodePath)
    return
  }

  // Process the template, flush the output, close the file.
  templateWriter := bufio.NewWriter(outFile)
  fmt.Fprintf(messageFile, "parsing %s\n", path)
  err = apptemplate.Process(siteRoot, path, templateWriter)
  templateWriter.Flush()
  outFile.Close()
  if err != nil {
    fmt.Fprintf(messageFile, "skipping compilation due to parsing error\n")
    return
  }

  fmt.Fprintf(messageFile, "compiling %s\n", goCodePath)
  cmd := exec.Command(GoPath, "build", goCodePath)
  output, err := cmd.CombinedOutput()
  if err != nil {
    fmt.Fprintf(messageFile, "compilation error: %s\n", err)
    fmt.Fprintf(messageFile, "command output: %s", string(output))
  }
}

func directoryWalker(path string, info os.FileInfo, err error) error {
  if err != nil {
    return err
  }
  mode := info.Mode()
  if mode & os.ModeDir != 0 {
    return nil
  }
  if len(path) >= 4 && path[len(path)-4:] == ".boo" {
    processTemplate(path)
  }
  return nil
}

func main() {
  messageFile = os.Stderr

  // The current working directory is a default value for the site root
  //  and for the starting point of a directory walk.
  workingDirectory, err := os.Getwd()
  if err != nil {
    fmt.Fprintf(messageFile, "%s\n", err.Error())
    return
  }

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
    // If no arguments remain after flag parsing, we're doing one of these:
    // buildapp -l <file>       # process the files listed in the named file
    // buildapp                 # equivalent to buildapp -w .
    // buildapp -w <directory>  # recursively walk a directory for .boo files
    // buildapp                 # walk from cwd; equivalent to "buildapp -w ."

    // buildapp -l <file>       # process the files listed in the named file
    if listPath != "" {
      fmt.Fprintf(messageFile, "reading file names from %s\n", listPath)
      file, err := os.Open(listPath)
      if err != nil {
        fmt.Fprintf(messageFile, "%s\n", err.Error())
        return
      }
      reader := bufio.NewReader(file)
      for {
        line, err := reader.ReadString('\n')
        if err != nil {
          if err != io.EOF {
            fmt.Fprintf(messageFile, "%s\n", err.Error())
          }
          break
        }
        path := strings.TrimSpace(line)
        processTemplate(path)
      }
      return
    }

    // buildapp                 # equivalent to buildapp -w .
    if walkDirectory == "" {
      walkDirectory = workingDirectory
    }

    // buildapp -w <directory>  # recursively walk a directory for .boo files
    fmt.Fprintf(messageFile, "recursive walk from %s\n", walkDirectory)
    err := filepath.Walk(walkDirectory, directoryWalker)
    if err != nil {
      fmt.Fprintf(messageFile, "%s\n", err.Error())
    }

  } else {
    // If we have non-flag arguments, each must name a template file.
    // buildapp <file 1> ...    # process the named files
    for _, path := range args {
      processTemplate(path)
    }
  }
} 
