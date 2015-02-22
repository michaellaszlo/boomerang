// Package apptemplate implements template parsing and code generation.
package apptemplate

import (
  "os"
  "fmt"
  "io"
  "bufio"
  "strings"
  "strconv"
  "path"
  "path/filepath"
  "errors"
  "bytes"
  "go/token"
  "go/parser"
  "go/printer"
  "golang.org/x/tools/go/ast/astutil"
)

var verbose = false
var log = os.Stderr

var MergeStaticText = true  // Concatenate consecutive static sections?

var sections []*Section     // Stores output sections during template parsing.
var stack []*Entry  // Used to prevent template insertion cycles.

// Section contains the text of a code section or static section.
type Section struct {
  Kind uint
  Text string
}
const (  // These are Section.Kind values.
  Static uint = iota
  Code
)


//--- Linear pattern matching

// Pattern helps us keep track of progress in matching a string.
type Pattern struct {
  Text []rune
  Length, Pos int
}

// NewPattern initializes a Pattern for a given string.
func NewPattern(s string) Pattern {
  runes := []rune(s)
  return Pattern{ Text: runes, Length: len(runes) }
}

// Next returns true when Pos advances past the last character of Text.
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

// Entry contains path and file information about a template.
type Entry struct {
  SitePath, HardPath string  // The site path is relative to the site root,
  FileInfo os.FileInfo       // while the hard path is a physical path in
  InsertionLine int          // the file system. A child template begins
}                            // at an insertion line of a parent template.

// String implements the fmt.Stringer interface for Entry.
func (entry Entry) String() string {
  if entry.InsertionLine == 0 {
    return entry.SitePath
  }
  return fmt.Sprintf("-> line %d: %s", entry.InsertionLine, entry.SitePath)
}

// MakeEntry fills in every field of an Entry, generating
// the hard path and file info based on the details of the site path.
func MakeEntry(siteRoot, startDir, sitePath string,
    insertionLine int) (*Entry, error) {
  hardPath := MakeHardPath(siteRoot, startDir, sitePath)
  fileInfo, error := os.Stat(hardPath)
  if error != nil {
    return nil, error
  }
  entry := Entry{
      SitePath: sitePath,
      HardPath: hardPath,
      FileInfo: fileInfo,
      InsertionLine: insertionLine,
    }
  return &entry, nil
}

// MakeHardPath uses the details of the site path to make a hard path.
// A hard path names a location in the physical file system rather than
// in the website's directory structure. It is either an absolute path
// or a relative path with respect to the starting directory, which is
// where the top-level template is located.
func MakeHardPath(siteRoot, startDir, sitePath string) string {
  var dir string
  if filepath.IsAbs(sitePath) {
    dir = siteRoot
  } else {
    dir = startDir
  }
  hardPath := filepath.Join(dir, sitePath)
  return hardPath
}

// parse makes an entry for the top-level template, initializes the section
// list and the parsing stack, and calls doParse.
func parse(siteRoot, templatePath string) error {
  // We resolve relative paths using the starting directory.
  startDir := filepath.Dir(templatePath)
  entryPoint := filepath.Base(templatePath)
  // Make an insertion stack with a top-level entry.
  entry, error := MakeEntry(siteRoot, startDir, entryPoint, 0)
  if error != nil {
    return error
  }
  sections = []*Section{}
  stack = []*Entry{ entry }
  return doParse(siteRoot, startDir)
}

// doParse recursively parses a template and its children.
func doParse(siteRoot, startDir string) error {
  current := stack[len(stack)-1]
  if verbose {
    fmt.Fprintf(log, "// start \"%s\"\n", current.SitePath)
  }

  // Check for an insertion cycle.
  for i := len(stack)-2; i >= 0; i-- {
    ancestor := stack[i]
    if os.SameFile(ancestor.FileInfo, current.FileInfo) {
      lines := []string{ "doParse: insertion cycle" }
      for j := i; j < len(stack); j++ {           // In the event of a cycle,
        lines = append(lines, stack[j].String())  // generate a stack trace.
      }
      message := fmt.Sprintf(strings.Join(lines, "\n  "))
      return errors.New(message)
    }
  }

  // Open the template file and make a reader.
  var error error
  var file *os.File
  file, error = os.Open(current.HardPath)
  if error != nil {
    return error
  }
  reader := bufio.NewReader(file)

  // There are two opening patterns but only one closing pattern. There is
  // no need to check tag depth because nested tags are not allowed.
  codePattern := NewPattern("<?code")
  insertPattern := NewPattern("<?insert")
  openPatterns := []*Pattern{ &codePattern, &insertPattern }
  var open *Pattern
  close := NewPattern("?>")

  // Each character goes into the buffer, which we empty whenever we match
  // an opening or closing tag. In the former case the buffer must contain
  // static text, while the latter case is code or a template insertion.
  var buffer []rune
  var ch rune
  var size int
  countBytes, countRunes := 0, 0  // Byte and rune counts are logged.
  lineIndex := 1  // The line index is stored in template entries.

  for {
    ch, size, error = reader.ReadRune()
    if error == nil {
      buffer = append(buffer, ch)
      countBytes += size
      countRunes++
      if ch == '\n' {
        lineIndex++
      }
    } else {               // We assume that the read failed due to EOF.
      content := string(buffer)
      pushStatic(content)
      break
    }

    // Once a tag has been opened, we ignore further opening tags until
    // we have come across the closing tag. Nesting is not allowed.
    if open == nil {
      for _, pattern := range openPatterns {
        if pattern.Next(ch) {
          open = pattern
          content := string(buffer[:len(buffer)-open.Length])  // Remove tag.
          pushStatic(content)  // Text before an opening tag must be static.
          buffer = []rune{}
        }
      }
    } else {
      if close.Next(ch) {
        content := buffer[:len(buffer)-close.Length]  // Remove tag.
        if open == &codePattern {           // Code sections are just text.
          pushCode(string(content))
        } else if open == &insertPattern {  // Insertion requires more work.
          childPath := strings.TrimSpace(string(content))
          entry, error := MakeEntry(siteRoot, startDir, childPath,
              lineIndex)                    // We have to push a new template
          if error != nil {                 // entry onto the stack and make
            return error                    // a recursive call.
          }
          stack = append(stack, entry)
          error = doParse(siteRoot, startDir)
          if error != nil {
            return error
          }
          stack = stack[:len(stack)-1]
        }
        open = nil
        buffer = []rune{}
      }
    }
  }
  if verbose {
    fmt.Fprintf(log, "parsed \"%s\"\n", current.SitePath)
    fmt.Fprintf(log, "read %d bytes, %d runes\n", countBytes, countRunes)
    fmt.Fprintf(log, "finished on line %d\n", lineIndex)
  }
  if error == io.EOF {
    return nil
  }
  return error
}

// pushCode makes a code section and adds it to the global sections.
func pushCode(content string) {
  sections = append(sections, &Section{ Kind: Code, Text: content })
}

// pushStatic makes a static section and adds it to the global sections.
func pushStatic(chunk string) {
  sections = append(sections, &Section{ Kind: Static, Text: chunk })
}

// makeRawStrings splits a string into back-quoted strings and back quotes.
func makeRawStrings(content string) (pieces []string) {
  pieces = []string{}
  from := 0
  for pos, ch := range content {
    if ch == '`' {
      if pos != from {
        pieces = append(pieces, fmt.Sprintf("`%s`", content[from:pos]))
      }
      pieces = append(pieces, "'`'")
      from = pos+1
    }
  }
  if from != len(content) {
    pieces = append(pieces, fmt.Sprintf("`%s`", content[from:]))
  }
  return
}

// Process is the top-level template parsing function. It calls
// parse, then glues the sections together and injects an import statement
// as needed. The final result is printed to the global writer. 
func Process(siteRoot, templatePath string, writer *bufio.Writer) {
  // We parse the template to obtain code sections and static sections.
  error := parse(siteRoot, templatePath)
  if error != nil {
    writer.WriteString(fmt.Sprintf("Template parsing error: %s\n", error))
    return
  }

  // Discard whitespace sections before the first code section.
  for len(sections) != 0 {
    section := sections[0]
    if section.Kind == Code {
      break
    }
    text := strings.TrimSpace(section.Text)
    if len(text) != 0 {
      break
    }
    sections = sections[1:]
  }

  // Discard whitespace sections after the last code section.
  for len(sections) != 0 {
    section := sections[len(sections)-1]
    if section.Kind == Code {
      break
    }
    text := strings.TrimSpace(section.Text)
    if len(text) != 0 {
      break
    }
    sections = sections[:len(sections)-1]
  }

  // Concatenate only the code sections. We're not adding print statements yet
  // because we don't know what the print command is going to look like. We
  // do want to parse the user's code in order to scan the imports.
  output := bytes.Buffer{}
  for _, section := range sections {
    if section.Kind == Code {
      fmt.Fprintf(&output, section.Text)
    }
  }
  fileSet := token.NewFileSet()
  fileNode, error := parser.ParseFile(fileSet, "output", output.Bytes(),
      parser.ParseComments)
  if error != nil {
    writer.Write(output.Bytes())
    writer.WriteString(fmt.Sprintf(
        "\n---\nError parsing code sections: %s\n", error))
    return
  }

  seekPath := "fmt"  // The print command is to be found in this package.
  seekName := path.Base(seekPath)
  printCall := "Print"

  // Has the desired package been imported? Is the name available?
  isImported := false
  var importedAs string          // Use this if the path has been imported.
  seenName := map[string]bool{}  // Consult this if we have to import.

  for _, importSpec := range fileNode.Imports {
    importPath, _ := strconv.Unquote(importSpec.Path.Value)
    var importName string
    if importSpec.Name == nil {
      importName = path.Base(importPath)
    } else {
      importName = importSpec.Name.Name
    }
    seenName[importName] = true  // NB: underscore imports only run a package.
    if !isImported && importPath == seekPath && importName != "_" {
      isImported = true          // If the package is imported several times,
      importedAs = importName    // we use the name in the first occurrence.
    }
  }

  var importAs, printPrefix string  // NB: these are "" by default
  if isImported {
    if importedAs != "." {  // No prefix is needed with a dot import.
      printPrefix = importedAs+"."
    }
  } else {
    if !seenName[seekName] {
      importAs = seekName
    } else {                // Look for a name that hasn't been used yet.
      for i := 0; ; i++ {
        importAs = fmt.Sprintf("%s_%d", seekName, i)
        _, found := seenName[importAs]
        if !found {
          break
        }
      }
    }
    printPrefix = importAs+"."
  }

  // Concatenate the code with static sections wrapped in print statements.
  output.Reset()
  for _, section := range sections {
    if section.Kind == Code {
      fmt.Fprintf(&output, section.Text)
    } else {
      pieces := makeRawStrings(section.Text)
      for _, piece := range pieces {
        s := fmt.Sprintf(";%s%s(%s);\n", printPrefix, printCall, piece)
        fmt.Fprintf(&output, s)
      }
    }
  }
  // Have Go parse the whole output in preparation for import injection
  // and formatted code output.
  fileSet = token.NewFileSet()
  fileNode, error = parser.ParseFile(fileSet, "output", output.Bytes(),
      parser.ParseComments)
  if error != nil {
    writer.Write(output.Bytes())
    writer.WriteString(fmt.Sprintf(
        "\n---\nError parsing entire template output: %s\n", error))
    return
  }
  // Inject an import statement if necessary.
  if !isImported {
    if importAs == seekName {  // Make 'import "fmt"', not 'import fmt "fmt"'.
      astutil.AddImport(fileSet, fileNode, seekPath)
    } else {                   // AddNamedImport would make 'import fmt "fmt"'.
      astutil.AddNamedImport(fileSet, fileNode, importAs, seekPath)
    }
  }

  // Print with a custom configuration: soft tabs of two spaces each.
  config := printer.Config{ Mode: printer.UseSpaces, Tabwidth: 2 }
  (&config).Fprint(writer, fileSet, fileNode)
}

