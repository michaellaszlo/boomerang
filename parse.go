package main

import (
  "os"
  foo "fmt"
  "io"
  "bufio"
  "strings"
  "path/filepath"
  "errors"
  "bytes"
  "go/token"
  "go/parser"
  "go/printer"
  "golang.org/x/tools/go/ast/astutil"
)

const verbose bool = false

var output bytes.Buffer


//--- Linear pattern matcher

type Pattern struct {
  Text []rune
  Length, Pos int
}

func newPattern(s string) Pattern {
  runes := []rune(s)
  return Pattern{ Text: runes, Length: len(runes) }
}

// Next returns true if Pos advances past the last character of Text.
func (pattern *Pattern) Next(ch rune) bool {
  // If Pos is past the end of Text, reset it to the beginning.
  if pattern.Pos == pattern.Length {
    pattern.Pos = 0
  }
  // Try to match the current rune in Text.
  if ch == pattern.Text[pattern.Pos] {
    pattern.Pos++
  }
  // Check for a complete match.
  return pattern.Pos == pattern.Length
}


//--- Template parsing and output generation

type TemplateEntry struct {
  SitePath, HardPath string
  FileInfo os.FileInfo
  InsertionLine int
}

func (entry TemplateEntry) String() string {
  if entry.InsertionLine == 0 {
    return entry.SitePath
  }
  return fmt.Sprintf("-> line %d: %s", entry.InsertionLine, entry.SitePath)
}

func makeTemplateEntry(siteRoot, startDir, sitePath string,
    insertionLine int) (*TemplateEntry, error) {
  hardPath := makeHardPath(siteRoot, startDir, sitePath)
  fileInfo, error := os.Stat(hardPath)
  if error != nil {
    return nil, error
  }
  entry := TemplateEntry{
      SitePath: sitePath,
      HardPath: hardPath,
      FileInfo: fileInfo,
      InsertionLine: insertionLine,
    }
  return &entry, nil
}

func makeHardPath(siteRoot, startDir, sitePath string) string {
  // A hard path names a location in the physical file system rather than
  //  in the website's directory structure. It is either an absolute path
  //  or a relative path with respect to the directory containing the
  //  top-level template that is being parsed.
  var dir string
  if filepath.IsAbs(sitePath) {
    dir = siteRoot
  } else {
    dir = startDir
  }
  // Note that filepath.Join automatically performs filepath.Clean, thus
  //  returning a lexically unique form of the path. However, the path
  //  does not uniquely identify a file if it includes a symbolic link.
  //  Therefore, we cannot rely on string comparison to prevent cycles.
  hardPath := filepath.Join(dir, sitePath)
  return hardPath
}

func parse(templatePath string) error {

  // We resolve absolute paths with the website root.
  siteRoot := "/var/www/dd1"  // Stub. We'll get the real value from Apache.
  // We resolve relative paths using the starting directory.
  startDir := filepath.Dir(templatePath)

  // Make an insertion stack with a top-level entry.
  entry, error := makeTemplateEntry(siteRoot, startDir, templatePath, 0)
  if error != nil {
    return error
  }
  stack := []*TemplateEntry{ entry }
  return doParse(siteRoot, startDir, stack)
}

func doParse(siteRoot, startDir string, stack []*TemplateEntry) error {
  current := stack[len(stack)-1]
  if verbose {
    fmt.Fprintf(&output, "// start \"%s\"\n", current.SitePath)
  }
  var topLevel bool
  if len(stack) == 1 {
    topLevel = true
  }

  // Check for an insertion cycle.
  for i := len(stack)-2; i >= 0; i-- {
    ancestor := stack[i]
    if os.SameFile(ancestor.FileInfo, current.FileInfo) {
      lines := []string{ "doParse: insertion cycle" }
      for j := i; j < len(stack); j++ {
        lines = append(lines, stack[j].String())
      }
      message := fmt.Sprintf(strings.Join(lines, "\n  "))
      return errors.New(message)
    }
  }

  var error error
  var file *os.File

  file, error = os.Open(current.HardPath)
  if error != nil {
    return error
  }

  reader := bufio.NewReader(file)
  writer := bufio.NewWriter(os.Stdout)
  defer writer.Flush()

  codePattern := newPattern("<?code")
  insertPattern := newPattern("<?insert")
  openPatterns := []*Pattern{ &codePattern, &insertPattern }
  var open *Pattern
  close := newPattern("?>")

  var buffer []rune
  var ch rune
  var size int
  countBytes, countRunes := 0, 0
  lineIndex := 1
  prefix := true

  for {
    ch, size, error = reader.ReadRune()
    if error == nil {
      buffer = append(buffer, ch)
      countBytes += size
      countRunes += 1
      if ch == '\n' {
        lineIndex += 1
      }
    } else {
      content := string(buffer)
      if topLevel {
        content = strings.TrimSpace(content)
      }
      emitStatic(content)
      break
    }

    if open == nil {
      for _, pattern := range openPatterns {
        if pattern.Next(ch) {
          open = pattern
          content := string(buffer[0:len(buffer)-open.Length])
          if prefix {
            if topLevel {
              content = strings.TrimSpace(content)
            }
            prefix = false
          }
          emitStatic(content)
          buffer = []rune{}
        }
      }
    } else {
      if close.Next(ch) {
        content := buffer[0:len(buffer)-close.Length]
        if open == &codePattern {
          emitCode(string(content))
        } else if open == &insertPattern {
          childPath := strings.TrimSpace(string(content))
          entry, error := makeTemplateEntry(siteRoot, startDir, childPath,
              lineIndex)
          if error != nil {
            return error
          }
          stack = append(stack, entry)
          error = doParse(siteRoot, startDir, stack)
          if error != nil {
            return error
          }
          stack = stack[0:len(stack)-1]
        }
        open = nil
        buffer = []rune{}
      }
    }
  }
  if verbose {
    fmt.Fprintf(&output, "// finish \"%s\"\n", current.SitePath)
    fmt.Fprintf(&output, "// read %d bytes, %d runes\n", countBytes, countRunes)
    fmt.Fprintf(&output, "// finished on line %d\n", lineIndex)
  }
  if error == io.EOF {
    return nil
  }
  return error
}

func emitCode(content string) {
  fmt.Fprint(&output, content)
}

func emitStatic(content string) {
  if len(content) == 0 {
    return
  }
  from := 0
  for pos, ch := range content {
    if ch == '`' {
      if pos != from {
        raw := fmt.Sprintf("`%s`", content[from:pos])
        emitRaw(raw)
      }
      emitRaw("'`'")
      from = pos+1
    }
  }
  if from != len(content) {
    raw := fmt.Sprintf("`%s`", content[from:len(content)])
    emitRaw(raw)
  }
}
func emitRaw(s string) {
  fmt.Fprintf(&output, "fmt.Print(%s)\n", s)
}


func main() {
  writer := bufio.NewWriter(os.Stdout)
  defer writer.Flush()
  log := os.Stderr

  numFiles := len(os.Args)-1
  if numFiles == 0 {
    writer.WriteString("No files specified.\n")
    return
  }
  for argIx := 1; argIx <= numFiles; argIx++ {
    // Parse the top-level template. The resulting source code goes into
    //  a global variable called output, the type of which is bytes.Buffer.
    path := os.Args[argIx]
    error := parse(path)
    if error == nil {
      // Use Go's parser on the code that came out of the template parser.
      fileSet := token.NewFileSet()
      fileNode, error := parser.ParseFile(fileSet, "output", output.Bytes(),
          parser.ParseComments)
      if error == nil {
        // Before injecting an import statement, we have to check for
        //  various collisions. The astutil.AddNameImport function only
        //  checks for an import with the same path. There are more cases.
        //  0. no import has the name or path "fmt":
        //    import "fmt"
        //  1. an import has the path "fmt" with a different name:
        //    import "fmt"
        //  2. an import has the name "fmt" with a different path:
        //    import _fmt0 "fmt" (or _fmt1, ...) and write _fmt0.Printf
        //  3. an import has the name and path "fmt":
        //    do nothing
        //  to inject an import statement. That function includes a 
        //  import of fmt because astutil.AddImport includes such a check.
        //  However, it does not check for aliased package names. We have
        //  to do this ourselves. If there is a conflict, we correct it by
        //  aliasing fmt to _fmt0 (or _fmt1, _fmt2, ...).
        //  Hmm... what if fmt is imported but aliased to something else?
        added := astutil.AddImport(fileSet, fileNode, "fmt")
        fmt.Fprintf(log, "added: %t\n", added)

        // Print with a custom configuration: soft tabs of two spaces each.
        config := printer.Config{ Mode: printer.UseSpaces, Tabwidth: 2 }
        (&config).Fprint(writer, fileSet, fileNode)
      } else {
        // If there was a Go parsing error, print our template-generated
        //  source code, followed by the Go parser's error message.
        writer.Write(output.Bytes())
        writer.WriteString("\n----------\n")
        writer.WriteString(fmt.Sprintf("Go parsing error: %s\n", error))
      }
    } else {
      writer.WriteString(fmt.Sprintf("template parsing error: %s\n", error))
    }
  }
}
