// The apptemplate package implements template parsing and code generation.
package apptemplate

import (
  "os"
  "fmt"
  "io"
  "bufio"
  "strings"
  "unicode"
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

var Verbose = false
var log = os.Stderr

var MergeStaticText = true  // Concatenate consecutive static sections?

var sections []*Section  // Stores output sections during template parsing.
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
    // Check for a complete match.
    if pattern.Pos == pattern.Length {
      return true
    }
  } else {
    pattern.Pos = 0
  }
  return false
}


//--- Template parsing and output generation

// Entry contains path and file information about a template.
type Entry struct {
  GivenPath, HardPath string  // GivenPath was passed to buildapp or insert.
  FileInfo os.FileInfo        // HardPath is an absolute file-system path.
  InsertionLine int           // InsertionLine is a line number in the parent.
}

// String implements the fmt.Stringer interface for Entry.
func (entry Entry) String() string {
  if entry.InsertionLine == 0 {
    return entry.GivenPath
  }
  return fmt.Sprintf("-> line %d: %s", entry.InsertionLine, entry.GivenPath)
}

// parse makes an entry for the top-level template, initializes the section
// list and the parsing stack, and calls doParse.
func parse(siteRoot, templatePath string) error {
  fileInfo, err := os.Stat(templatePath)
  if err != nil {
    fmt.Fprintf(os.Stderr, "os.Stat failed in %s\n", templatePath)
    return err
  }
  // Work out the name of the containing directory. This becomes templateDir,
  // which will be used to resolve relative paths for inserted templates.
  templateDir := filepath.Dir(templatePath)
  if !filepath.IsAbs(templatePath) {  // We want an absolute file-system path.
    workingDirectory, err := os.Getwd()
    if err != nil {
      fmt.Fprintf(os.Stderr, "os.Getwd failed in %s\n", templatePath)
      return err
    }
    templateDir = filepath.Join(workingDirectory, templateDir)
  }
  hardPath := filepath.Join(templateDir, filepath.Base(templatePath))
  // Make an insertion stack with an entry for the top-level template.
  entry := Entry{
      GivenPath: templatePath,
      HardPath: hardPath,
      FileInfo: fileInfo,
      InsertionLine: 0,
    }
  sections = []*Section{}
  stack = []*Entry{ &entry }
  return doParse(siteRoot, templateDir)
}

// doParse recursively parses a template and its children.
func doParse(siteRoot, templateDir string) error {
  current := stack[len(stack)-1]
  if Verbose {
    fmt.Fprintf(log, "  doParse \"%s\"\n", current.GivenPath)
  }

  // Check for an insertion cycle.
  for i := len(stack)-2; i >= 0; i-- {
    ancestor := stack[i]
    if os.SameFile(ancestor.FileInfo, current.FileInfo) {
      lines := []string{ "doParse: insertion cycle" }
      for j := 0; j < len(stack); j++ {           // In the event of a cycle,
        lines = append(lines, stack[j].String())  // generate a stack trace.
      }
      message := fmt.Sprintf(strings.Join(lines, "\n  "))
      return errors.New(message)
    }
  }

  // Open the template file and make a reader.
  var file *os.File
  file, err := os.Open(current.HardPath)
  if err != nil {
    fmt.Fprintf(os.Stderr, "os.Open failed on %s\n", current.GivenPath)
    return err
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
  // an opening or closing tag. An opening tag signals the end of a static
  // portion. A closing tag marks the end of code or an insert statement.
  var buffer []rune
  countBytes, countRunes := 0, 0  // Byte and rune counts are logged.
  lineIndex := 1  // The line index is stored in template entries.

  for {
    ch, size, err := reader.ReadRune()
    if err == nil {
      buffer = append(buffer, ch)
      countBytes += size
      countRunes++
      if ch == '\n' {
        lineIndex++
      }
    } else if err == io.EOF {
      content := string(buffer)
      pushStatic(content)
      if Verbose {
        fmt.Fprintf(log, "parsed \"%s\"\n", current.GivenPath)
        fmt.Fprintf(log, "read %d bytes, %d runes\n", countBytes, countRunes)
        fmt.Fprintf(log, "finished on line %d\n", lineIndex)
      }
      return nil
    } else {
      fmt.Fprintf(os.Stderr, "reader.ReadRune failed in %s\n",
          current.GivenPath)
      return err
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
          givenPath := strings.TrimSpace(string(content))
          // Convert the given path into a hard path.
          // Absolute given path: consult the site root.
          // Relative given path: consult the current template directory.
          var hardDir string
          if path.IsAbs(givenPath) {
            hardDir = siteRoot
          } else {
            hardDir = templateDir
          }
          hardPath := filepath.Join(hardDir, givenPath)
          fileInfo, err := os.Stat(hardPath)
          if err != nil {
            fmt.Fprintf(os.Stderr, "os.Stat failed on %s\n", hardPath)
            return err
          }
          entry := Entry{
              GivenPath: givenPath,
              HardPath: hardPath,
              FileInfo: fileInfo,
              InsertionLine: lineIndex,
            }
          // Push the new entry onto the stack and make a recursive call.
          stack = append(stack, &entry)
          childTemplateDir := filepath.Dir(hardPath)
          err = doParse(siteRoot, childTemplateDir)
          if err != nil {
            return err
          }
          stack = stack[:len(stack)-1]
        }
        open = nil
        buffer = []rune{}
      }
    }
  }
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
// as needed. The final result is printed to a buffered writer.
func Process(siteRoot, templatePath string, writer *bufio.Writer) error {
  // Parse the template to obtain code sections and static sections.
  err := parse(siteRoot, templatePath)
  if err != nil {
    message := fmt.Sprintf("Template parsing error in %s: %s\n",
        templatePath, err)
    fmt.Fprint(os.Stderr, message)
    writer.WriteString(message)
    return err
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

  // Take care of initial and final whitespace within the code sections.
  // Find the first and last code sections.
  codeLeft, codeRight := -1, -1
  for i, section := range sections {
    if section.Kind == Code {
      codeLeft = i
      break
    }
  }
  for i := len(sections)-1; i >= 0; i-- {
    if sections[i].Kind == Code {
      codeRight = i
      break
    }
  }
  // Between these code sections, left-trim the initial static sections and
  //  right-trim the final static sections.
  for i := codeLeft+1; i < codeRight; i++ {
    if sections[i].Kind == Static {
      sections[i].Text = strings.TrimLeftFunc(sections[i].Text,
          unicode.IsSpace)
      if len(sections[i].Text) != 0 {  // Continue trimming until the
        break                          // resulting section is non-empty. 
      }
    }
  }
  for i := codeRight-1; i > codeLeft; i-- {
    if sections[i].Kind == Static {
      sections[i].Text = strings.TrimRightFunc(sections[i].Text,
          unicode.IsSpace)
      if len(sections[i].Text) != 0 {  // Continue trimming until non-empty.
        // Add a newline to the final static section.
        sections[i].Text += "\n"
        break
      }
    }
  }

  // Concatenate consecutive static sections if desired.
  if MergeStaticText {
    newSections := []*Section{}
    n := len(sections)
    for pos := 0; pos < n; pos++ {
      section := sections[pos]
      if section.Kind == Code || pos+1 == n || sections[pos+1].Kind == Code {
        newSections = append(newSections, section)
        continue
      }
      substrings := []string{}
      var seek int
      for seek = pos; seek < n && sections[seek].Kind == Static; seek++ {
        substrings = append(substrings, sections[seek].Text)
      }
      section.Text = strings.Join(substrings, "")
      newSections = append(newSections, section)
      pos = seek-1
    }
    sections = newSections
  }

  // Discard code and static sections that consist entirely of whitespace.
  newSections := []*Section{}
  for _, section := range sections {
    trimmed := strings.TrimFunc(section.Text, unicode.IsSpace)
    if len(trimmed) != 0 {
      newSections = append(newSections, section)
    }
  }
  sections = newSections

  // Concatenate only the code sections. We're not adding print statements yet
  // because we don't know what the print command is going to look like. We
  // do want to parse the user's code in order to scan the imports.
  output := bytes.Buffer{}
  for _, section := range sections {
    if section.Kind == Code {
      fmt.Fprint(&output, section.Text)
      fmt.Fprint(&output, "\n")  // Ensure that statements are separated.
    }
  }
  fileSet := token.NewFileSet()
  fileNode, err := parser.ParseFile(fileSet, "output", output.Bytes(),
      parser.ParseComments)
  if err != nil {
    message := fmt.Sprintf("Error parsing code sections: %s\n", err)
    fmt.Fprint(os.Stderr, message)
    writer.WriteString(fmt.Sprintf("%s\n---\n%s", output.Bytes(), message))
    return err
  }

  // seekPath is the import path of the package containing the print command.
  seekPath := "github.com/michaellaszlo/boomerang/runtime" 
  seekName := path.Base(seekPath)
  printCall := "WriteString"

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
      fmt.Fprint(&output, section.Text)
      fmt.Fprint(&output, "\n")  // Ensure that statements are separated.
    } else {
      pieces := makeRawStrings(section.Text)
      for _, piece := range pieces {
        s := fmt.Sprintf(";%s%s(%s);", printPrefix, printCall, piece)
        fmt.Fprintf(&output, s)
      }
    }
  }
  // Have Go parse the whole output in preparation for import injection
  // and formatted code output.
  fileSet = token.NewFileSet()
  fileNode, err = parser.ParseFile(fileSet, "output", output.Bytes(),
      parser.ParseComments)
  if err != nil {
    message := fmt.Sprintf("Error parsing template output: %s\n", err)
    fmt.Fprint(os.Stderr, message)
    writer.WriteString(fmt.Sprintf("%s\n---\n%s", output.Bytes(), message))
    return err
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
  return nil
} // end Process

