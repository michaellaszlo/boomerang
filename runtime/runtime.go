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
  "strings"
)

var (
  statusHeader = ""
  locationHeader = ""
  headers = []string{ "Content-Type: text/html; charset=utf-8" }
  contentBuffer = new(bytes.Buffer)
)

func appendHeader(header string) {
  headers = append(headers, header)
}


//--- User facilities for output.

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


//-- Automatic output.

// PrintCGI writes a whole CGI response: headers, blank line, body. The body
// is made from contentBuffer. The Content-Type and Content-Length
// headers are printed by default. An additional header may be printed for
// redirection or an HTTP status change.
func PrintCGI() {
  contentString := strings.TrimSpace(contentBuffer.String())
  if statusHeader != "" {
    appendHeader(statusHeader)
  }
  if locationHeader != "" {
    appendHeader(locationHeader)
  }
  appendHeader(fmt.Sprintf("Content-Length: %d\n", len(contentString)))
  headerString := strings.Join(headers, "\n")
  writer := bufio.NewWriter(os.Stdout)
  writer.WriteString(headerString)
  writer.WriteString("\n")
  writer.WriteString(contentString)
  writer.WriteString("\n")
  writer.Flush()
}

// PrintBody writes out the content buffer.
func PrintBody() {
  contentBuffer.WriteTo(os.Stdout)
}


//--- HTTP redirection and status modification

// SetHTTPStatus causes a status header to be added to the CGI output. It
// can be called after body content has been emitted because the runtime
// package buffers all CGI output. SetHTTPStatus can be called several
// times and only the header generated for the final call will be emitted.
func SetHTTPStatus(statusCode int, reasonPhrase string) {
  statusHeader = fmt.Sprintf("Status: %d %s", statusCode, reasonPhrase)
}

// Redirect causes a Status header with "301 Moved Permanently" and a
// Location header with the specified URL to be added to the CGI output.
// Like SetHTTPStatus, it can be called after emittinng content and it can
// be called several times, with only the final call taking effect.
func Redirect(url string) {
  RedirectWithStatus(url, 301, "Moved Permanently")
}

// RedirectWithStatus is like Redirect except it allows the caller to
// specify a status code and reason phrase.
func RedirectWithStatus(url string, statusCode int, reasonPhrase string) {
  SetHTTPStatus(statusCode, reasonPhrase)
  locationHeader = "Location: " + url
}


