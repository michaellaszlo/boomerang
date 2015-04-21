// The runtime package enables buffered writing and HTTP header generation
// in the Go programs that result from template compilation. It is
// automatically injected by apptemplate.Process if the user did not already
// import it in the top-level template. The user can use functions provided
// by the runtime package to change the HTTP status or perform redirection.
package runtime

import (
  "os"
  "bufio"
  "bytes"
  "fmt"
)

//--- Output from the compiled program.

var contentBuffer bytes.Buffer

// WriteString appends a string to the content buffer.
func WriteString(s string) {
  contentBuffer.WriteString(s)
}

// Print calls fmt.Sprint and writes the result to the content buffer.
func Print(a ...interface{}) {
  contentBuffer.WriteString(fmt.Sprint(a...))
}

// Println calls fmt.Sprintln and writes the result to the content buffer.
func Println(a ...interface{}) {
  contentBuffer.WriteString(fmt.Sprintln(a...))
}

// Printf calls fmt.Sprintf and writes the result to the content buffer.
func Printf(format string, a ...interface{}) {
  contentBuffer.WriteString(fmt.Sprintf(format, a...))
}

// PrintCGI writes a whole CGI response: headers, blank line, body. The body
// is made from contentBuffer. The headers Content-Length and Content-Type
// headers are printed by default. An additional header may be printed if
// the user has requested an HTTP status change.
func PrintCGI() {
  writer := bufio.NewWriter(os.Stdout)
  writer.WriteString(fmt.Sprintf("Content-Length: %d\n", contentBuffer.Len()))
  writer.WriteString("Content-Type: text/html; charset=utf-8\n")
  writer.WriteString("\n")
  contentBuffer.WriteTo(writer)
  writer.Flush()
}

// PrintBody writes out the content buffer.
func PrintBody() {
  contentBuffer.WriteTo(os.Stdout)
}

func SetHTTPStatus(code int, message string) {
}

func Redirect(url string) {
}

