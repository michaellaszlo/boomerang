// The executable "parse-template" takes one or more file names and
//  calls processTemplate on each one.

package main

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

var verbose bool = false
var log *os.File = os.Stderr

var sections []*Section     // stores output sections during template parsing
var stack []*TemplateEntry  // used to prevent template insertion cycles

// Section contains the text of a code section or static section.
type Section struct {
  Kind uint
  Text string
}
const (
  StaticSection uint = iota
  CodeSection
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

// TemplateEntry contains path and file information about a template.
type TemplateEntry struct {
  SitePath, HardPath string  // The site path is relative to the site root,
  FileInfo os.FileInfo       //  while the hard path is a physical path in
  InsertionLine int          //  the file system. A child template begins
}                            //  at an insertion line of a parent template.

// String implements the fmt.Stringer interface for TemplateEntry.
func (entry TemplateEntry) String() string {
  if entry.InsertionLine == 0 {
    return entry.SitePath
  }
  return fmt.Sprintf("-> line %d: %s", entry.InsertionLine, entry.SitePath)
}

// MakeTemplateEntry fills in every field of a TemplateEntry, generating
//  the hard path and file info based on the details of the site path.
func MakeTemplateEntry(siteRoot, startDir, sitePath string,
    insertionLine int) (*TemplateEntry, error) {
  hardPath := MakeHardPath(siteRoot, startDir, sitePath)
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

// MakeHardPath uses the details of the site path to make a hard path.
// A hard path names a location in the physical file system rather than
//  in the website's directory structure. It is either an absolute path
//  or a relative path with respect to the starting directory, which is
//  where the top-level template is located.
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
//  list and the parsing stack, and calls doParse.
func parse(siteRoot, templatePath string) error {
  // We resolve relative paths using the starting directory.
  startDir := filepath.Dir(templatePath)
  entryPoint := filepath.Base(templatePath)
  // Make an insertion stack with a top-level entry.
  entry, error := MakeTemplateEntry(siteRoot, startDir, entryPoint, 0)
  if error != nil {
    return error
  }
  sections = []*Section{}
  stack = []*TemplateEntry{ entry }
  return doParse(siteRoot, startDir)
}

// doParse recursively parses a template and its children.
func doParse(siteRoot, startDir string) error {
  current := stack[len(stack)-1]
  if verbose {
    fmt.Fprintf(log, "// start \"%s\"\n", current.SitePath)
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
      for j := i; j < len(stack); j++ {           //  In the event of a cycle,
        lines = append(lines, stack[j].String())  //   generate a stack trace.
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
  //  no need to check tag depth because nested tags are not allowed.
  codePattern := NewPattern("<?code")
  insertPattern := NewPattern("<?insert")
  openPatterns := []*Pattern{ &codePattern, &insertPattern }
  var open *Pattern
  close := NewPattern("?>")

  // Each character goes into the buffer, which we empty whenever we match
  //  an opening or closing tag. In the former case the buffer must contain
  //  static text, while the latter case is code or a template insertion.
  var buffer []rune
  var ch rune
  var size int
  countBytes, countRunes := 0, 0  // Byte and rune counts only appear in log
  lineIndex := 1                  // messages. We store the line index in
  prefix := true                  //  template entries for debugging purposes.

  for {
    ch, size, error = reader.ReadRune()
    if error == nil {
      buffer = append(buffer, ch)
      countBytes += size
      countRunes += 1
      if ch == '\n' {
        lineIndex += 1
      }
    } else {               // We assume that the read failed due to EOF.
      content := string(buffer)
      if topLevel {        // Trim the end of the top-level template.
        content = strings.TrimSpace(content)
      }
      emitStatic(content)
      break
    }

    // Once a tag has been opened, we ignore further opening tags until
    //  we have come across the closing tag. Nesting is not allowed.
    if open == nil {
      for _, pattern := range openPatterns {
        if pattern.Next(ch) {
          open = pattern
          content := string(buffer[0:len(buffer)-open.Length])  // remove tag
          if prefix {
            if topLevel {  // Trim the start of the top-level template.
              content = strings.TrimSpace(content)
            }
            prefix = false
          }
          emitStatic(content)  // Text before an opening tag must be static.
          buffer = []rune{}
        }
      }
    } else {
      if close.Next(ch) {
        content := buffer[0:len(buffer)-close.Length]  // remove tag
        if open == &codePattern {           // Code sections are just text.
          emitCode(string(content))
        } else if open == &insertPattern {  // Insertion requires more work.
          childPath := strings.TrimSpace(string(content))
          entry, error := MakeTemplateEntry(siteRoot, startDir, childPath,
              lineIndex)                    // We have to push a new template
          if error != nil {                 //  entry onto the stack and make
            return error                    //  a recursive call.
          }
          stack = append(stack, entry)
          error = doParse(siteRoot, startDir)
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
    fmt.Fprintf(log, "// finish \"%s\"\n", current.SitePath)
    fmt.Fprintf(log, "// read %d bytes, %d runes\n", countBytes, countRunes)
    fmt.Fprintf(log, "// finished on line %d\n", lineIndex)
  }
  if error == io.EOF {
    return nil
  }
  return error
}

// emitCode makes a code section and adds it to the global sections.
func emitCode(content string) {
  sections = append(sections, &Section{ Kind: CodeSection, Text: content })
}

// emitStatic breaks a string into back-quoted strings and back quotes,
//  calling emitStaticChunk for each one. 
func emitStatic(content string) {
  if len(content) == 0 {
    return
  }
  from := 0
  for pos, ch := range content {
    if ch == '`' {
      if pos != from {
        raw := fmt.Sprintf("`%s`", content[from:pos])
        emitStaticChunk(raw)
      }
      emitStaticChunk("'`'")
      from = pos+1
    }
  }
  if from != len(content) {
    raw := fmt.Sprintf("`%s`", content[from:len(content)])
    emitStaticChunk(raw)
  }
}
// emitStaticChunk makes a static section and adds it to the global sections.
func emitStaticChunk(chunk string) {
  sections = append(sections, &Section{ Kind: StaticSection, Text: chunk })
}

// ProcessTemplate is the top-level template parsing function. It calls
//  parse, then glues the sections together and injects an import statement
//  as needed. The final result is printed to the global writer. 
func ProcessTemplate(siteRoot, templatePath string, writer *bufio.Writer) {
  // We parse the template to obtain code sections and static sections.
  error := parse(siteRoot, templatePath)
  if error != nil {
    writer.WriteString(fmt.Sprintf("Template parsing error: %s\n", error))
    return
  }

  // Concatenate only the code sections. We're not adding print statements yet
  //  because we don't know what the print command is going to look like. We
  //  do want to parse the user's code in order to scan the imports.
  output := bytes.Buffer{}
  for _, section := range sections {
    if section.Kind == CodeSection {
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

  //  Has the desired package been imported? Is the name available?
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
      importedAs = importName    //  we use the name in the first occurrence.
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
    if section.Kind == CodeSection {
      fmt.Fprintf(&output, section.Text)
    } else {
      s := fmt.Sprintf(";%s%s(%s);", printPrefix, printCall, section.Text)
      fmt.Fprintf(&output, s)
    }
  }
  // Have Go parse the whole output in preparation for import injection
  //  and formatted code output.
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
    if importAs == seekName {  // to get 'import "fmt"', not 'import fmt "fmt"'
      astutil.AddImport(fileSet, fileNode, seekPath)
    } else {                   // AddNamedImport would make 'import fmt "fmt"'
      astutil.AddNamedImport(fileSet, fileNode, importAs, seekPath)
    }
  }

  // Print with a custom configuration: soft tabs of two spaces each.
  config := printer.Config{ Mode: printer.UseSpaces, Tabwidth: 2 }
  (&config).Fprint(writer, fileSet, fileNode)
}

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
    ProcessTemplate(siteRoot, os.Args[argIx], writer)
  }
}
